import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Type } from 'typebox';
import { Store, type Job } from './store.js';
import { ShelfEditor } from './shelf.js';
import { createRedactor } from './output.js';
import type { QueryTools } from './tools.js';

// Stage 3 medium spec. The synthetic service replaces only remote changeset storage.
test('货架提议经本人当前口令确认且重复投递只写入一次', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'shelf-confirm-'));
  const scope = { appId: 'app', tenantKey: 'tenant', chatId: 'chat' };
  let store = new Store(directory, scope);
  let writes = 0;
  let loseResponse = false;
  let failBeforeCommit = false;
  let loseCreateResponse = false;
  let entity = { id: 'model', library_id: 'library', name: '合成型号', revision: 1,
    attributes: { sale_statuses: [{ region: 'eu', status: 'DEV' }, { region: 'jp', status: 'GA' }], current_stage: 'P1' } };
  const drafts = new Map<string, any>();
  const queries = {
    redact: createRedactor([]), proposalSchema: () => Type.Any(),
    shelfEntity: async (library: string) => { assert.equal(library, 'library'); return structuredClone(entity); },
    projectChangeset: (draft: any) => ({ data: structuredClone(draft), warnings: [] }),
    readChangesets: async (_lib: string, id?: string) => id ? structuredClone(drafts.get(id)) : { items: [...drafts.values()], total: drafts.size },
    submitShelf: async (request: any) => {
      if (request.confirmed && failBeforeCommit) throw new Error('connection interrupted before commit');
      let draft = request.changeset_id && drafts.get(request.changeset_id);
      if (!draft) {
        draft = { id: `draft-${drafts.size}`, library_id: request.library_id, source: request.source,
          revision: 1, status: 'pending', items: request.operations.map((operation: any) => ({
            id: operation.id, entity_id: 'model', operation, before: structuredClone(entity),
            after: { ...structuredClone(entity), attributes: { ...entity.attributes, sale_statuses: operation.sale_statuses } },
            conflicts: [], decision: request.decisions.find((d: any) => d.item_id === operation.id),
          })) };
        drafts.set(draft.id, draft);
        if (loseCreateResponse) { loseCreateResponse = false; throw new Error('draft response lost'); }
      } else {
        assert.equal(request.revision, draft.revision);
        assert.equal(draft.status, 'pending');
        draft.revision++;
        for (const decision of request.decisions) {
          const item = draft.items.find((item: any) => item.id === decision.item_id);
          item.decision = decision;
          if (decision.action === 'replace') {
            item.operation = decision.replacement;
            item.decision = { item_id: item.id, action: 'apply' };
            item.after.attributes.sale_statuses = decision.replacement.sale_statuses;
          }
        }
      }
      if (request.confirmed) {
        writes++;
        entity = { ...structuredClone(draft.items[0].after), revision: entity.revision + 1 };
        draft.status = 'confirmed';
        if (loseResponse) { loseResponse = false; throw new Error('response lost'); }
      }
      return structuredClone(draft);
    },
  } as unknown as QueryTools;
  const accept = (id: string, text = id, parentId: string | null = null, senderId = 'owner', branch = false): Job => {
    store.accept({ messageId: id, eventId: id, senderId, text, parentId, branch });
    const job = store.nextJob()!;
    store.route(job);
    return job;
  };
  let editor = new ShelfEditor(queries, store);
  const preview = (job: Job, proposal: any, message: string) => {
    store.finish(job, { text: editor.render(proposal), proposalId: proposal.id, sessionFile: 'session.jsonl', answerEntryId: 'answer' });
    store.sent(store.dueOutgoing().find(row => row.job_id === job.id)!, message);
  };
  try {
    const first = accept('request');
    let proposal = await editor.propose(first, { library_id: 'library', operations: [
      { type: 'set_sale_statuses', entity_id: 'model', revision: 1, sale_statuses: [{ region: 'eu', status: 'GA' }] },
    ] });
    assert.equal(writes, 0);
    assert.equal(entity.attributes.sale_statuses[0].status, 'DEV');
    assert.deepEqual(proposal.changeset!.items[0].after.attributes.sale_statuses,
      [{ region: 'jp', status: 'GA', conditions: [] }, { region: 'eu', status: 'GA', conditions: [] }]);
    const oldToken = proposal.token;
    preview(first, proposal, 'first-preview');
    const stranger = accept('stranger', `确认 ${oldToken}`, 'first-preview', 'other');
    assert.match((await editor.command(stranger))!.text, /不匹配/);
    store.finish(stranger, { text: '拒绝' });
    const fork = accept('fork', `确认 ${oldToken}`, 'first-preview', 'owner', true);
    assert.match((await editor.command(fork))!.text, /完整/);
    store.finish(fork, { text: '拒绝' });
    const edit = accept('edit', '修订', 'first-preview');
    proposal = await editor.propose(edit, { library_id: 'library', decisions: [{ item_id: proposal.changeset!.items[0].id,
      action: 'replace', replacement: { type: 'set_sale_statuses', entity_id: 'model', revision: 1,
        sale_statuses: [{ region: 'eu', status: 'EOM' }] } }] });
    assert.notEqual(proposal.token, oldToken);
    preview(edit, proposal, 'revised-preview');
    const wrongPreview = accept('new-code-old-preview', `确认 ${proposal.token}`, 'first-preview');
    assert.match((await editor.command(wrongPreview))!.text, /不匹配/);
    store.finish(wrongPreview, { text: '拒绝' });
    const stale = accept('old-code', `确认 ${oldToken}`, 'first-preview');
    assert.match((await editor.command(stale))!.text, /不匹配/);
    store.finish(stale, { text: '拒绝' });
    proposal.expires = 1; store.saveProposal(proposal);
    const expired = accept('expired', `确认 ${proposal.token}`, 'revised-preview');
    const renewed = (await editor.command(expired))!;
    assert.match(renewed.text, /过期/);
    proposal = renewed.proposal!; preview(expired, proposal, 'renewed-preview');
    drafts.get(proposal.changeset!.id).revision++;
    const concurrent = accept('concurrent', `确认 ${proposal.token}`, 'renewed-preview');
    const refreshed = (await editor.command(concurrent))!;
    assert.match(refreshed.text, /变化/);
    proposal = refreshed.proposal!; preview(concurrent, proposal, 'current-preview');
    const confirm = accept('confirm', `确认 ${proposal.token}`, 'current-preview');
    loseResponse = true;
    const confirmed = (await editor.command(confirm))!;
    assert.equal(confirmed.proposal!.status, 'confirmed');
    assert.equal(writes, 1);
    assert.equal(entity.attributes.sale_statuses.find(item => item.region === 'eu')!.status, 'EOM');
    assert.equal(entity.attributes.current_stage, 'P1');
    preview(confirm, confirmed.proposal, 'result');
    const repeated = accept('confirm-again', `确认 ${proposal.token}`, 'current-preview');
    assert.equal((await editor.command(repeated))!.proposal!.status, 'confirmed');
    assert.equal(writes, 1);
    store.finish(repeated, { text: '已确认' });
    const next = accept('cancel-request', '提议', 'result');
    const cancelled = await editor.propose(next, { library_id: 'library', operations: [{ type: 'set_sale_statuses',
      entity_id: 'model', revision: 2, sale_statuses: [] }] });
    preview(next, cancelled, 'cancel-preview');
    const cancel = accept('cancel', `取消 ${cancelled.token}`, 'cancel-preview');
    assert.equal((await editor.command(cancel))!.proposal!.status, 'cancelled');
    store.finish(cancel, { text: '已取消' });
    store.close(); store = new Store(directory, scope); editor = new ShelfEditor(queries, store);
    assert.equal(store.proposal(cancelled.id)!.status, 'cancelled');
    assert.equal(writes, 1);
    const recoverable = accept('recoverable', '恢复测试', 'result');
    const pending = await editor.propose(recoverable, { library_id: 'library', operations: [{ type: 'set_sale_statuses',
      entity_id: 'model', revision: entity.revision, sale_statuses: [{ region: 'eu', status: 'GA' }] }] });
    preview(recoverable, pending, 'recovery-preview');
    failBeforeCommit = true;
    const interrupted = accept('interrupted-confirm', `确认 ${pending.token}`, 'recovery-preview');
    assert.equal((await editor.command(interrupted))!.proposal!.status, 'unknown');
    assert.equal(writes, 1);
    store.close(); store = new Store(directory, scope); editor = new ShelfEditor(queries, store);
    failBeforeCommit = false;
    const restored = store.proposal(pending.id)!;
    await editor.recover(restored);
    assert.equal(restored.status, 'confirmed');
    assert.equal(writes, 2);
    await editor.recover(restored);
    assert.equal(writes, 2);
    const lost = accept('lost-draft', '提议响应丢失');
    loseCreateResponse = true;
    const countBefore = drafts.size;
    await assert.rejects(editor.propose(lost, { library_id: 'library', operations: [{ type: 'set_sale_statuses',
      entity_id: 'model', revision: entity.revision, sale_statuses: [{ region: 'eu', status: 'DEV' }] }] }));
    const recoveredDraft = store.proposalForJob(lost.id)!;
    assert.equal(recoveredDraft.status, 'unknown');
    await editor.recover(recoveredDraft);
    assert.equal(recoveredDraft.status, 'pending');
    assert.equal(drafts.size, countBefore + 1);
    assert.equal(writes, 2);
  } finally { store.close(); await rm(directory, { recursive: true, force: true }); }
});
