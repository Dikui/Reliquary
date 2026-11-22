import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { loadConfig } from './config.js';
import { modelRuntime } from './pi.js';
import { Store, type Outgoing } from './store.js';
import type { QueryTools } from './tools.js';
import { PiService, Worker } from './worker.js';

// One real Pi runtime, synthetic model responses and platform receipts.
test('双渠道收到相同消息编号时，各自回复并只续接本渠道的 Pi 历史', async t => {
  const dir = await mkdtemp(join(tmpdir(), 'reliquary-channels-'));
  const config = loadConfig({ AGENT_DATA_DIR: dir, RELIQUARY_API_URL: 'http://localhost', RELIQUARY_API_KEY: 'fixture',
    RELIQUARY_LIBRARY_IDS: 'library', MODEL_API: 'openai-completions', MODEL_BASE_URL: 'http://localhost/v1',
    MODEL_API_KEY: 'fixture', MODEL_NAME: 'fixture', MODEL_CONTEXT_WINDOW: '64000', MODEL_MAX_TOKENS: '2000' });
  const questions: string[][] = [];
  t.mock.method(globalThis, 'fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    const body = await request.json();
    const messages: { role: string; content: string | { text: string }[] }[] = body.messages;
    questions.push(messages.filter(m => m.role === 'user').map(m =>
      typeof m.content === 'string' ? m.content : m.content.map(block => block.text).join('')));
    const chunk = JSON.stringify({ id: 'answer', object: 'chat.completion.chunk', created: 1, model: 'fixture',
      choices: [{ index: 0, delta: { role: 'assistant', content: '合成答复' }, finish_reason: 'stop' }] });
    return new Response(`data: ${chunk}\n\ndata: [DONE]\n\n`, { headers: { 'content-type': 'text/event-stream' } });
  });
  const tools = { instructions: '', definitions: [], redact: (s: string) => s, protectSecrets() {} } as unknown as QueryTools;
  const shared = await modelRuntime(config, tools);
  const scope = { appId: 'bot', tenantKey: 'tenant', chatId: 'group' };
  const makeChannel = (name: string) => {
    const dataDir = join(dir, name);
    const store = new Store(dataDir, scope, name);
    const replies: Outgoing[] = [];
    const worker = new Worker({ ...config, dataDir }, tools, store, {
      async reply(row) { replies.push(row); return { messageId: 'reply-' + row.reply_to }; },
      async reconcile() { return undefined; },
    }, () => {}, undefined, async () => shared);
    return { store, worker, replies };
  };
  const channels = ['alpha', 'beta'].map(makeChannel);
  const service = new PiService(channels.map(c => c.worker), () => {});
  try {
    for (const turn of ['first', 'next']) {
      for (const { store } of channels) {
        store.accept({ messageId: turn, eventId: turn, senderId: 'user',
          text: store.channel + ' ' + turn, parentId: turn === 'next' ? 'reply-first' : null, branch: false });
      }
      await service.wake();
      await Promise.all(channels.map(c => c.worker.flushOutbox()));
    }
    for (const { store, replies } of channels) {
      assert.deepEqual(replies.map(r => r.reply_to), ['first', 'next']);
      const histories = questions.filter(q => q[0] === store.channel + ' first');
      assert.deepEqual(histories, [[store.channel + ' first'], [store.channel + ' first', store.channel + ' next']]);
    }
  } finally {
    await service.close();
    for (const { store } of channels) store.close();
    await rm(dir, { recursive: true, force: true });
  }
});
