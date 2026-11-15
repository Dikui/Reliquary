import { loadConfig, loadFeishuConfig } from './config.js';
import { FeishuTransport } from './feishu.js';
import { connectQueryTools, safeFailure } from './tools.js';
import { Store } from './store.js';
import { Worker } from './worker.js';
import { ShelfEditor } from './shelf.js';
import { startHealth, type HealthState } from './health.js';
import type { Server } from 'node:http';

if (process.argv.includes('--help')) {
  console.log(`飞书查询与货架维护服务\n  node --use-env-proxy --env-file=/绝对路径/demo.env --import tsx main.ts\n\n沿用本地查询配置，另需\n  FEISHU_APP_ID       企业自建应用 App ID\n  FEISHU_APP_SECRET   应用密钥\n  FEISHU_TENANT_KEY   指定租户\n  FEISHU_CHAT_ID      指定测试群\n\n独立 @机器人 新建会话；回复机器人并 @ 继续会话；回复完整答案并输入“分支：问题”创建子会话。\n修改先保存待确认提议，发起人回复完整预览并 @机器人，发送“确认 口令”或“取消 口令”。\n状态保存在 AGENT_DATA_DIR/feishu.sqlite，模型历史保存在 sessions/。`);
} else {
  process.umask(0o077);
  let store: Store | undefined;
  let queries: Awaited<ReturnType<typeof connectQueryTools>> | undefined;
  let worker: Worker | undefined;
  let transport: FeishuTransport | undefined;
  let timer: NodeJS.Timeout | undefined;
  let health: Server | undefined;
  let stopTimer: NodeJS.Timeout | undefined;
  let phase: HealthState['phase'] = 'starting';
  let recovered = false;
  let stopping = false;
  let resolveStop: () => void;
  const stopped = new Promise<void>(resolve => { resolveStop = resolve; });
  const stop = (failed = false) => {
    if (stopping) return;
    stopping = true;
    phase = 'stopping';
    if (failed) process.exitCode = 1;
    stopTimer = setTimeout(() => process.exit(1), 20000);
    stopTimer.unref();
    transport?.close();
    resolveStop();
  };
  process.once('SIGINT', () => stop());
  process.once('SIGTERM', () => stop());
  const log = (event: string, details?: object) => console.log(JSON.stringify({ time: new Date().toISOString(), event, ...details }));
  try {
    const config = loadConfig();
    const feishu = loadFeishuConfig();
    health = await startHealth(() => ({ phase, recovered, connection: transport?.connectionStatus() ?? 'idle',
      storage: Boolean(store && store.health()), lastError: worker?.lastError ?? null }));
    store = new Store(config.dataDir, feishu);
    queries = await connectQueryTools(config);
    queries.protectSecrets(feishu.appSecret);
    transport = new FeishuTransport(feishu, log);
    transport.onFatal = () => stop(true);
    await transport.identify();
    const shelf = new ShelfEditor(queries, store);
    queries.proposalSchema();
    worker = new Worker(config, queries, store, transport, log, shelf);
    await worker.recover();
    recovered = true;
    const readyStore = store;
    const readyWorker = worker;
    if (!stopping) {
      await transport.start(message => {
        if (stopping) throw new Error('service_stopping');
        message.text = queries!.redact(message.text);
        const accepted = readyStore.accept(message);
        log('message_received', { messageId: message.messageId, accepted });
        readyWorker.wake();
      });
      phase = 'running';
      timer = setInterval(() => { readyWorker.wake(); readyWorker.deliver(); }, 1000);
      worker.wake();
      worker.deliver();
      await stopped;
    }
  } catch (error) {
    console.error(JSON.stringify(safeFailure(error)));
    process.exitCode = 1;
  } finally {
    phase = 'stopping';
    if (timer) clearInterval(timer);
    transport?.close();
    await worker?.close();
    await queries?.close();
    store?.close();
    health?.closeAllConnections();
    health?.close();
    if (stopTimer) clearTimeout(stopTimer);
  }
}
