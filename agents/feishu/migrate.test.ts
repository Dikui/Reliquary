import assert from 'node:assert/strict';
import test from 'node:test';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { loadConfig } from './config.js';
import { migrateState } from './migrate.js';
import { Store } from './store.js';
import { TOOL_NAMES } from './tools.js';

// Stage 4 medium spec. Synthetic MCP metadata replaces both endpoint identities.
test('离线迁移保留消息与分支并要求待确认提议重新核验', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'feishu-migration-'));
  const source = join(directory, 'old');
  const destination = join(directory, 'new');
  const scope = { appId: 'app', tenantKey: 'tenant', chatId: 'chat' };
  const store = new Store(source, scope);
  store.accept({ messageId: 'message', eventId: 'event', senderId: 'user', parentId: null, branch: false, text: '保留原问题' });
  const job = store.nextJob()!;
  store.route(job);
  store.started(job, 'child.jsonl', 'answer');
  store.finish(job, { text: '保留原答案', answerEntryId: 'answer', sessionFile: 'child.jsonl' });
  store.saveProposal({ id: 'proposal', ownerId: 'user', conversationId: job.conversation_id!, jobId: job.id,
    libraryId: 'library', status: 'pending', source: 'test', request: {}, token: 'code', expires: Date.now() + 600000, fingerprint: 'hash' });
  store.close();
  await mkdir(join(source, 'sessions'));
  const message = JSON.stringify({ type: 'message', id: 'answer', message: { role: 'assistant', content: [{ type: 'text', text: '保留原答案' }] } });
  let endpoint = '';
  const server = createServer(async (request, response) => {
    let body = '';
    for await (const chunk of request) body += chunk;
    const data = JSON.parse(body || '{}');
    if (!('id' in data)) { response.writeHead(202).end(); return; }
    let result: unknown = {};
    if (data.method === 'initialize') result = { protocolVersion: '2025-03-26', capabilities: { tools: {} },
      serverInfo: { name: 'test', version: '1' }, instructions: '测试规则' };
    if (data.method === 'tools/list') result = { tools: TOOL_NAMES.map(name => {
      const required: Record<string, string[]> = { search: ['query', 'libraries'], get_entry: ['id'], get_entity: ['id'],
        list_entities: ['library_id'], graph_search: ['libraries'], shelf_find: ['library_id', 'q'],
        shelf_get: ['library_id', 'node_id'], shelf_query: ['library_id'], shelf_guide: ['library_id'] };
      return { name, inputSchema: { type: 'object', properties: Object.fromEntries((required[name] ?? []).map(key => [key,
        key === 'libraries' ? { type: 'array', items: { type: 'string' } } : { type: 'string' }])) } };
    }) };
    if (data.method === 'tools/call') result = data.params.name === 'reliquary_guide'
      ? { content: [{ type: 'text', text: '- compare: test\n- entity-profile: test\n- shelf-expert: test' }] }
      : { structuredContent: { libraries: [{ id: 'library', directory: 'synthetic', entity_schema: { fields: [] } }] }, content: [] };
    response.writeHead(200, { 'content-type': 'application/json' }).end(JSON.stringify({ jsonrpc: '2.0', id: data.id, result }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  endpoint = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  const context = JSON.stringify({ type: 'custom', id: 'context', customType: 'reliquary_query_context',
    data: { version: 1, endpoint, libraries: ['library'] } });
  const parent = `${JSON.stringify({ type: 'session', id: 'parent', cwd: join(source, 'workspace') })}\n${context}\n${message}\n`;
  const child = `${JSON.stringify({ type: 'session', id: 'child', cwd: join(source, 'workspace'), parentSession: join(source, 'sessions/parent.jsonl') })}\n${context}\n${message}\n`;
  await writeFile(join(source, 'sessions/parent.jsonl'), parent);
  await writeFile(join(source, 'sessions/child.jsonl'), child);
  const config = loadConfig({ RELIQUARY_API_URL: endpoint + '/new', RELIQUARY_API_KEY: 'test',
    RELIQUARY_LIBRARY_IDS: 'library', AGENT_DATA_DIR: destination, MODEL_API: 'openai-codex-responses' });
  const options = { source, destination, runtimeRoot: '/data', sourceEndpoint: endpoint, config, scope };
  try {
    assert.equal((await migrateState(options)).sessions, 2);
    assert.equal(await readFile(join(source, 'sessions/child.jsonl'), 'utf8'), child);
    const migrated = (await readFile(join(destination, 'sessions/child.jsonl'), 'utf8')).trim().split('\n');
    assert.equal(migrated.at(-1), message);
    assert.equal(JSON.parse(migrated[0]).parentSession, '/data/sessions/parent.jsonl');
    assert.equal(JSON.parse(migrated[1]).data.endpoint, endpoint + '/new');
    const db = new DatabaseSync(join(destination, 'feishu.sqlite'), { readOnly: true });
    assert.equal(db.prepare('SELECT text FROM jobs').get()!.text, '保留原问题');
    assert.equal(JSON.parse(String(db.prepare('SELECT data FROM shelf_proposals').get()!.data)).expires, 0);
    db.close();
    await assert.rejects(migrateState(options), /目标目录已存在/);
  } finally { server.closeAllConnections(); server.close(); await rm(directory, { recursive: true, force: true }); }
});
