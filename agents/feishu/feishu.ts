import { Client, defaultHttpInstance, EventDispatcher, LoggerLevel, WSClient, type HttpInstance } from '@larksuiteoapi/node-sdk';
import type { FeishuConfig } from './config.js';
import { QueryError, record } from './output.js';
import type { IncomingMessage, Outgoing } from './store.js';

export type MessageInfo = { message_id: string; chat_id: string; parent_id?: string; thread_id?: string;
  create_time?: string; deleted?: boolean; sender?: { id: string; sender_type: string }; body?: { content: string } };
export type Delivery = { messageId: string };

export function parseIncomingMessage(value: unknown, config: FeishuConfig, botId: string): IncomingMessage | undefined {
  const event = record(value);
  const message = record(event.message);
  const sender = record(event.sender);
  if (event.app_id !== config.appId || event.tenant_key !== config.tenantKey ||
      message.chat_id !== config.chatId || message.chat_type !== 'group' || sender.sender_type !== 'user') return;
  const mentions = Array.isArray(message.mentions) ? message.mentions.map(record) : [];
  const bot = mentions.find(mention => record(mention.id).open_id === botId);
  if (!bot && !(message.message_type === 'file' && message.parent_id)) return;
  if (typeof message.message_id !== 'string' || typeof event.event_id !== 'string') return;
  const senderId = record(sender.sender_id).open_id;
  if (typeof senderId !== 'string') return;
  const result: IncomingMessage = { messageId: message.message_id, eventId: event.event_id, senderId,
    parentId: typeof message.parent_id === 'string' ? message.parent_id : null, text: '', branch: false };
  if (message.message_type === 'file') {
    try {
      const file = record(JSON.parse(String(message.content)));
      if (typeof file.file_key !== 'string' || typeof file.file_name !== 'string') throw new Error();
      return { ...result, text: `用户发送附件 ${file.file_name}。先读取附件目录，结合会话确认用途。`,
        attachment: { fileKey: file.file_key, name: file.file_name } };
    } catch { return { ...result, rejection: '无法读取附件信息，请重新发送。' }; }
  }
  if (message.message_type !== 'text') return { ...result, rejection: '请发送文字，或回复机器人消息发送文件。' };
  try {
    const text = record(JSON.parse(String(message.content))).text;
    if (typeof text !== 'string') throw new Error();
    result.text = text;
    for (const mention of mentions) {
      if (typeof mention.key === 'string') result.text = result.text.split(mention.key).join(mention === bot ? '' : String(mention.name ?? ''));
    }
    result.text = result.text.trim();
    result.branch = /^分支\s*[:：]/u.test(result.text);
    if (result.branch) result.text = result.text.replace(/^分支\s*[:：]\s*/u, '');
    if (!result.text) result.rejection = '请在 @机器人 后输入问题。';
    if (Buffer.byteLength(result.text) > 24000) result.rejection = '问题过长，请缩小范围后重试。';
    return result;
  } catch { return { ...result, rejection: '无法解析这条文字消息，请重新输入。' }; }
}

export class DeliveryError extends Error {
  constructor(public kind: 'unknown' | 'retry' | 'failed', public code: string) { super(code); }
}

function apiData(response: { code?: number; msg?: string; data?: unknown }) {
  if (response.code !== 0) {
    const retry = [99991400, 230020, 11232, 11233].includes(response.code ?? -1);
    throw new DeliveryError(retry ? 'retry' : 'failed', `feishu_${response.code ?? 'invalid_response'}`);
  }
  return response.data;
}

export class FeishuTransport {
  readonly client: Client;
  private ws?: WSClient;
  private botId = '';
  private http = defaultHttpInstance.create({ timeout: 15000 });
  constructor(readonly config: FeishuConfig, private log: (event: string, details?: object) => void) {
    this.http.interceptors.response.use(response => (response.config as { $return_headers?: boolean }).$return_headers
      ? { data: response.data, headers: response.headers } : response.data);
    this.client = new Client({ appId: config.appId, appSecret: config.appSecret, httpInstance: this.http as unknown as HttpInstance, loggerLevel: LoggerLevel.error,
      logger: { error: () => log('feishu_sdk_error'), warn: () => {}, info: () => {}, debug: () => {}, trace: () => {} } });
  }

  async identify() {
    const response = await this.client.request<{ code: number; bot?: { open_id: string } }>({ url: '/open-apis/bot/v3/info', method: 'GET' });
    if (response.code !== 0 || !response.bot?.open_id) throw new QueryError('feishu_identity', '无法读取飞书机器人身份');
    this.botId = response.bot.open_id;
    return this.botId;
  }

  async start(accept: (message: IncomingMessage) => void) {
    if (!this.botId) await this.identify();
    const dispatcher = new EventDispatcher({ loggerLevel: LoggerLevel.error,
      logger: { error: () => this.log('feishu_event_error'), warn: () => {}, info: () => {}, debug: () => {}, trace: () => {} } })
      .register({ 'im.message.receive_v1': data => {
        const message = parseIncomingMessage(data, this.config, this.botId);
        if (message) accept(message);
      } });
    this.ws = new WSClient({ appId: this.config.appId, appSecret: this.config.appSecret, httpInstance: this.http as unknown as HttpInstance, handshakeTimeoutMs: 15000,
      loggerLevel: LoggerLevel.error,
      logger: { error: () => this.log('feishu_connection_error'), warn: () => {}, info: () => {}, debug: () => {}, trace: () => {} },
      onReady: () => this.log('feishu_connected'), onReconnected: () => this.log('feishu_reconnected'),
      onReconnecting: () => this.log('feishu_reconnecting'), onError: () => {
        this.log('feishu_connection_failed');
        this.onFatal?.();
      } });
    await this.ws.start({ eventDispatcher: dispatcher });
  }

  onFatal?: () => void;

  connectionStatus() { return this.ws?.getConnectionStatus().state ?? 'idle'; }

  async download(messageId: string, fileKey: string): Promise<Buffer> {
    await this.original(messageId);
    const resource = await this.client.im.v1.messageResource.get({ path: { message_id: messageId, file_key: fileKey }, params: { type: 'file' } });
    const stream = resource.getReadableStream();
    const chunks: Buffer[] = [];
    let size = 0;
    for await (const chunk of stream) {
      size += chunk.length;
      if (size > 32 * 1024 * 1024) {
        stream.destroy();
        throw new QueryError('file_too_large', '附件超过 32 MiB');
      }
      chunks.push(Buffer.from(chunk));
    }
    return Buffer.concat(chunks);
  }

  private async original(messageId: string): Promise<MessageInfo> {
    const response = await this.client.im.v1.message.get({ path: { message_id: messageId } });
    apiData(response);
    const message = response.data?.items?.find(item => item.message_id === messageId);
    if (!message || message.deleted || message.chat_id !== this.config.chatId) {
      throw new DeliveryError('failed', 'original_message_unavailable');
    }
    return message as MessageInfo;
  }

  async reply(row: Outgoing): Promise<Delivery> {
    try { await this.original(row.reply_to); }
    catch (error) {
      if (error instanceof DeliveryError) throw error;
      throw new DeliveryError('retry', 'original_lookup_interrupted');
    }
    try {
      // Text messages interpret <at> specially. Keep model output from notifying users.
      const content = JSON.stringify({ text: row.content.replace(/<(?=\/?at\b)/gi, '＜') });
      const response = await this.client.im.v1.message.reply({ path: { message_id: row.reply_to },
        data: { content, msg_type: 'text', uuid: row.uuid } });
      apiData(response);
      if (!response.data?.message_id || response.data.chat_id !== this.config.chatId) throw new DeliveryError('unknown', 'reply_response_incomplete');
      return { messageId: response.data.message_id };
    } catch (error) {
      if (error instanceof DeliveryError) throw error;
      throw new DeliveryError('unknown', 'reply_transport_interrupted');
    }
  }

  async reconcile(row: Outgoing): Promise<Delivery | undefined> {
    const original = await this.original(row.reply_to);
    let pageToken: string | undefined;
    const found = new Map<string, MessageInfo>();
    const seenPages = new Set<string>();
    do {
      const response = await this.client.im.v1.message.list({ params: {
        container_id_type: original.thread_id ? 'thread' : 'chat', container_id: original.thread_id || this.config.chatId,
        start_time: original.thread_id ? undefined : String(Math.floor((row.first_attempt! - 60000) / 1000)),
        end_time: original.thread_id ? undefined : String(Math.ceil((Date.now() + 1000) / 1000)),
        page_size: 50, page_token: pageToken, sort_type: 'ByCreateTimeAsc',
      } });
      apiData(response);
      for (const item of response.data?.items ?? []) {
        if (item.chat_id !== this.config.chatId || item.parent_id !== row.reply_to || item.deleted ||
            item.sender?.sender_type !== 'app' || ![this.config.appId, this.botId].includes(item.sender.id)) continue;
        let text: unknown;
        try { text = JSON.parse(item.body?.content ?? '{}').text; } catch { continue; }
        if (text === row.content.replace(/<(?=\/?at\b)/gi, '＜') && item.message_id &&
            Number(item.create_time) >= row.first_attempt! - 60000) found.set(item.message_id, item as MessageInfo);
      }
      pageToken = response.data?.has_more ? response.data.page_token : undefined;
      if (response.data?.has_more && !pageToken) throw new DeliveryError('unknown', 'history_incomplete');
      if (pageToken && seenPages.has(pageToken)) throw new DeliveryError('unknown', 'history_repeated_page');
      if (pageToken) seenPages.add(pageToken);
    } while (pageToken);
    if (found.size > 1) throw new DeliveryError('unknown', 'delivery_ambiguous');
    const messageId = found.keys().next().value;
    return messageId ? { messageId } : undefined;
  }

  close() { this.ws?.close({ force: true }); }
}
