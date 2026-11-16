import assert from 'node:assert/strict';
import test from 'node:test';
import { McpClient, McpHttpError } from '@earendil-works/pi-mcp';
import { createInMemoryTransportPair } from '@earendil-works/pi-mcp/testing';
import { loadConfig } from './config.js';
import { QueryTools, TOOL_NAMES, safeFailure } from './tools.js';
import { createRedactor, OutputProjector } from './output.js';
import { readFileSync, readdirSync } from 'node:fs';

const workflows = Object.fromEntries(readdirSync('../../skills/reliquary/workflows').filter(name => name.endsWith('.md'))
  .map(name => [name.slice(0, -3), readFileSync(`../../skills/reliquary/workflows/${name}`, 'utf8')]));
const extraTools = ['list_topics', 'list_follows', 'follow_feed', 'check_urls', 'list_content_checks', 'get_content_check'];

const config = loadConfig({ RELIQUARY_API_URL: 'http://localhost:8080', RELIQUARY_API_KEY: 'synthetic-library-secret',
  RELIQUARY_LIBRARY_IDS: 'library', AGENT_DATA_DIR: '/tmp/reliquary-unit', MODEL_API: 'openai-codex-responses' });
const library = { id: 'library', name: '演示库', shelf_enabled: true,
  entity_schema: { kinds: [], fields: [{ key: 'energy', type: 'number', name: '能量', kinds: ['model'], product_line_ids: ['line'] }] },
  shelf_config: { regions: [{ key: 'eu', name: '欧洲' }], parsers: [{ authorization: 'synthetic-parser-secret' }] } };
const entity = { id: 'model', library_id: 'library', canonical_name: '演示型号', kind: 'model',
  attributes: { energy: [{ value: { min: 40, max: 50 }, conditions: [{ module_type: 'battery', part_ids: ['part'] }] }], authorization: 'hidden', unknown: { password: 'hidden' } } };
const schemas: Record<string, object> = {
  search: { query: { type: 'string' }, libraries: { type: 'array', items: { type: 'string' } } },
  get_entry: { id: { type: 'string' } }, get_entity: { id: { type: 'string' } },
  list_entities: { library_id: { type: 'string' } }, graph_search: { libraries: { type: 'array', items: { type: 'string' } } },
  shelf_find: { library_id: { type: 'string' }, q: { type: 'string' } },
  shelf_get: { library_id: { type: 'string' }, node_id: { type: 'string' } },
  shelf_query: { library_id: { type: 'string' } }, shelf_guide: { library_id: { type: 'string' } },
  list_topics: { library_id: { type: 'string' } },
};

// Synthetic MCP transport replaces Reliquary. No network or business writes.
test('查询工具校验范围并过滤文本、结构化和错误输出', async () => {
  const pair = createInMemoryTransportPair();
  await pair.server.start();
  const calls: string[] = [];
  pair.server.onMessage(async message => {
    if (!('id' in message) || !('method' in message)) return;
    let result: unknown;
    if (message.method === 'initialize') result = { protocolVersion: '2025-03-26', capabilities: { tools: {} }, serverInfo: { name: 'fixture', version: '1' }, instructions: '只依据资料回答' };
    if (message.method === 'tools/list') result = { tools: [...TOOL_NAMES, ...extraTools, 'unrelated_tool'].map(name => ({ name,
      annotations: { readOnlyHint: true },
      inputSchema: { type: 'object', properties: schemas[name] ?? {}, required: Object.keys(schemas[name] ?? {}) } })) };
    if (message.method === 'tools/call') {
      const params = message.params as { name: string; arguments: Record<string, unknown> };
      calls.push(params.name);
      if (params.name === 'reliquary_guide') result = { content: [{ type: 'text', text: params.arguments.workflow
        ? workflows[String(params.arguments.workflow)] : Object.keys(workflows).map(name => `- ${name}: 流程`).join('\n') }] };
      else {
        let data: unknown = { libraries: [library, { id: 'foreign', entity_schema: {} }] };
        if (params.name === 'get_entry') data = { library_id: params.arguments.id === 'foreign' ? 'foreign' : 'library',
          header: { id: 'entry', title: '正文' }, body: '事实 synthetic-parser-secret', offset: 0, next_offset: 10 };
        if (params.name === 'list_topics') data = { topics: [{ id: 'topic', library_id: 'library', name: '主题' }] };
        if (params.name === 'list_follows') data = { follows: [{ target_type: 'topic', target_id: 'topic', name: '主题' }, { target_type: 'topic', target_id: 'foreign', name: '越界主题' }] };
        if (params.name === 'follow_feed') data = { results: [{ library_id: 'library', title: '动态' }], next_cursor: 'next' };
        if (params.name === 'get_content_check') data = { id: 'run', targets: ['entry', 'foreign'], results: [
          { entry_id: 'entry', findings: [{ kind: 'duplicate', evidence: [{ entry_id: 'entry', quote: '事实' }] }] },
          { entry_id: 'foreign', findings: [{ kind: 'duplicate', evidence: [{ entry_id: 'foreign', quote: '越界' }] }] },
        ] };
        if (params.name === 'shelf_get') data = { entity, as_of: '2026-11-15T00:00:00Z', recorded: true, facts: [
          { entity_id: 'model', field: 'energy', value: { min: 40, max: 50, password: 'hidden' },
            conditions: [{ module_type: 'battery', part_ids: ['part'] }], origin: 'component',
            evidence: { snapshot_id: 'snapshot', effective_at: '2026-01-01', recorded_at: '2026-02-01', source: { worksheet: '型号', cells: ['A2'], authorization: 'hidden' } } },
        ] };
        result = { content: [{ type: 'text', text: JSON.stringify(data) }], structuredContent: data };
        if (params.arguments.id === 'error') result = { isError: true, content: [{ type: 'text', text: 'Authorization: synthetic-library-secret' }], structuredContent: { password: 'raw-error' } };
      }
    }
    await pair.server.send({ jsonrpc: '2.0', id: message.id, result });
  });
  const client = new McpClient({ name: 'test', version: '1' });
  await client.connect(pair.client);
  const tools = new QueryTools(config, client);
  try {
    await tools.initialize();
    assert.equal(tools.definitions.length, TOOL_NAMES.length + extraTools.length);
    assert.ok(!tools.definitions.some(tool => tool.name === 'unrelated_tool'));
    for (const workflow of Object.keys(workflows)) {
      const output = await tools.call('reliquary_guide', { workflow });
      assert.equal(output.ok, true);
      assert.ok(JSON.stringify(output).includes(workflow));
    }
    const before = calls.length;
    assert.equal((await tools.call('shelf_submit', {})).ok, false);
    assert.equal((await tools.call('search', { query: 'x', libraries: ['foreign'] })).ok, false);
    assert.equal((await tools.call('shelf_query', { library_id: 'library', confirmed: true })).ok, false);
    assert.equal(calls.length, before);
    assert.equal((await tools.call('get_entry', { id: 'foreign' })).ok, false);
    const results = await Promise.all([
      tools.call('list_libraries', {}), tools.call('get_entry', { id: 'entry' }),
      tools.call('shelf_get', { library_id: 'library', node_id: 'model' }), tools.call('get_entry', { id: 'error' }),
      tools.call('reliquary_guide', {}),
    ]);
    const serialized = JSON.stringify(results);
    assert.doesNotMatch(serialized, /synthetic-.*-secret|raw-error|hidden/);
    assert.match(serialized, /write-report|shelf-import|topic-brief|review-check/);
    assert.match(serialized, /snapshot|A2|next_offset|欧洲|"min":40|component/);
    assert.equal(results[3].ok, false);
    assert.doesNotMatch(JSON.stringify(await tools.call('list_follows', {})), /越界主题/);
    assert.match(JSON.stringify(await tools.call('list_topics', { library_id: 'library' })), /主题/);
    assert.match(JSON.stringify(await tools.call('follow_feed', {})), /next_cursor/);
    assert.doesNotMatch(JSON.stringify(await tools.call('get_content_check', {})), /越界|foreign/);
    const definition = tools.definitions.find(tool => tool.name === 'get_entry')!;
    const result = await definition.execute('id', { id: 'entry' }, undefined, undefined, {} as never);
    assert.doesNotMatch(JSON.stringify(result), /synthetic-parser-secret/);
    const controller = new AbortController(); controller.abort();
    assert.equal((await tools.call('list_libraries', {}, controller.signal)).ok, false);
  } finally { await tools.close(); }
});

test('投影保留查询证据和完整货架契约并过滤凭据', () => {
  const projector = new OutputProjector(['library'], createRedactor(['secret-value']));
  projector.project('list_libraries', { libraries: [library] });
  const cases: Record<string, unknown> = {
    search: { results: [{ library_id: 'library', entry_id: 'entry', snippets: [{ content: '证据', locations: [{ heading_path: ['规格'], paragraph: 2, start: 0, end: 2 }] }], similar_reports: [{ library_id: 'foreign', title: 'outside' }] }], vector_unavailable: true },
    list_entities: { entities: [entity] }, get_entity: { entity, library_groups: [{ library_id: 'foreign', entries: [entity] }] },
    graph_search: { entities: [entity], edges: [{ library_id: 'library', sources: [{ entry_id: 'entry', library_id: 'library', evidence: [{ locations: [{ start: 0, end: 2 }] }] }] }], pending_sync: { global: 2, involved: 1 } },
    shelf_find: { items: [entity], total: 1, truncated: false },
    shelf_query: { items: [{ entity, matching_conditions: [[{ module_type: 'battery', part_ids: ['part'] }]] }], total: 1, parameter_missing: [{ field: 'energy', models: 2 }] },
    shelf_changeset: { id: 'draft', library_id: 'library', revision: 1, status: 'pending', source: { type: 'agent', agent_name: 'test' }, items: [{ id: 'change', entity_id: 'model', operation: { id: 'change', type: 'update_entity', patch: { revision: 1, attributes: entity.attributes, password: 'outside' } }, before: entity, after: entity, conflicts: [{ type: 'uncertain', message: 'secret-value', details: { authorization: 'outside' } }], decision: { item_id: 'change', action: 'apply' } }] },
    shelf_guide: { library_id: 'library', rules: 'Bearer secret-value', values: { product_lines: [entity] }, import_plans: [{ password: 'outside' }] },
  };
  for (const [name, value] of Object.entries(cases)) {
    const output = JSON.stringify(projector.project(name, { ...recordForTest(value), authorization: 'outside' }));
    assert.doesNotMatch(output, /outside|hidden|secret-value|authorization/);
    if (!['shelf_changeset', 'shelf_guide'].includes(name)) assert.doesNotMatch(output, /unknown/);
  }
  const projected = projector.project('shelf_query', cases.shelf_query);
  assert.deepEqual((projected.data as any).items[0].matching_conditions, [[{ module_type: 'battery', part_ids: ['part'] }]]);
  const plan = projector.project('shelf_guide', { library_id: 'library', import_plans: [{ key: 'map', definition: { sheets: [] } }] });
  assert.match(JSON.stringify(plan), /import_plans/);
  assert.doesNotMatch(JSON.stringify(safeFailure(new McpHttpError(401, 'secret-value', 'secret-value'))), /secret-value/);
});

function recordForTest(value: unknown) { return value as Record<string, unknown>; }
