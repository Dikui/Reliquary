import assert from 'node:assert/strict';
import test from 'node:test';
import { EventDispatcher, LoggerLevel } from '@larksuiteoapi/node-sdk';
import { loadFeishuConfig } from './config.js';
import { DeliveryError, FeishuTransport, parseIncomingMessage } from './feishu.js';
import { splitReply, type Outgoing } from './store.js';

const config = { appId: 'app', appSecret: 'secret', tenantKey: 'tenant', chatId: 'chat' };
function event(text: string, parentId?: string) {
  return { schema: '2.0', header: { app_id: 'app', tenant_key: 'tenant', event_id: 'event', event_type: 'im.message.receive_v1' },
    event: { sender: { sender_type: 'user', sender_id: { open_id: 'user' } }, message: {
      message_id: 'incoming', chat_id: 'chat', chat_type: 'group', message_type: 'text', parent_id: parentId,
      content: JSON.stringify({ text: `@_user_1 ${text}` }), mentions: [{ key: '@_user_1', id: { open_id: 'bot' } }],
    } } };
}

// Stage 2 contract: real SDK dispatch with synthetic events, no Feishu account required.
test('飞书事件仅接收指定群的机器人提及，并保留回复和派生关系', async () => {
  let message: ReturnType<typeof parseIncomingMessage>;
  const dispatcher = new EventDispatcher({ loggerLevel: LoggerLevel.fatal }).register({
    'im.message.receive_v1': data => { message = parseIncomingMessage(data, config, 'bot'); },
  });
  await dispatcher.invoke(event('分支：比较两个型号', 'answer'), { needCheck: false });
  assert.deepEqual(message, { messageId: 'incoming', eventId: 'event', senderId: 'user',
    parentId: 'answer', text: '比较两个型号', branch: true });
  const foreign = event('问题');
  foreign.event.message.chat_id = 'foreign';
  await dispatcher.invoke(foreign, { needCheck: false });
  assert.equal(message, undefined);
  const untargeted = event('问题');
  untargeted.event.message.mentions[0].id.open_id = 'someone';
  await dispatcher.invoke(untargeted, { needCheck: false });
  assert.equal(message, undefined);
  const failure = new EventDispatcher({ loggerLevel: LoggerLevel.fatal }).register({
    'im.message.receive_v1': () => { throw new Error('disk unavailable'); },
  });
  await assert.rejects(failure.invoke(event('问题'), { needCheck: false }), /disk unavailable/);
  assert.throws(() => loadFeishuConfig({}), /FEISHU_APP_ID/);
  const pieces = splitReply('汉字😀'.repeat(4000));
  assert.ok(pieces.length > 1);
  assert.ok(pieces.every(piece => Buffer.byteLength(piece) < 12100 && !piece.includes('\uFFFD')));
});

// SDK API calls are replaced with an in-memory Feishu service to test delivery decisions.
test('飞书回复校验原群，并从分页历史核对未知发送结果', async () => {
  const transport = new FeishuTransport(config, () => {});
  transport.client.request = async <T>() => ({ code: 0, bot: { open_id: 'bot' } }) as T;
  await transport.identify();
  const row: Outgoing = { id: 1, job_id: 1, part: 0, uuid: 'uuid', message_id: null, reply_to: 'incoming',
    content: '结果<at user_id="all">全体</at>', status: 'pending', first_attempt: Date.now(), attempts: 0,
    next_attempt: 0, answer_entry: 'answer', session_file: 'session' };
  let chat = 'chat';
  let sent = 0;
  transport.client.im.v1.message.get = async () => ({ code: 0, data: { items: [{
    message_id: 'incoming', chat_id: chat, thread_id: 'thread',
  }] } });
  transport.client.im.v1.message.reply = async payload => {
    assert.equal(payload?.data.uuid, 'uuid');
    assert.doesNotMatch(payload!.data.content, /<at/);
    sent++;
    return { code: 0, data: { message_id: 'outgoing', chat_id: chat } };
  };
  assert.deepEqual(await transport.reply(row), { messageId: 'outgoing' });
  chat = 'foreign';
  await assert.rejects(transport.reply(row), error => error instanceof DeliveryError && error.kind === 'failed');
  assert.equal(sent, 1);
  chat = 'chat';
  const requested: string[] = [];
  transport.client.im.v1.message.list = async payload => {
    assert.equal(payload?.params.container_id_type, 'thread');
    requested.push(payload?.params.page_token ?? 'first');
    if (!payload?.params.page_token) return { code: 0, data: { has_more: true, page_token: 'second', items: [] } };
    return { code: 0, data: { has_more: false, items: [{ message_id: 'outgoing', chat_id: chat, parent_id: 'incoming',
      sender: { id: 'app', id_type: 'app_id', sender_type: 'app' }, create_time: String(Date.now()),
      body: { content: JSON.stringify({ text: row.content.replace(/<(?=\/?at\b)/gi, '＜') }) } }] } };
  };
  assert.deepEqual(await transport.reconcile(row), { messageId: 'outgoing' });
  assert.deepEqual(requested, ['first', 'second']);
});
