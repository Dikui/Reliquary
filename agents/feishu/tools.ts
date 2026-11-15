import { McpClient, McpHttpError, McpTimeoutError, StreamableHttpTransport, type CallToolResult, type Tool } from '@earendil-works/pi-mcp';
import { Type, type TSchema } from 'typebox';
import { Value } from 'typebox/value';
import type { ToolDefinition } from '@earendil-works/pi-coding-agent';
import type { Config } from './config.js';
import { createRedactor, OutputProjector, QueryError, record, type Json } from './output.js';

export const TOOL_NAMES = [
  'list_libraries', 'reliquary_guide', 'search', 'get_entry', 'list_entities', 'get_entity',
  'graph_search', 'shelf_find', 'shelf_get', 'shelf_query', 'shelf_guide',
] as const;
const WORKFLOWS = ['compare', 'entity-profile', 'shelf-expert'];
const REQUIRED_PARAMETERS: Record<string, string[]> = {
  search: ['query', 'libraries'], get_entry: ['id'], list_entities: ['library_id'], get_entity: ['id'],
  graph_search: ['libraries'], shelf_find: ['library_id', 'q'], shelf_get: ['library_id', 'node_id'],
  shelf_query: ['library_id'], shelf_guide: ['library_id'],
};

export type ToolOutput =
  | { ok: true; data: Json; warnings: string[] }
  | { ok: false; error: { code: string; message: string; retryable: boolean } };

export function safeFailure(error: unknown): Extract<ToolOutput, { ok: false }> {
  if (error instanceof QueryError) return { ok: false, error: { code: error.code, message: error.message, retryable: error.retryable } };
  if (error instanceof McpHttpError && [401, 403].includes(error.status)) {
    return { ok: false, error: { code: 'authentication_failed', message: 'Reliquary 认证失败，请管理员检查密钥', retryable: false } };
  }
  if (error instanceof McpTimeoutError || error instanceof Error && ['TimeoutError', 'AbortError'].includes(error.name)) {
    return { ok: false, error: { code: 'interrupted', message: '查询超时或已取消', retryable: true } };
  }
  return { ok: false, error: { code: 'service_unavailable', message: '查询服务暂时不可用，请稍后重试', retryable: true } };
}

// Close typed objects while preserving explicit maps such as combination.
function closeSchema(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(closeSchema);
  if (!value || typeof value !== 'object') return value;
  const schema = Object.fromEntries(Object.entries(value).map(([key, child]) => [key, closeSchema(child)]));
  if (schema.type === 'object' && schema.properties && schema.additionalProperties === undefined) schema.additionalProperties = false;
  return schema;
}

function toolData(result: CallToolResult): unknown {
  if (result.isError) throw new QueryError('query_rejected', 'Reliquary 拒绝了本次查询，请核对目标、参数和业务时间');
  if (result.structuredContent !== undefined) return result.structuredContent;
  const text = result.content.filter(block => block.type === 'text').map(block => block.text).join('\n');
  try { return JSON.parse(text); }
  catch { throw new QueryError('invalid_response', '查询服务未返回结构化数据'); }
}

export class QueryTools {
  readonly redact: (text: string) => string;
  readonly definitions: ToolDefinition[] = [];
  instructions = '';
  private projector: OutputProjector;
  private schemas = new Map<string, TSchema>();
  private guideCatalog = '';
  private secrets: string[];
  private shelfSchema?: TSchema;

  constructor(private config: Config, private client: McpClient) {
    this.secrets = [config.apiKey, config.modelKey];
    this.redact = createRedactor(this.secrets);
    this.projector = new OutputProjector(config.libraries, this.redact);
  }

  protectSecrets(...values: string[]) {
    for (const value of values) {
      const token = value.replace(/^(Bearer|Basic)\s+/i, '');
      for (const secret of [value, token]) if (secret && !this.secrets.includes(secret)) this.secrets.push(secret);
    }
  }

  async initialize(): Promise<void> {
    this.instructions = this.redact(this.client.instructions ?? '');
    if (!this.instructions.trim()) throw new QueryError('missing_rules', 'Reliquary 未提供使用规则');
    const available = await this.client.listTools();
    const shelf = available.find(tool => tool.name === 'shelf_submit');
    if (shelf) this.shelfSchema = this.schema('shelf_submit', shelf);
    for (const name of TOOL_NAMES) {
      const remote = available.find(tool => tool.name === name);
      if (!remote) throw new QueryError('missing_tool', `Reliquary 缺少必需工具 ${name}`);
      const schema = this.schema(name, remote);
      this.schemas.set(name, schema);
      this.definitions.push({
        name, label: name, description: this.redact(remote.description ?? name), parameters: schema,
        execute: async (_id, params, signal) => {
          const output = await this.call(name, params, signal);
          return { content: [{ type: 'text', text: JSON.stringify(output) }], details: output, isError: !output.ok };
        },
      });
    }
    const libraries = await this.call('list_libraries', {});
    if (!libraries.ok) throw new QueryError(libraries.error.code, libraries.error.message);
    const guide = await this.client.callTool('reliquary_guide', {}, { timeoutMs: this.config.timeoutMs });
    if (guide.isError) throw new QueryError('missing_guide', '无法读取 Reliquary 指南目录');
    this.guideCatalog = this.redact(guide.content.filter(block => block.type === 'text')
      .flatMap(block => block.text.split('\n')).filter(line => WORKFLOWS.some(name => line.startsWith(`- ${name}:`))).join('\n'));
    if (this.guideCatalog.split('\n').length !== WORKFLOWS.length) throw new QueryError('missing_guide', '必需业务指南不完整');
  }

  private schema(name: string, remote: Tool): TSchema {
    if (name === 'reliquary_guide') {
      return Type.Object({ workflow: Type.Optional(Type.Union(WORKFLOWS.map(name => Type.Literal(name)))) }, { additionalProperties: false });
    }
    const schema = record(closeSchema(remote.inputSchema));
    const properties = record(schema.properties ?? {});
    for (const key of REQUIRED_PARAMETERS[name] ?? []) {
      if (!(key in properties)) throw new QueryError('tool_contract_changed', `${name} 参数契约已变化`);
    }
    if (schema.type !== 'object') throw new QueryError('tool_contract_changed', `${name} 参数契约无效`);
    // Raw JSON Schema is supported by TypeBox. Validate again at our public call boundary.
    return Type.Unsafe<Record<string, unknown>>({ ...schema, additionalProperties: false } as TSchema);
  }

  async call(name: string, params: unknown, signal?: AbortSignal): Promise<ToolOutput> {
    try {
      const schema = this.schemas.get(name);
      if (!schema || !Value.Check(schema, params)) throw new QueryError('invalid_arguments', '工具名称或参数不符合查询契约');
      const args = record(params);
      const targets = args.libraries ?? (args.library_id ? [args.library_id] : []);
      if (!Array.isArray(targets) || targets.some(id => !this.config.libraries.includes(String(id))) ||
          ('libraries' in args && targets.length === 0)) {
        throw new QueryError('library_scope', '只能查询管理员配置的目标库');
      }
      signal?.throwIfAborted();
      if (name === 'reliquary_guide') {
        if (!args.workflow) return { ok: true, data: { content: this.guideCatalog }, warnings: [] };
        const result = await this.client.callTool(name, args, { signal, timeoutMs: this.config.timeoutMs });
        if (result.isError) throw new QueryError('guide_unavailable', '业务指南读取失败');
        const content = this.redact(result.content.filter(block => block.type === 'text').map(block => block.text).join('\n'));
        if (!content.trim()) throw new QueryError('guide_unavailable', '业务指南为空');
        return { ok: true, data: { workflow: String(args.workflow), content }, warnings: [] };
      }
      const raw = toolData(await this.client.callTool(name, args, { signal, timeoutMs: this.config.timeoutMs }));
      if (name === 'list_libraries') {
        for (const library of (record(raw).libraries as unknown[] ?? []).map(record)) {
          if (!this.config.libraries.includes(String(library.id))) continue;
          const parsers = record(library.shelf_config ?? {}).parsers;
          if (Array.isArray(parsers)) this.protectSecrets(...parsers.map(parser => String(record(parser).authorization ?? '')));
        }
      }
      if (name === 'get_entry' || name === 'get_entity') {
        const response = record(raw);
        const library = name === 'get_entry' ? response.library_id : record(response.entity).library_id;
        if (typeof library !== 'string' || !this.config.libraries.includes(library)) {
          throw new QueryError('library_scope', '该记录不属于管理员配置的目标库');
        }
      }
      if (name === 'graph_search') {
        const libraries = record(raw).libraries;
        if (!Array.isArray(libraries) || libraries.some(id => !this.config.libraries.includes(String(id)))) {
          throw new QueryError('library_scope', '图谱响应超出管理员配置的目标库');
        }
      }
      const projected = this.projector.project(name, raw);
      if (Buffer.byteLength(JSON.stringify(projected)) > 128 * 1024) {
        throw new QueryError('result_too_large', '结果过大，请缩小范围、分页或分段读取');
      }
      return { ok: true, ...projected };
    } catch (error) { return safeFailure(error); }
  }

  async close(): Promise<void> { await this.client.close(); }

  proposalSchema(): TSchema {
    if (!this.shelfSchema) throw new QueryError('missing_tool', 'Reliquary 缺少货架变更集接口');
    const properties = record(record(this.shelfSchema).properties);
    const operation = structuredClone(properties.operations) as Record<string, any>;
    operation.items.properties.type.enum = ['create_entity', 'update_entity', 'add_alias', 'archive_entity',
      'save_relation', 'end_relation', 'set_sale_statuses', 'set_parameter_values', 'create_certification', 'update_certification'];
    for (const name of ['entry_id', 'excluded', 'relation_type', 'library', 'library_updated_at', 'document',
      'classification', 'version_id', 'reparse', 'import_plan', 'import_location']) delete operation.items.properties[name];
    delete operation.items.properties.entity.properties.auto_match;
    delete operation.items.properties.patch.properties.auto_match;
    const decisions = structuredClone(properties.decisions) as Record<string, any>;
    decisions.items.properties.replacement = operation.items;
    decisions.items.properties.action.enum = ['apply', 'discard', 'defer', 'replace', 'same_entity'];
    return Type.Unsafe({ type: 'object', additionalProperties: false, required: ['library_id'],
      properties: { library_id: properties.library_id, operations: operation, decisions } });
  }

  async submitShelf(args: Record<string, unknown>): Promise<Record<string, any>> {
    if (!this.shelfSchema || !Value.Check(this.shelfSchema, args) || !this.config.libraries.includes(String(args.library_id))) {
      throw new QueryError('invalid_proposal', '货架操作或目标库不符合契约');
    }
    const result = await this.client.callTool('shelf_submit', args, { timeoutMs: this.config.timeoutMs });
    const raw = result.structuredContent ?? JSON.parse(result.content.filter(block => block.type === 'text').map(block => block.text).join('\n'));
    if (result.isError) {
      const error = record(record(raw).error);
      const failure = new QueryError('shelf_rejected', this.redact(String(error.message ?? '货架操作被拒绝')), false, error.details);
      throw failure;
    }
    return record(raw);
  }

  async readChangesets(library: string, id?: string, page = 1): Promise<Record<string, any>> {
    if (!this.config.libraries.includes(library)) throw new QueryError('library_scope', '目标库超出配置范围');
    const path = `/api/v1/libraries/${encodeURIComponent(library)}/shelf/changesets`;
    const url = `${this.config.apiURL}${path}${id ? `/${encodeURIComponent(id)}` : `?source_type=agent&page=${page}&page_size=100`}`;
    const response = await fetch(url, { headers: { Authorization: `Bearer ${this.config.apiKey}` },
      redirect: 'error', signal: AbortSignal.timeout(this.config.timeoutMs) });
    if (!response.ok) throw new QueryError('changeset_unavailable', '变更集状态暂时无法读取', true);
    const text = await response.text();
    if (Buffer.byteLength(text) > 8 * 1024 * 1024) throw new QueryError('result_too_large', '变更集响应过大');
    const body = record(JSON.parse(text));
    if (body.success !== true) throw new QueryError('changeset_unavailable', '变更集状态读取失败', true);
    return record(body.data);
  }

  projectChangeset(raw: unknown) { return this.projector.project('shelf_changeset', raw); }

  async shelfEntity(library: string, id: string): Promise<Record<string, any>> {
    if (!this.config.libraries.includes(library)) throw new QueryError('library_scope', '目标库超出配置范围');
    const raw = record(toolData(await this.client.callTool('get_entity', { id }, { timeoutMs: this.config.timeoutMs })));
    const entity = record(raw.entity);
    if (entity.library_id !== library) throw new QueryError('library_scope', '引用节点必须属于同一货架库');
    return entity;
  }
}

export async function connectQueryTools(config: Config): Promise<QueryTools> {
  const url = `${config.apiURL}/mcp`;
  const client = new McpClient({ name: 'reliquary-query-demo', version: '1', requestTimeoutMs: config.timeoutMs });
  const transport = new StreamableHttpTransport({
    url, openGetStream: false, maxMessageBytes: 8 * 1024 * 1024,
    headers: { Authorization: `Bearer ${config.apiKey}` },
    fetch: async (input, init) => {
      if (String(input) !== url) throw new QueryError('endpoint_changed', '查询端点发生变化');
      const response = await fetch(input, { ...init, redirect: 'error' });
      const reader = response.body?.getReader();
      if (!reader) return response;
      const chunks: Uint8Array[] = [];
      let bytes = 0;
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        bytes += value.length;
        if (bytes > 8 * 1024 * 1024) {
          await reader.cancel();
          throw new QueryError('result_too_large', '服务响应过大，请缩小查询范围');
        }
        chunks.push(value);
      }
      return new Response(Buffer.concat(chunks), { status: response.status, statusText: response.statusText, headers: response.headers });
    },
  });
  try {
    await client.connect(transport);
    const tools = new QueryTools(config, client);
    await tools.initialize();
    return tools;
  } catch (error) {
    await client.close().catch(() => {});
    throw error;
  }
}
