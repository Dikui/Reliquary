import type { ToolDefinition } from '@earendil-works/pi-coding-agent';
import { Type, type Static } from 'typebox';
import { Value } from 'typebox/value';
import type { Config } from './config.js';
import { QueryError, record } from './output.js';
import { safeFailure, type QueryTools } from './tools.js';
import type { FeishuTransport } from './feishu.js';
import type { Job, Store } from './store.js';
import type { ShelfEditor } from './shelf.js';

const attachmentSchema = Type.Object({
  action: Type.Union(['list', 'read', 'upload', 'import', 'changesets'].map(value => Type.Literal(value))),
  message_id: Type.Optional(Type.String()), library_id: Type.Optional(Type.String()),
  plan_key: Type.Optional(Type.String()), file_date: Type.Optional(Type.String()),
  worksheet: Type.Optional(Type.String()), offset: Type.Optional(Type.Integer({ minimum: 0 })),
  limit: Type.Optional(Type.Integer({ minimum: 1, maximum: 200 })),
  page: Type.Optional(Type.Integer({ minimum: 1 })), changeset_id: Type.Optional(Type.String()),
}, { additionalProperties: false });

export function attachmentTool(config: Config, queries: QueryTools, store: Store, job: Job,
  transport: Pick<FeishuTransport, 'download'>, shelf: ShelfEditor): ToolDefinition {
  return {
    name: 'attachment', label: '会话附件与货架导入',
    description: 'list 列出当前发送人在本会话提供的文件；read 读取 UTF-8 文本或完整 xlsx 盘点（可指定 worksheet、offset、limit 分页）；upload 暂存货架原件；import 按已确认方案生成待确认变更集；changesets 分页读取已有批次。先 list 取得 message_id，写入目标使用 library_id，import 需要 plan_key 和 file_date。',
    parameters: attachmentSchema,
    execute: async (_id, value) => {
      try {
        if (!Value.Check(attachmentSchema, value)) throw new QueryError('invalid_arguments', '附件参数不符合契约');
        const input = value as Static<typeof attachmentSchema>;
        const files = store.attachments(job);
        if (input.action === 'list') return result(files.map(({ messageId, name }) => ({ message_id: messageId, name })));
        if (!input.library_id || !config.libraries.includes(input.library_id)) throw new QueryError('library_scope', '请指定配置范围内的资料库');
        if (input.action === 'changesets') return result(queries.projectChangeset(await queries.readChangesets(input.library_id, input.changeset_id, input.page ?? 1, true)).data);
        const file = files.find(file => file.messageId === input.message_id);
        if (!file) throw new QueryError('attachment_scope', '附件必须由当前发送人在本会话中提供');
        const data = await transport.download(file.messageId, file.fileKey);
        const offset = input.offset ?? 0;
        const limit = input.limit ?? 100;
        if (input.action === 'read' && !file.name.toLowerCase().endsWith('.xlsx')) {
          const content = new TextDecoder('utf-8', { fatal: true }).decode(data);
          const characters = [...content];
          return result({ name: file.name, content: characters.slice(offset, offset + limit * 100).join(''),
            offset, total: characters.length, next_offset: offset + limit * 100 < characters.length ? offset + limit * 100 : null });
        }
        if (input.action === 'import' && !input.plan_key) throw new QueryError('invalid_arguments', '导入需要已经确认的 plan_key');
        if (input.action === 'import' && store.activeProposal(job.conversation_id!, job.sender_id)) {
          throw new QueryError('proposal_pending', '请先处理当前待确认提议');
        }
        const form = new FormData();
        form.append('file', new Blob([new Uint8Array(data)]), file.name);
        if (input.plan_key) form.append('plan_key', input.plan_key);
        if (input.file_date) form.append('file_date', input.file_date);
        const path = input.action === 'upload' ? 'entity-attachments'
          : input.action === 'import' ? 'shelf/imports' : 'shelf/imports/inspect';
        const response = await fetch(`${config.apiURL}/api/v1/libraries/${encodeURIComponent(input.library_id)}/${path}`, {
          method: 'POST', body: form, headers: { Authorization: `Bearer ${config.apiKey}` },
          redirect: 'error', signal: AbortSignal.timeout(config.timeoutMs),
        });
        const body = record(await response.json());
        if (!response.ok || body.success !== true) throw new QueryError('attachment_failed', '附件操作失败，请核对文件和导入方案');
        if (input.action === 'import') await shelf.adopt(job, record(body.data));
        if (input.action === 'upload') return result(body.data);
        if (input.action === 'import') return result(queries.projectChangeset(body.data).data);
        const sheets = record(record(body.data).Sheets);
        const names = Object.keys(sheets);
        if (!input.worksheet) return result({ name: file.name, worksheets: names.map(name => {
          const sheet = record(sheets[name]);
          return { name, cells: Object.keys(record(sheet.Cells)).length, hidden: sheet.Hidden, merges: sheet.Merges,
            hidden_rows: sheet.HiddenRows, hidden_columns: sheet.HiddenCols, drawing: sheet.Drawing,
            conditional_formatting: sheet.ConditionalFormatting };
        }) });
        if (!(input.worksheet in sheets)) throw new QueryError('invalid_worksheet', '工作表不存在');
        const sheet = record(sheets[input.worksheet]);
        const cells = Object.entries(record(sheet.Cells));
        return result({ name: input.worksheet, ...sheet, Cells: Object.fromEntries(cells.slice(offset, offset + limit)),
          total: cells.length, next_offset: offset + limit < cells.length ? offset + limit : null });
      } catch (error) {
        const output = safeFailure(error);
        return { content: [{ type: 'text', text: JSON.stringify(output) }], details: output, isError: true };
      }
    },
  };

  function result(data: unknown) {
    const text = queries.redact(JSON.stringify({ ok: true, data }));
    if (Buffer.byteLength(text) > 128 * 1024) throw new QueryError('result_too_large', '请减小 limit 或指定工作表分页读取');
    const output = JSON.parse(text);
    return { content: [{ type: 'text' as const, text }], details: output };
  }
}
