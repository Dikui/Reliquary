import assert from 'node:assert/strict';
import test from 'node:test';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { loadConfig } from './config.js';
import { QueryAgent } from './pi.js';
import { connectQueryTools, TOOL_NAMES } from './tools.js';
import { createRedactor } from './output.js';
import { Store } from './store.js';
import { Worker } from './worker.js';

// Customer installation uses the local subscription relay, never an OAuth token.
test('客户订阅模式通过本机转发完成工具调用并保留会话', async () => {
  const dataDir = await mkdtemp(join(tmpdir(), 'reliquary-customer-pi-'));
  const originalFetch = globalThis.fetch;
  const secrets = ['customer-local-key'];
  let requests = 0;
  let requestFailure: unknown;
  const queries = {
    instructions: '根据工具回答', redact: createRedactor(secrets), protectSecrets: (...values: string[]) => secrets.push(...values),
    definitions: TOOL_NAMES.map(name => ({ name, label: name, description: name,
      parameters: { type: 'object', properties: {} }, execute: async () => ({ content: [{ type: 'text', text: '资料库正常' }], details: {} }) })),
  };
  let agent: QueryAgent | undefined;
  try {
    globalThis.fetch = async (input, init) => { try {
      const request = input instanceof Request ? input : new Request(input, init);
      assert.equal(request.url, 'http://localhost/api/v1/agent/models/customer-model/responses');
      assert.equal(request.headers.get('authorization'), 'Bearer customer-local-key');
      const body = await request.json() as any;
      assert.equal(body.store, false);
      assert.equal(body.stream, true);
      assert.equal(body.max_output_tokens, undefined);
      assert.ok(!body.input.some((item: any) => item.role === 'system' || item.type === 'tool_search_call'));
      if (body.tools) assert.equal(body.tools[0].type, 'namespace');
      requests++;
      const item = requests === 1
        ? { type: 'function_call', id: 'fc_customer', call_id: 'call_customer', name: 'list_libraries', arguments: '{}', status: 'completed' }
        : { type: 'message', id: 'msg_customer', role: 'assistant', status: 'completed', content: [{ type: 'output_text', text: '连接正常', annotations: [] }] };
      if (requests === 2) assert.ok(body.input.some((entry: any) => entry.type === 'function_call_output'));
      const events = [{ type: 'response.created', response: { id: 'resp_customer' } },
        { type: 'response.output_item.added', output_index: 0, item },
        { type: 'response.output_item.done', output_index: 0, item },
        { type: 'response.completed', response: { id: 'resp_customer', status: 'completed', output: [item], usage: { input_tokens: 10, output_tokens: 2, total_tokens: 12 } } }];
      return new Response(events.map(event => `data: ${JSON.stringify(event)}\n\n`).join(''), { headers: { 'content-type': 'text/event-stream' } });
    } catch (error) { requestFailure = error; throw error; }
    };
    const config = loadConfig({ MODEL_SOURCE: 'chatgpt', MODEL_ID: 'customer-model', MODEL_NAME: 'gpt-6.1-sol',
      RELIQUARY_API_URL: 'http://localhost', RELIQUARY_API_KEY: 'customer-local-key', RELIQUARY_LIBRARY_IDS: 'library', AGENT_DATA_DIR: dataDir });
    agent = await QueryAgent.open(config, queries as never);
    const answer = await agent.ask('检查资料库').catch(error => { throw requestFailure ?? error; });
    assert.equal(answer.text, '连接正常');
    assert.equal(requests, 2);
    assert.ok(!(await readFile(agent.session.sessionFile!, 'utf8')).includes('customer-local-key'));
  } finally { globalThis.fetch = originalFetch; await agent?.close(); await rm(dataDir, { recursive: true, force: true }); }
});

// Synthetic HTTP model and MCP server verify the real SDK/transport lifecycle.
// Responses are scripted and do not measure the answer quality of a real model.
test('本地查询会话串行运行、恢复固定配置并从指定答案派生', async () => {
  const dataDir = await mkdtemp(join(tmpdir(), 'reliquary-pi-'));
  const requests: Record<string, any>[] = [];
  const toolCalls: string[] = [];
  const server = createServer(async (request, response) => {
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const body = JSON.parse(Buffer.concat(chunks).toString() || '{}');
    if (request.url === '/mcp') {
      assert.equal(request.headers.authorization, 'Bearer synthetic-api-key');
      if (!('id' in body)) { response.writeHead(202).end(); return; }
      let result: unknown;
      if (body.method === 'initialize') result = {
        protocolVersion: '2025-03-26', capabilities: { tools: {} },
        serverInfo: { name: 'synthetic-reliquary', version: '1' }, instructions: '只引用工具返回的事实。',
      };
      if (body.method === 'tools/list') result = { tools: TOOL_NAMES.map(name => {
        const properties: Record<string, object> = {};
        if (['search', 'graph_search'].includes(name)) properties.libraries = { type: 'array', items: { type: 'string' } };
        if (name === 'search') properties.query = { type: 'string' };
        if (['get_entry', 'get_entity'].includes(name)) properties.id = { type: 'string' };
        if (['list_entities', 'shelf_find', 'shelf_get', 'shelf_query', 'shelf_guide'].includes(name)) properties.library_id = { type: 'string' };
        if (name === 'shelf_find') properties.q = { type: 'string' };
        if (name === 'shelf_get') properties.node_id = { type: 'string' };
        return { name, inputSchema: { type: 'object', properties, required: Object.keys(properties) } };
      }) };
      if (body.method === 'tools/call') {
        const { name, arguments: args } = body.params;
        toolCalls.push(name);
        if (name === 'reliquary_guide') {
          result = { content: [{ type: 'text', text: args.workflow ? '按证据比较，遇到歧义先澄清。' : '- compare: 对比\n- entity-profile: 实体\n- shelf-expert: 货架' }] };
        } else {
          let data: unknown = {};
          if (name === 'list_libraries') data = { libraries: [{ id: 'library', name: '演示', entity_schema: { fields: [] }, shelf_config: {} }] };
          if (name === 'search') data = { results: [{ entry_id: 'entry', library_id: 'library', title: '规格', snippets: [{ content: '50 kWh', locations: [{ heading_path: ['参数'], paragraph: 1, start: 0, end: 6 }] }] }] };
          if (name === 'get_entry') data = { library_id: 'library', header: { id: 'entry', title: '规格' }, body: '50 kWh', offset: 0, total_length: 6 };
          if (name === 'shelf_query') data = { items: [], total: 0, truncated: false, as_of: '2026-11-15T00:00:00Z' };
          result = { content: [{ type: 'text', text: JSON.stringify(data) }], structuredContent: data };
        }
      }
      response.writeHead(200, { 'content-type': 'application/json' }).end(JSON.stringify({ jsonrpc: '2.0', id: body.id, result }));
      return;
    }
    assert.equal(request.url, '/v1/chat/completions');
    assert.equal(request.headers.authorization, 'Bearer synthetic-model-key');
    requests.push(body);
    const messages = body.messages as Record<string, any>[];
    const userIndex = messages.findLastIndex(message => message.role === 'user');
    const content = messages[userIndex].content;
    const question = typeof content === 'string' ? content : content.map((block: { text?: string }) => block.text ?? '').join('');
    if (question === '模型失败') {
      response.writeHead(401, { 'content-type': 'application/json' }).end(JSON.stringify({ error: { message: 'unknown-server-secret', type: 'authentication_error' } }));
      return;
    }
    if (question === '等待超时') return;
    const results = messages.slice(userIndex + 1).filter(message => message.role === 'tool');
    let next: { name: string; arguments: object } | undefined;
    const steps = question.includes('比较') ? [
      { name: 'reliquary_guide', arguments: { workflow: 'compare' } },
      { name: 'search', arguments: { libraries: ['library'], query: 'A' } },
      { name: 'search', arguments: { libraries: ['library'], query: 'B' } },
      { name: 'get_entry', arguments: { id: 'entry' } },
    ] : [{ name: question.includes('禁止') ? 'bash' : 'shelf_query', arguments: { library_id: 'library' } }];
    next = steps[results.length];
    let delta: object;
    if (next) delta = { tool_calls: [{ index: 0, id: `call-${results.length}`, type: 'function', function: { name: next.name, arguments: JSON.stringify(next.arguments) } }] };
    else delta = { content: question.includes('推荐') ? '请说明区域。' : '事实 50 kWh。资料 entry，参数第 1 段。synthetic-model-key' };
    response.writeHead(200, { 'content-type': 'text/event-stream' });
    const chunk = (delta: object, finish_reason: string | null) => `data: ${JSON.stringify({ id: 'synthetic', object: 'chat.completion.chunk', created: 1, model: 'fixture-model', choices: [{ index: 0, delta, finish_reason }] })}\n\n`;
    response.end(chunk({ role: 'assistant', ...delta }, null) + chunk({}, next ? 'tool_calls' : 'stop') + 'data: [DONE]\n\n');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address() as { port: number };
  const config = loadConfig({
    RELIQUARY_API_URL: `http://127.0.0.1:${address.port}`, RELIQUARY_API_KEY: 'synthetic-api-key', RELIQUARY_LIBRARY_IDS: 'library',
    MODEL_API: 'openai-completions', MODEL_BASE_URL: `http://127.0.0.1:${address.port}/v1`, MODEL_API_KEY: 'synthetic-model-key',
    MODEL_NAME: 'fixture-model', MODEL_CONTEXT_WINDOW: '64000', MODEL_MAX_TOKENS: '2000', AGENT_DATA_DIR: dataDir,
  });
  let queries: Awaited<ReturnType<typeof connectQueryTools>> | undefined;
  let agent: QueryAgent | undefined;
  let branch: QueryAgent | undefined;
  try {
    await mkdir(join(dataDir, 'workspace', '.pi'), { recursive: true });
    await writeFile(join(dataDir, 'workspace', 'AGENTS.md'), 'UNAUTHORIZED_CONTEXT');
    await writeFile(join(dataDir, 'workspace', '.pi', 'settings.json'), '{"defaultTools":["bash"]}');
    queries = await connectQueryTools(config);
    agent = await QueryAgent.open(config, queries);
    const clarification = await agent.ask('推荐型号');
    assert.match(clarification.text, /区域/);
    const [answer, comparison] = await Promise.all([agent.ask('欧洲'), agent.ask('比较 A 和 B')]);
    assert.match(comparison.text, /资料 entry/);
    assert.ok(comparison.evidence.length >= 4);
    assert.doesNotMatch(comparison.text, /synthetic-model-key/);
    const later = await agent.ask('后续问题');
    const sessionText = await readFile(join(dataDir, 'sessions', later.sessionFile), 'utf8');
    assert.doesNotMatch(sessionText, /synthetic-api-key|synthetic-model-key|UNAUTHORIZED_CONTEXT/);
    await agent.close();
    agent = await QueryAgent.open({ ...config, model: 'updated-model' }, queries, { sessionFile: answer.sessionFile });
    assert.deepEqual(agent.session.getActiveToolNames().sort(), [...TOOL_NAMES].sort());
    await agent.ask('/skill:unknown !echo 继续');
    assert.equal(requests.at(-1)?.model, 'updated-model');
    assert.ok(JSON.stringify(requests.at(-1)).includes('后续问题'));
    branch = await QueryAgent.open(config, queries, { sessionFile: answer.sessionFile, answerEntryId: answer.answerEntryId });
    assert.notEqual(branch.session.sessionId, agent.session.sessionId);
    await branch.ask('分支追问');
    const branchedRequest = JSON.stringify(requests.at(-1));
    assert.match(branchedRequest, /推荐型号|欧洲/);
    assert.doesNotMatch(branchedRequest, /后续问题|比较 A/);
    await agent.ask('禁止工具');
    assert.ok(!toolCalls.includes('bash'));
    assert.match(JSON.stringify(requests.at(-1)), /not found|not available|Unknown tool/i);
    assert.ok(requests.every(body => body.tools.length === TOOL_NAMES.length));
    // Stage 3: the host adds one proposal tool and persists its result for later turns.
    const maintained = await QueryAgent.open(config, queries, { sessionFile: later.sessionFile,
      proposalTool: { name: 'shelf_propose', label: 'proposal', description: 'fixture proposal',
        parameters: { type: 'object', properties: {} } as never,
        execute: async () => ({ content: [{ type: 'text', text: 'pending' }], details: {} }) }, shelfContext: '没有待确认提议' });
    assert.deepEqual(maintained.session.getCallableToolNames().sort(), [...TOOL_NAMES, 'shelf_propose'].sort());
    await maintained.close();
    const receipt = await QueryAgent.recordHost(config, later.sessionFile, 'host-result', '宿主已确认合成修改');
    const receiptBranch = await QueryAgent.open(config, queries, { sessionFile: receipt.sessionFile, answerEntryId: receipt.answerEntryId });
    await receiptBranch.ask('读取宿主结果');
    assert.match(JSON.stringify(requests.at(-1)), /宿主已确认合成修改/);
    await receiptBranch.close();

    // Stage 2 recovery uses the real Pi files and the same synthetic model/MCP transport.
    const scope = { appId: 'app', tenantKey: 'tenant', chatId: 'chat' };
    let store = new Store(dataDir, scope);
    try {
      const accept = (id: string, text: string, parentId: string | null = null, branch = false) =>
        store.accept({ messageId: id, eventId: id, senderId: 'user', parentId, branch, text });
      const transport = { reply: async (row: { id: number }) => ({ messageId: `sent-${row.id}` }), reconcile: async () => undefined };
      let worker = new Worker(config, queries, store, transport, () => {});
      accept('first', '推荐型号');
      await worker.runNextJob();
      await worker.flushOutbox();
      const firstReply = store.db.prepare('SELECT * FROM outbox ORDER BY id').get()!;
      accept('follow', '欧洲', String(firstReply.message_id));
      await worker.runNextJob();
      await worker.flushOutbox();
      const followReply = store.db.prepare('SELECT * FROM outbox ORDER BY id DESC').get()!;
      accept('derive', '分支追问', String(firstReply.message_id), true);
      await worker.runNextJob();
      assert.doesNotMatch(JSON.stringify(requests.at(-1)), /欧洲/);
      await worker.flushOutbox();
      // Crash after Pi completion but before the task transaction commits.
      accept('complete-before-crash', '完整答案', String(followReply.message_id));
      const recoverable = store.nextJob()!;
      const conversation = store.route(recoverable);
      const pendingAgent = await QueryAgent.open(config, queries, { sessionFile: conversation.session_file! });
      const checkpoint = pendingAgent.checkpoint();
      store.started(recoverable, checkpoint.sessionFile, checkpoint.baseLeaf);
      await pendingAgent.ask(recoverable.text, String(recoverable.id));
      await pendingAgent.close();
      const beforeRecovery = requests.length;
      store.close();
      store = new Store(dataDir, scope);
      worker = new Worker(config, queries, store, transport, () => {});
      await worker.recover();
      assert.equal(requests.length, beforeRecovery);
      assert.equal(store.db.prepare('SELECT status FROM jobs WHERE id=?').get(recoverable.id)?.status, 'succeeded');
      await worker.flushOutbox();
      // Interrupted turns preserve complete context but exclude partial prompts from the next turn.
      accept('interrupted', '模型失败', String(followReply.message_id));
      await worker.runNextJob();
      await worker.flushOutbox();
      assert.equal(store.db.prepare('SELECT status FROM jobs WHERE message_id=?').get('interrupted')?.status, 'interrupted');
      const interruptedReply = store.db.prepare('SELECT * FROM outbox ORDER BY id DESC').get()!;
      accept('continue', '恢复后继续', String(interruptedReply.message_id));
      await worker.runNextJob();
      assert.match(JSON.stringify(requests.at(-1)), /完整答案|欧洲/);
      assert.doesNotMatch(JSON.stringify(requests.at(-1)), /模型失败/);
      await worker.flushOutbox();
      // A crash before the first user entry has created the JSONL file remains recoverable.
      accept('empty-file', '新问题');
      const empty = store.nextJob()!;
      store.route(empty);
      const emptyAgent = await QueryAgent.open(config, queries);
      const emptyCheckpoint = emptyAgent.checkpoint();
      store.started(empty, emptyCheckpoint.sessionFile, emptyCheckpoint.baseLeaf);
      await emptyAgent.close();
      store.close();
      store = new Store(dataDir, scope);
      worker = new Worker(config, queries, store, transport, () => {});
      await worker.recover();
      assert.equal(store.db.prepare('SELECT status FROM jobs WHERE id=?').get(empty.id)?.status, 'interrupted');
    } finally { store.close(); }
    await assert.rejects(agent.ask('模型失败'), /本轮未完成/);
    assert.doesNotMatch(await readFile(join(dataDir, 'sessions', answer.sessionFile), 'utf8'), /unknown-server-secret/);
    await assert.rejects(QueryAgent.open({ ...config, libraries: ['foreign'] }, queries, { sessionFile: answer.sessionFile }), /目标库/);
    await assert.rejects(QueryAgent.open(config, queries, { sessionFile: answer.sessionFile, answerEntryId: 'unknown' }), /已完成的答案/);
    await branch.close();
    branch = await QueryAgent.open({ ...config, timeoutMs: 30 }, queries);
    await assert.rejects(branch.ask('等待超时'), /本轮未完成/);
    assert.match(await readFile(branch.session.sessionFile!, 'utf8'), /等待超时/);
    await assert.rejects(QueryAgent.open(config, queries, { sessionFile: '../auth.json' }));
    assert.ok((await readdir(join(dataDir, 'sessions'))).length >= 2);
  } finally {
    await branch?.close(); await agent?.close(); await queries?.close();
    server.closeAllConnections(); server.close();
    await rm(dataDir, { recursive: true, force: true });
  }
});

test('客户 API 缺少协议时拒绝启动，不回落到开发者 Codex 登录', () => {
  const base = { RELIQUARY_API_URL: 'http://localhost', RELIQUARY_API_KEY: 'key', RELIQUARY_LIBRARY_IDS: 'library',
    AGENT_DATA_DIR: '/data', MODEL_BASE_URL: 'https://model.invalid/v1', MODEL_API_KEY: 'synthetic-model-key', MODEL_NAME: 'model' };
  for (const env of [base, { ...base, MODEL_SOURCE: 'api' }]) {
    assert.throws(() => loadConfig(env), /MODEL_API/);
  }
});

test('Codex 订阅配置使用固定端点且缺少登录时明确失败', async () => {
  const dataDir = await mkdtemp(join(tmpdir(), 'reliquary-auth-'));
  try {
    const config = loadConfig({ RELIQUARY_API_URL: 'http://localhost', RELIQUARY_API_KEY: 'key',
      RELIQUARY_LIBRARY_IDS: 'library', AGENT_DATA_DIR: dataDir, MODEL_BASE_URL: 'https://ignored.invalid',
      MODEL_API: 'openai-codex-responses' });
    assert.equal(config.modelAPI, 'openai-codex-responses');
    assert.equal(config.modelURL, 'https://chatgpt.com/backend-api');
    await assert.rejects(QueryAgent.open(config, { redact: (text: string) => text } as never), /--login/);
  } finally { await rm(dataDir, { recursive: true, force: true }); }
});

test('Codex 订阅凭据通过 Pi Responses 协议使用且不进入会话', async () => {
  const dataDir = await mkdtemp(join(tmpdir(), 'reliquary-codex-'));
  const originalFetch = globalThis.fetch;
  const payload = Buffer.from(JSON.stringify({ 'https://api.openai.com/auth': { chatgpt_account_id: 'synthetic-account' } })).toString('base64');
  const access = `header.${payload}.signature`;
  const secrets = [access, 'synthetic-refresh'];
  const queries = {
    instructions: '只依据查询结果回答', redact: createRedactor(secrets),
    protectSecrets: (...values: string[]) => secrets.push(...values),
    definitions: TOOL_NAMES.map(name => ({ name, label: name, description: name,
      parameters: { type: 'object', properties: {} }, execute: async () => ({ content: [], details: {} }) })),
  };
  let agent: QueryAgent | undefined;
  let called = false;
  try {
    await writeFile(join(dataDir, 'auth.json'), JSON.stringify({ 'openai-codex': {
      type: 'oauth', access, refresh: 'synthetic-refresh', expires: Date.now() + 3600000,
    } }), { mode: 0o600 });
    globalThis.fetch = async (input, init) => {
      assert.equal(String(input), 'https://chatgpt.com/backend-api/codex/responses');
      assert.equal(new Headers(init?.headers).get('authorization'), `Bearer ${access}`);
      called = true;
      const item = { type: 'message', id: 'message', role: 'assistant', status: 'completed',
        content: [{ type: 'output_text', text: '已连接 synthetic-refresh', annotations: [] }] };
      const events = [
        { type: 'response.created', response: { id: 'response' } },
        { type: 'response.output_item.added', output_index: 0, item: { ...item, content: [] } },
        { type: 'response.output_item.done', output_index: 0, item },
        { type: 'response.completed', response: { id: 'response', status: 'completed', output: [item],
          usage: { input_tokens: 10, output_tokens: 2, total_tokens: 12 } } },
      ];
      return new Response(events.map(event => `data: ${JSON.stringify(event)}\n\n`).join(''), { headers: { 'content-type': 'text/event-stream' } });
    };
    const config = loadConfig({ RELIQUARY_API_URL: 'http://localhost', RELIQUARY_API_KEY: 'key', RELIQUARY_LIBRARY_IDS: 'library', AGENT_DATA_DIR: dataDir,
      MODEL_API: 'openai-codex-responses' });
    agent = await QueryAgent.open(config, queries as never);
    const answer = await agent.ask('连接检查');
    assert.ok(called);
    assert.match(answer.text, /已连接/);
    assert.doesNotMatch(await readFile(join(dataDir, 'sessions', answer.sessionFile), 'utf8'), /synthetic-refresh|signature/);
  } finally {
    await agent?.close(); globalThis.fetch = originalFetch;
    await rm(dataDir, { recursive: true, force: true });
  }
});

test('只读复用 Codex 缓存并读取续期后的令牌，过期时保持缓存原样', async () => {
  const dataDir = await mkdtemp(join(tmpdir(), 'reliquary-shared-auth-'));
  const path = join(dataDir, 'codex-auth.json');
  const originalFetch = globalThis.fetch;
  const token = (tag: string, expires: number) => `header.${Buffer.from(JSON.stringify({
    exp: expires, 'https://api.openai.com/auth': { chatgpt_account_id: 'synthetic-account' }, tag,
  })).toString('base64url')}.signature`;
  const first = token('first', Math.floor(Date.now() / 1000) + 3600);
  const second = token('renewed', Math.floor(Date.now() / 1000) + 7200);
  const secrets: string[] = [];
  const queries = {
    instructions: '只回答连接状态', redact: createRedactor(secrets),
    protectSecrets: (...values: string[]) => secrets.push(...values),
    definitions: TOOL_NAMES.map(name => ({ name, label: name, description: name,
      parameters: { type: 'object', properties: {} }, execute: async () => ({ content: [], details: {} }) })),
  };
  const save = async (access: string) => {
    const data = JSON.stringify({ auth_mode: 'chatgpt', tokens: { access_token: access, refresh_token: 'never-share-refresh' } });
    await writeFile(path, data, { mode: 0o600 });
    return data;
  };
  const seen: string[] = [];
  let agent: QueryAgent | undefined;
  try {
    await save(first);
    globalThis.fetch = async (_input, init) => {
      seen.push(new Headers(init?.headers).get('authorization')!);
      const item = { type: 'message', id: 'message', role: 'assistant', status: 'completed',
        content: [{ type: 'output_text', text: '已连接', annotations: [] }] };
      return new Response([
        { type: 'response.created', response: { id: 'response' } },
        { type: 'response.output_item.done', output_index: 0, item },
        { type: 'response.completed', response: { id: 'response', status: 'completed', output: [item] } },
      ].map(event => `data: ${JSON.stringify(event)}\n\n`).join(''), { headers: { 'content-type': 'text/event-stream' } });
    };
    const config = loadConfig({ RELIQUARY_API_URL: 'http://localhost', RELIQUARY_API_KEY: 'key',
      RELIQUARY_LIBRARY_IDS: 'library', AGENT_DATA_DIR: dataDir, CODEX_AUTH_FILE: path, MODEL_API: 'openai-codex-responses' });
    agent = await QueryAgent.open(config, queries as never);
    const answer = await agent.ask('连接检查');
    const renewed = await save(second);
    await agent.ask('继续检查');
    assert.deepEqual(seen, [`Bearer ${first}`, `Bearer ${second}`]);
    assert.equal(await readFile(path, 'utf8'), renewed);
    const expired = await save(token('expired', 1));
    await assert.rejects(QueryAgent.open(config, queries as never), /续期/);
    assert.equal(await readFile(path, 'utf8'), expired);
    assert.doesNotMatch(await readFile(join(dataDir, 'sessions', answer.sessionFile), 'utf8'), /signature|never-share-refresh/);
    assert.ok(!(await readdir(dataDir)).includes('auth.json'));
  } finally {
    await agent?.close(); globalThis.fetch = originalFetch;
    await rm(dataDir, { recursive: true, force: true });
  }
});
