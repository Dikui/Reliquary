import { loadConfig, type Config } from './config.js';
import { connectQueryTools, safeFailure } from './tools.js';
import { Store, type ChannelScope } from './store.js';
import { PiService, Worker } from './worker.js';
import { ShelfEditor } from './shelf.js';
import { startHealth, type HealthState } from './health.js';
import { modelRuntime, refreshModelCatalog } from './pi.js';
import type { MessageTransport } from './transport.js';
import type { Server } from 'node:http';

export interface MessageChannel {
  name: string;
  dataDir: string;
  scope: ChannelScope;
  secrets: string[];
  replyBytes?: number;
  connect(store: Store, log: (event: string, details?: object) => void): MessageTransport;
}

export async function runService(configure: (config: Config) => MessageChannel[]) {
  process.umask(0o077);
  const log = (event: string, details?: object) => process.stdout.write(JSON.stringify({ time: new Date().toISOString(), event, ...details }) + '\n');
  const channels: { name: string; store: Store; worker: Worker; transport: MessageTransport }[] = [];
  const stores: Store[] = [];
  let queries: Awaited<ReturnType<typeof connectQueryTools>> | undefined;
  let service: PiService | undefined;
  let timer: NodeJS.Timeout | undefined;
  let catalogTimer: NodeJS.Timeout | undefined;
  const catalogShutdown = new AbortController();
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
    resolveStop();
  };
  const onSignal = () => stop();
  process.once('SIGINT', onSignal);
  process.once('SIGTERM', onSignal);
  try {
    const config = loadConfig();
    const definitions = configure(config);
    if (!definitions.length) throw new Error('missing_message_channel');
    health = await startHealth(() => {
      const connections = Object.fromEntries(channels.map(c => [c.name, c.transport.connectionStatus()]));
      return { phase, recovered, channels: connections,
        connection: Object.values(connections).find(status => status !== 'connected') ?? (channels.length ? 'connected' : 'idle'),
        storage: channels.length > 0 && channels.every(c => c.store.health()),
        lastError: channels.find(c => c.worker.lastError)?.worker.lastError ?? null };
    });
    queries = await connectQueryTools(config);
    const tools = queries;
    tools.proposalSchema();
    for (const definition of definitions) tools.protectSecrets(...definition.secrets);
    let runtime: ReturnType<typeof modelRuntime> | undefined;
    const getRuntime = () => runtime ??= modelRuntime(config, tools).catch(error => { runtime = undefined; throw error; });

    for (const definition of definitions) {
      const { dataDir, scope, name: channel, replyBytes } = definition;
      const store = new Store(dataDir, scope, channel, replyBytes);
      stores.push(store);
      const transport = definition.connect(store, log);
      const name = store.channel.toLowerCase();
      const channelLog = (event: string, details?: object) => log(event, { channel: name, ...details });
      transport.onFatal = () => stop(true);
      const worker = new Worker({ ...config, dataDir }, tools, store, transport, channelLog,
        new ShelfEditor(tools, store), getRuntime);
      channels.push({ name, store, worker, transport });
    }
    const pi = new PiService(channels.map(c => c.worker), log);
    service = pi;
    for (const channel of channels) await channel.worker.recover();
    recovered = true;
    if (!stopping) {
      for (const channel of channels) {
        await channel.transport.start(message => {
          if (stopping) throw new Error('service_stopping');
          message.text = tools.redact(message.text);
          const accepted = channel.store.accept(message);
          log('message_received', { channel: channel.name, messageId: message.messageId, accepted });
          pi.wake();
          return accepted;
        });
      }
      phase = 'running';
      if (config.modelAPI === 'openai-codex-responses' && process.env.PI_OFFLINE === undefined) {
        const refresh = () => {
          void refreshModelCatalog(config, catalogShutdown.signal).then(() => {
            runtime = undefined;
            log('model_catalog_refreshed');
          }).catch(() => {
            if (!catalogShutdown.signal.aborted) log('model_catalog_refresh_failed');
          });
        };
        refresh();
        catalogTimer = setInterval(refresh, 4 * 60 * 60 * 1000);
      }
      const tick = () => {
        pi.wake();
        for (const channel of channels) channel.worker.deliver();
      };
      timer = setInterval(tick, 1000);
      tick();
      await stopped;
    }
  } catch (error) {
    console.error(JSON.stringify(safeFailure(error)));
    process.exitCode = 1;
  } finally {
    phase = 'stopping';
    if (timer) clearInterval(timer);
    if (catalogTimer) clearInterval(catalogTimer);
    catalogShutdown.abort();
    stopping = true;
    for (const channel of channels) channel.transport.close();
    await service?.close();
    await queries?.close();
    for (const store of stores) store.close();
    health?.closeAllConnections();
    health?.close();
    if (stopTimer) clearTimeout(stopTimer);
    process.off('SIGINT', onSignal);
    process.off('SIGTERM', onSignal);
  }
}
