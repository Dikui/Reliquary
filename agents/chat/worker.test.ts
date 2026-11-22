import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Store, type IncomingMessage, type Outgoing } from './store.js';
import { Worker } from './worker.js';
import { DeliveryError } from './transport.js';
import type { Config } from './config.js';
import type { QueryTools } from './tools.js';

const scope = { appId: 'app', tenantKey: 'tenant', chatId: 'chat' };
function incoming(id: string, parent: string | null = null, branch = false): IncomingMessage {
  return { messageId: id, parentId: parent, eventId: id, senderId: 'user', text: id, branch };
}

// Stage 2 contract: SQLite state survives restart; external delivery is synthetic.
test('持久化队列去重，回复续接原会话，历史答案派生独立会话', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'feishu-store-'));
  let store = new Store(directory, scope);
  try {
    assert.throws(() => new Store(directory, scope), /已有运行中/);
    assert.equal(store.accept(incoming('first')), true);
    assert.equal(store.accept(incoming('first')), false);
    store.close();
    store = new Store(directory, scope);
    const first = store.nextJob()!;
    const conversation = store.route(first);
    store.started(first, 'first.jsonl', null);
    store.finish(first, { text: '旧答案', sessionFile: 'first.jsonl', answerEntryId: 'old-answer' });
    store.sent(store.dueOutgoing()[0], 'old-reply');
    store.accept(incoming('follow', 'old-reply'));
    const follow = store.nextJob()!;
    assert.equal(store.route(follow).id, conversation.id);
    store.started(follow, 'first.jsonl', 'old-answer');
    store.finish(follow, { text: '新答案', sessionFile: 'first.jsonl', answerEntryId: 'new-answer' });
    store.sent(store.dueOutgoing()[0], 'new-reply');
    store.accept(incoming('branch', 'old-reply', true));
    const branch = store.nextJob()!;
    const child = store.route(branch);
    assert.notEqual(child.id, conversation.id);
    assert.equal(child.branch_entry, 'old-answer');
    assert.equal(child.parent_file, 'first.jsonl');
    store.finish(branch, { text: '派生答案' });
    store.accept(incoming('separate'));
    assert.notEqual(store.route(store.nextJob()!).id, conversation.id);
    store.close();
    store = new Store(directory, scope);
    assert.equal(store.runningJobs().length, 1);
    store.close();
    assert.throws(() => new Store(directory, { ...scope, chatId: 'other' }), /绑定其他/);
  } finally { store.close(); await rm(directory, { recursive: true, force: true }); }
});

// State table: pending -> sending -> unknown -> sent. Unknown never gets a new UUID.
test('未知发送在重启后核对，已送达答案不会重复发送', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'feishu-outbox-'));
  let store = new Store(directory, scope);
  let sends = 0;
  const delivered = new Map<string, string>();
  const transport = {
    async reply(row: Outgoing) {
      sends++;
      delivered.set(row.uuid, `reply-${row.id}`);
      throw new DeliveryError('unknown', 'response_lost');
    },
    async reconcile(row: Outgoing) {
      const messageId = delivered.get(row.uuid);
      return messageId ? { messageId } : undefined;
    },
  };
  try {
    store.accept(incoming('question'));
    const job = store.nextJob()!;
    store.route(job);
    store.finish(job, { text: '答'.repeat(5000), sessionFile: 'session', answerEntryId: 'answer' });
    let worker = new Worker({} as Config, {} as QueryTools, store, transport, () => {});
    await worker.flushOutbox();
    assert.equal(sends, 1);
    assert.equal(store.db.prepare('SELECT status FROM outbox ORDER BY id LIMIT 1').get()?.status, 'unknown');
    store.close();
    store = new Store(directory, scope);
    store.db.exec('UPDATE outbox SET next_attempt=0');
    worker = new Worker({} as Config, {} as QueryTools, store, transport, () => {});
    await worker.flushOutbox();
    assert.equal(sends, 1);
    assert.equal(store.db.prepare('SELECT status FROM outbox ORDER BY id LIMIT 1').get()?.status, 'sent');
    await worker.flushOutbox();
    assert.equal(sends, 2);
    store.db.exec("UPDATE outbox SET first_attempt=1, next_attempt=0 WHERE status='unknown'");
    delivered.clear();
    await worker.flushOutbox();
    assert.equal(sends, 2);
    assert.equal(store.db.prepare("SELECT COUNT(*) AS count FROM outbox WHERE status='unknown'").get()?.count, 1);
  } finally { store.close(); await rm(directory, { recursive: true, force: true }); }
});
