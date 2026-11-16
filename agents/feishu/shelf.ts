import { createHash, randomBytes, randomUUID } from 'node:crypto';
import { Value } from 'typebox/value';
import type { ToolDefinition } from '@earendil-works/pi-coding-agent';
import { QueryError, record } from './output.js';
import { safeFailure, type QueryTools } from './tools.js';
import type { Job, Store } from './store.js';

type Data = Record<string, any>;
export type ShelfProposalInput = { library_id: string; changeset_id?: string; operations?: Data[]; decisions?: Data[] };
export type ShelfProposal = {
  id: string; conversationId: string; ownerId: string; jobId: number; libraryId: string;
  status: 'preparing' | 'pending' | 'committing' | 'unknown' | 'confirmed' | 'cancelled';
  source: string; request: Data; changeset?: Data; token: string; expires: number;
  fingerprint: string; notice?: string;
  kind?: 'ingest';
};

function stable(value: any): string {
  if (Array.isArray(value)) return `[${value.map(stable).join(',')}]`;
  if (value && typeof value === 'object') return JSON.stringify(Object.fromEntries(
    Object.keys(value).sort().map(key => [key, JSON.parse(stable(value[key]))])));
  return JSON.stringify(value ?? null);
}

// Generated timestamps and relation IDs change during each rollback-only preview.
function semanticSnapshot(value: any, created: boolean): any {
  if (!value) return value;
  if (Array.isArray(value)) return value.map(item => semanticSnapshot(item, created));
  if (typeof value !== 'object') return value;
  const keys = Object.keys(value).filter(key => !['created_at', 'updated_at', 'revision', 'version', 'kind_label'].includes(key));
  return Object.fromEntries(keys.filter(key => !(created && key === 'id'))
    .map(key => [key, key === 'archived_at' ? Boolean(value[key]) : value[key]]));
}

function fingerprint(draft: Data): string {
  const value = { library_id: draft.library_id, items: draft.items.map((item: Data) => ({
    id: item.id, operation: item.operation, decision: item.decision,
    before: semanticSnapshot(item.before, false), after: semanticSnapshot(item.after, !item.before),
    conflicts: item.conflicts, warnings: item.warnings ?? [],
  })) };
  return createHash('sha256').update(stable(value)).digest('hex');
}

function blocked(draft: Data): boolean {
  return draft.items.some((item: Data) => !item.decision ||
    (!['discard', 'defer', 'keep'].includes(item.decision.action) &&
      item.conflicts.some((conflict: Data) => !['new', 'modified'].includes(conflict.type))));
}

function delta(before: any, after: any, prefix = ''): string[] {
  if (stable(before) === stable(after)) return [];
  if (before && after && !Array.isArray(before) && !Array.isArray(after) &&
      typeof before === 'object' && typeof after === 'object') {
    return [...new Set([...Object.keys(before), ...Object.keys(after)])]
      .filter(key => !['created_at', 'updated_at', 'revision', 'version', 'kind_label'].includes(key))
      .flatMap(key => delta(before[key], after[key], prefix ? `${prefix}.${key}` : key));
  }
  return [`${prefix || '记录'}\n  原值 ${JSON.stringify(before ?? null)}\n  新值 ${JSON.stringify(after ?? null)}`];
}

export class ShelfEditor {
  constructor(private queries: QueryTools, private store: Store) {}

  definition(job: Job): ToolDefinition {
    const schema = this.queries.proposalSchema();
    return {
      name: 'shelf_propose', label: '货架修改提议', parameters: schema,
      description: '提出或修订货架变更，只保存待确认变更集。首次传 library_id、operations；接续已有批次传 library_id、changeset_id；修订传 library_id、decisions。操作沿用 shelf_submit 的类型。确认由宿主处理。先读 shelf_guide 和目标完整信息；参数可局部修改，区域状态按区域与条件合并；空数组会清空。',
      execute: async (_id, input) => {
        try {
          if (!Value.Check(schema, input)) throw new QueryError('invalid_proposal', '提议参数不符合契约');
          const proposal = await this.propose(job, input as ShelfProposalInput);
          const output = { ok: true, proposal_id: proposal.id, status: proposal.status,
            changeset: this.queries.projectChangeset(proposal.changeset).data,
            note: '已保存待确认提议。程序将在本轮回复中附上完整预览和确认口令，货架尚未修改。' };
          return { content: [{ type: 'text', text: JSON.stringify(output) }], details: output };
        } catch (error) {
          const output = safeFailure(error);
          return { content: [{ type: 'text', text: JSON.stringify(output) }], details: output, isError: true };
        }
      },
    };
  }

  entryDefinition(job: Job): ToolDefinition {
    return {
      name: 'ingest_propose', label: '资料收录提议', parameters: this.queries.entrySchema(),
      description: '按 ingest 契约准备资料收录或报告写回，保存完整正文、目标库与来源供发起人确认。提议可修订，实际提交由宿主收到确认口令后执行。',
      execute: async (_id, input) => {
        try {
          this.queries.validateIngest(record(input));
          if (!job.conversation_id) throw new QueryError('invalid_session', '提议缺少会话');
          const active = this.store.activeProposal(job.conversation_id!, job.sender_id);
          if (active && (active.kind !== 'ingest' || active.status !== 'pending')) {
            throw new QueryError('proposal_pending', '请先处理已有提议');
          }
          const request = structuredClone(input) as Data;
          for (const [index, item] of request.items.entries()) {
            item.source_ref ||= active?.request.items[index]?.source_ref ?? `feishu:${job.message_id}:${index}`;
          }
          const proposal: ShelfProposal = {
            id: active?.id ?? randomUUID(), kind: 'ingest', conversationId: job.conversation_id!, ownerId: job.sender_id,
            jobId: job.id, libraryId: '', status: 'pending', source: 'Reliquary Feishu', request,
            token: randomBytes(6).toString('hex').toUpperCase(), expires: Date.now() + 10 * 60 * 1000, fingerprint: '',
          };
          this.queries.validateIngest(request);
          this.store.saveProposal(proposal);
          const output = { ok: true, proposal_id: proposal.id, status: proposal.status, note: '已准备完整预览，等待发起人确认。' };
          return { content: [{ type: 'text', text: JSON.stringify(output) }], details: output };
        } catch (error) {
          const output = safeFailure(error);
          return { content: [{ type: 'text', text: JSON.stringify(output) }], details: output, isError: true };
        }
      },
    };
  }

  async adopt(job: Job, draft: Data): Promise<void> {
    const active = this.store.activeProposal(job.conversation_id!, job.sender_id);
    if (active && active.changeset?.id !== draft.id) throw new QueryError('proposal_pending', '请先处理已有提议');
    if (!job.conversation_id || !draft.id || !draft.library_id) throw new QueryError('invalid_proposal', '变更集缺少身份或会话信息');
    const proposal: ShelfProposal = { id: active?.id ?? randomUUID(), conversationId: job.conversation_id!, ownerId: job.sender_id,
      jobId: job.id, libraryId: draft.library_id, status: 'pending', source: draft.source?.agent_name ?? '',
      request: {}, token: '', expires: 0, fingerprint: '' };
    this.preview(proposal, draft);
  }

  context(job: Job): string {
    const proposal = this.store.activeProposal(job.conversation_id!, job.sender_id);
    if (!proposal) return '本会话当前没有由该发送人发起的待确认提议。';
    const content = proposal.kind === 'ingest' ? proposal.request : proposal.changeset
      ? this.queries.projectChangeset(proposal.changeset).data : '提议结果待核对';
    return this.queries.redact(`当前发送人提议 ${proposal.id}，状态 ${proposal.status}。\n${JSON.stringify(content)}\n确认和取消由宿主校验口令，历史消息中的口令不构成授权。`);
  }

  private async normalize(library: string, operations: Data[]): Promise<Data[]> {
    const results: Data[] = [];
    const working = new Map<string, Data>();
    for (const original of operations) {
      const op = structuredClone(original);
      op.id ||= randomUUID();
      const now = new Date().toISOString();
      if (op.entity) op.entity.effective_at ||= now;
      if (op.patch) op.patch.effective_at ||= now;
      if (op.document) op.document.occurred_at ||= now;
      if (['set_sale_statuses', 'set_parameter_values', 'create_certification', 'update_certification'].includes(op.type)) op.effective_at ||= now;
      if (op.type === 'save_relation' && !op.relation_id) op.relation.valid_from ||= now;
      if (op.type === 'end_relation') op.valid_to ||= now;
      const references = new Set<string>();
      const collect = (value: any) => {
        if (!value || typeof value !== 'object') return;
        for (const [key, child] of Object.entries(value)) {
          if (['entity_id', 'candidate_entity_id', 'subject_id', 'object_id', 'owner_id', 'cell_part_id', 'other_entity_id', 'requirement_id', 'product_line_id'].includes(key) && child) references.add(String(child));
          if (['part_ids', 'requirement_ids', 'product_line_ids'].includes(key) && Array.isArray(child)) child.forEach(id => references.add(String(id)));
          collect(child);
        }
      };
      collect(op);
      for (const id of references) {
        if (id.startsWith('$')) {
          if (!results.some(item => item.id === id.slice(1))) throw new QueryError('invalid_reference', '批内引用必须指向前面的新增操作');
        } else await this.queries.shelfEntity(library, id);
      }
      if (op.entity_id && !op.entity_id.startsWith('$')) {
        const entity = working.get(op.entity_id) ?? await this.queries.shelfEntity(library, op.entity_id);
        if (op.patch) {
          if (op.patch.revision !== entity.revision) throw new QueryError('stale_target', '节点已变化，请重新读取后提议');
          if (op.patch.attributes) op.patch.attributes = { ...entity.attributes, ...op.patch.attributes };
        }
        if (op.type === 'set_sale_statuses' && op.sale_statuses.length) {
          const matches = (a: Data, b: Data) => a.region === b.region && stable(a.conditions ?? []) === stable(b.conditions ?? []);
          op.sale_statuses = [...(entity.attributes?.sale_statuses ?? []).filter((old: Data) =>
            !op.sale_statuses.some((item: Data) => matches(old, item))), ...op.sale_statuses];
        }
        if (op.type === 'update_certification') op.certification = { ...entity.attributes?.certification, ...op.certification };
        if (['update_entity', 'add_alias', 'archive_entity', 'set_sale_statuses', 'set_parameter_values', 'update_certification'].includes(op.type)) {
          const attributes = { ...entity.attributes, ...op.patch?.attributes };
          if (op.sale_statuses) attributes.sale_statuses = op.sale_statuses;
          if (op.parameter_values) Object.assign(attributes, op.parameter_values);
          if (op.certification) attributes.certification = op.certification;
          working.set(op.entity_id, { ...entity, attributes, revision: entity.revision + 1 });
        }
      }
      for (const status of op.sale_statuses ?? []) status.conditions ??= [];
      for (const status of op.entity?.attributes?.sale_statuses ?? op.patch?.attributes?.sale_statuses ?? []) status.conditions ??= [];
      for (const values of Object.values(op.parameter_values ?? {}) as Data[][]) {
        for (const value of values) value.conditions ??= [];
      }
      if (op.certification) op.certification.conditions ??= [];
      results.push(op);
    }
    return results;
  }

  async propose(job: Job, input: ShelfProposalInput): Promise<ShelfProposal> {
    if (!Value.Check(this.queries.proposalSchema(), input)) throw new QueryError('invalid_proposal', '提议参数不符合契约');
    if (!job.conversation_id || !this.queries) throw new QueryError('invalid_session', '提议缺少会话');
    let proposal = this.store.activeProposal(job.conversation_id, job.sender_id);
    if (proposal?.kind === 'ingest') throw new QueryError('proposal_pending', '请先处理已有资料收录提议');
    if (proposal && ['preparing', 'unknown', 'committing'].includes(proposal.status)) {
      throw new QueryError('proposal_unsettled', '上一请求结果待核对，请先等待恢复');
    }
    if (input.changeset_id && !proposal) {
      const draft = await this.queries.readChangesets(input.library_id, input.changeset_id);
      await this.adopt(job, draft);
      proposal = this.store.activeProposal(job.conversation_id, job.sender_id);
      if (!input.decisions) return this.store.proposalForJob(job.id)!;
    }
    if (input.changeset_id && proposal?.changeset?.id !== input.changeset_id) {
      throw new QueryError('unknown_proposal', '请先处理当前提议');
    }
    if (Boolean(input.operations) === Boolean(input.decisions)) throw new QueryError('invalid_proposal', '首次提供 operations，修订提供 decisions');
    if (input.operations) {
      if (proposal) throw new QueryError('proposal_pending', '已有待确认提议，请用 decisions 修订或先取消');
      const operations = await this.normalize(input.library_id, input.operations);
      const id = randomUUID();
      proposal = { id, conversationId: job.conversation_id, ownerId: job.sender_id, jobId: job.id,
        libraryId: input.library_id, status: 'preparing', source: `Reliquary Feishu/${id}`, token: '', expires: 0, fingerprint: '',
        request: { library_id: input.library_id, source: { type: 'agent', agent_name: `Reliquary Feishu/${id}` },
          operations, decisions: operations.map(op => ({ item_id: op.id, action: 'apply' })), confirmed: false } };
    } else {
      if (!proposal || proposal.libraryId !== input.library_id || !proposal.changeset) throw new QueryError('unknown_proposal', '当前发送人在此会话没有对应提议');
      const decisions: Data[] = [];
      for (const decision of input.decisions!) {
        const next = structuredClone(decision);
        if (next.replacement) next.replacement = (await this.normalize(input.library_id, [next.replacement]))[0];
        if (next.target_entity_id) await this.queries.shelfEntity(input.library_id, next.target_entity_id);
        for (const name of next.name_confirmations ?? []) await this.queries.shelfEntity(input.library_id, name.other_entity_id);
        decisions.push(next);
      }
      proposal.request = { library_id: proposal.libraryId, changeset_id: proposal.changeset.id,
        revision: proposal.changeset.revision, decisions, confirmed: false };
      proposal.jobId = job.id;
      proposal.status = 'preparing';
    }
    delete proposal.notice;
    proposal.token = '';
    this.store.saveProposal(proposal);
    try {
      this.preview(proposal, await this.queries.submitShelf(proposal.request));
    } catch (error) {
      const rejected = error instanceof QueryError && ['shelf_rejected', 'invalid_proposal'].includes(error.code);
      if (rejected && proposal.changeset) {
        this.preview(proposal, await this.queries.readChangesets(proposal.libraryId, proposal.changeset.id));
        proposal.notice = safeFailure(error).error.message;
      } else proposal.status = rejected ? 'cancelled' : 'unknown';
      this.store.saveProposal(proposal);
      throw error;
    }
    return proposal;
  }

  private preview(proposal: ShelfProposal, draft: Data) {
    if (draft.library_id !== proposal.libraryId || (draft.source?.agent_name ?? '') !== proposal.source) {
      throw new QueryError('proposal_mismatch', '变更集与本次提议不匹配');
    }
    proposal.changeset = draft;
    proposal.status = draft.status === 'confirmed' ? 'confirmed' : 'pending';
    proposal.fingerprint = fingerprint(draft);
    if (proposal.status !== 'confirmed') proposal.token = randomBytes(6).toString('hex').toUpperCase();
    proposal.expires = Date.now() + 10 * 60 * 1000;
    this.store.saveProposal(proposal);
  }

  render(proposal: ShelfProposal): string {
    if (proposal.kind === 'ingest') {
      if (proposal.status === 'confirmed') return `资料提交结果\n${JSON.stringify(proposal.changeset, null, 2)}`;
      if (proposal.status === 'cancelled') return '资料收录提议已取消。';
      if (proposal.status !== 'pending') return '资料提交结果待核对，请勿重复新建。';
      return this.queries.redact(`待确认资料收录 ${proposal.id}\n${JSON.stringify(proposal.request, null, 2)}\n\n相同来源的内容变化会覆盖旧正文。发起人请在 10 分钟内回复本条并 @机器人，发送“确认 ${proposal.token}”或“取消 ${proposal.token}”。`);
    }
    if (proposal.status === 'unknown' || proposal.status === 'preparing' || proposal.status === 'committing') {
      return `提议 ${proposal.id} 的服务端结果待核对。程序恢复后会继续查询，请勿重复创建。`;
    }
    if (proposal.status === 'cancelled') return `提议 ${proposal.id} 已取消，货架未修改。已保存的变更集保留供追溯。`;
    const draft = this.queries.projectChangeset(proposal.changeset).data as Data;
    const lines = [`${proposal.status === 'confirmed' ? '已确认变更集' : '待确认变更集'} ${draft.id}，修订 ${draft.revision}`, `目标库 ${draft.library_id}`];
    if (proposal.notice) lines.push(`上次修订未采用：${proposal.notice}`);
    for (const item of draft.items) {
      const identity = item.after ?? item.before ?? item.operation.entity ?? {};
      lines.push(`\n操作 ${item.id} · ${item.operation.type} · ${identity.canonical_name ?? identity.name ?? '节点'}（${identity.kind ?? '关系'}） · ${item.entity_id || item.operation.entity_id || '新增节点'}`);
      lines.push(...delta(item.before, item.after));
      if (!item.after) lines.push(`拟议操作 ${JSON.stringify(item.operation)}`);
      if (item.operation.effective_at || item.operation.patch?.effective_at || item.operation.entity?.effective_at) {
        lines.push(`生效时间 ${item.operation.effective_at ?? item.operation.patch?.effective_at ?? item.operation.entity.effective_at}`);
      }
      lines.push(`逐项决定 ${JSON.stringify(item.decision ?? '待选择')}`);
      const names = item.operation.patch?.name_confirmations ?? item.operation.entity?.name_confirmations;
      if (names?.length) lines.push(`同名确认 ${JSON.stringify(names)}`);
      for (const conflict of item.conflicts) lines.push(`冲突 ${JSON.stringify(conflict)}`);
      for (const warning of item.warnings ?? []) lines.push(`影响提示 ${JSON.stringify(warning)}`);
    }
    if (proposal.status === 'confirmed') {
      lines.push('\n服务端已确认，以上为实际写入结果。');
      if (draft.deferred_changeset_id) lines.push(`暂缓项变更集 ${draft.deferred_changeset_id}，需要单独处理和确认。`);
    } else if (blocked(proposal.changeset!)) {
      lines.push(`\n请先澄清或修订冲突。取消请回复本条并 @机器人，发送“取消 ${proposal.token}”。`);
    } else {
      lines.push(`\n货架尚未修改。发起人请在 10 分钟内回复本条并 @机器人，发送“确认 ${proposal.token}”。\n取消请发送“取消 ${proposal.token}”。`);
    }
    return this.queries.redact(lines.join('\n'));
  }

  async command(job: Job): Promise<{ text: string; proposal?: ShelfProposal } | undefined> {
    if (!/^(确认|取消)(?:\s|$)/u.test(job.text)) return;
    const match = /^(确认|取消)\s+([A-F0-9]{12})$/u.exec(job.text);
    if (!match || !job.parent_id || job.branch) return { text: '请回复最新预览并 @机器人，发送预览中的完整确认或取消口令。' };
    const proposal = this.store.previewProposal(job.parent_id);
    if (!proposal || proposal.conversationId !== job.conversation_id || proposal.ownerId !== job.sender_id || proposal.token !== match[2]) {
      return { text: '确认未执行。口令、发起人或预览消息不匹配，请使用本人当前提议的最新预览。' };
    }
    if (proposal.status === 'confirmed' || proposal.status === 'cancelled') return { text: this.render(proposal), proposal };
    if (proposal.status !== 'pending') return { text: '当前提议已取消或结果待核对，未发起新的提交。' };
    if (proposal.kind === 'ingest') {
      if (match[1] === '取消') proposal.status = 'cancelled';
      else if (Date.now() > proposal.expires) {
        proposal.token = randomBytes(6).toString('hex').toUpperCase();
        proposal.expires = Date.now() + 10 * 60 * 1000;
      } else {
        proposal.jobId = job.id;
        proposal.status = 'committing';
        this.store.saveProposal(proposal);
        try {
          proposal.changeset = await this.queries.ingest(proposal.request) as Data;
          proposal.status = 'confirmed';
        } catch { proposal.status = 'unknown'; }
      }
      this.store.saveProposal(proposal);
      return { text: this.render(proposal), proposal };
    }
    if (match[1] === '取消') {
      const current = await this.queries.readChangesets(proposal.libraryId, proposal.changeset!.id);
      if (current.status === 'confirmed') {
        this.preview(proposal, current);
        return { text: this.render(proposal), proposal };
      }
      proposal.status = 'cancelled'; this.store.saveProposal(proposal);
      return { text: this.render(proposal), proposal };
    }
    if (Date.now() > proposal.expires) {
      this.preview(proposal, await this.refresh(proposal));
      return { text: `原口令已过期，请重新查看。\n${this.render(proposal)}`, proposal };
    }
    let reviewed: Data;
    try { reviewed = await this.refresh(proposal); }
    catch (error) {
      const current = await this.queries.readChangesets(proposal.libraryId, proposal.changeset!.id);
      this.preview(proposal, current);
      return { text: `本次未提交，重新校验失败。${safeFailure(error).error.message}\n${this.render(proposal)}`, proposal };
    }
    if (reviewed.status === 'confirmed') {
      this.preview(proposal, reviewed);
      return { text: this.render(proposal), proposal };
    }
    if (reviewed.review_stale || fingerprint(reviewed) !== proposal.fingerprint || blocked(reviewed)) {
      this.preview(proposal, reviewed);
      return { text: `提议或目标已变化，请重新查看。\n${this.render(proposal)}`, proposal };
    }
    proposal.changeset = reviewed;
    proposal.status = 'committing';
    proposal.jobId = job.id;
    proposal.request = { library_id: proposal.libraryId, changeset_id: reviewed.id, revision: reviewed.revision,
      decisions: reviewed.items.map((item: Data) => item.decision), confirmed: true };
    this.store.saveProposal(proposal);
    try {
      await this.queries.submitShelf(proposal.request);
      this.preview(proposal, await this.queries.readChangesets(proposal.libraryId, reviewed.id));
    } catch (error) {
      if (error instanceof QueryError && error.code === 'shelf_rejected') {
        const draft = error.details ?? await this.queries.readChangesets(proposal.libraryId, reviewed.id);
        this.preview(proposal, record(draft));
        return { text: `本次未提交。${error.message}\n${this.render(proposal)}`, proposal };
      }
      proposal.status = 'unknown'; this.store.saveProposal(proposal);
      await this.recover(proposal);
    }
    return { text: this.render(proposal), proposal };
  }

  private async refresh(proposal: ShelfProposal): Promise<Data> {
    const current = await this.queries.readChangesets(proposal.libraryId, proposal.changeset!.id);
    if (current.status === 'confirmed') return current;
    if (current.revision !== proposal.changeset!.revision) return { ...current, review_stale: true };
    return this.queries.submitShelf({ library_id: proposal.libraryId, changeset_id: current.id,
      revision: current.revision, decisions: current.items.map((item: Data) => item.decision).filter(Boolean), confirmed: false });
  }

  async recover(proposal: ShelfProposal): Promise<void> {
    if (proposal.kind === 'ingest') {
      // Retrying an acknowledged-but-lost ingest could overwrite a newer edit.
      if (proposal.status === 'committing') proposal.status = 'unknown';
      this.store.saveProposal(proposal);
      return;
    }
    try {
      let draft: Data | undefined;
      if (proposal.changeset) {
        draft = await this.queries.readChangesets(proposal.libraryId, proposal.changeset.id);
      } else {
        for (let page = 1; ; page++) {
          const result = await this.queries.readChangesets(proposal.libraryId, undefined, page);
          const matches = result.items.filter((item: Data) => item.source?.agent_name === proposal.source);
          if (matches.length > 1) throw new QueryError('ambiguous_proposal', '发现重复提议，需核对');
          if (matches.length) { draft = matches[0]; break; }
          if (page * 100 >= result.total) break;
        }
      }
      if (draft) {
        if (proposal.request.changeset_id && draft.status === 'pending' && draft.revision === proposal.request.revision) {
          // A lost acknowledgement is retried only for the same immutable draft/revision.
          try { await this.queries.submitShelf(proposal.request); }
          catch (error) {
            if (error instanceof QueryError && error.code === 'shelf_rejected') {
              this.preview(proposal, record(error.details ?? draft));
              return;
            }
          }
          draft = await this.queries.readChangesets(proposal.libraryId, draft.id);
          if (draft.status !== 'confirmed' && draft.revision === proposal.request.revision) return;
        }
        this.preview(proposal, draft);
      }
    } catch { /* Keep the persisted uncertain state until the service can be read. */ }
  }
}
