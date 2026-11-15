import { createServer, type Server } from 'node:http';
import { once } from 'node:events';

export type HealthState = {
  phase: 'starting' | 'running' | 'stopping';
  recovered: boolean;
  connection: string;
  storage: boolean;
  lastError: string | null;
};

export async function startHealth(read: () => HealthState, port = 8787): Promise<Server> {
  const server = createServer((request, response) => {
    if (!['/health', '/ready'].includes(request.url ?? '')) { response.writeHead(404).end(); return; }
    let state: HealthState;
    try { state = read(); }
    catch { state = { phase: 'running', recovered: false, connection: 'unknown', storage: false, lastError: 'storage_unavailable' }; }
    const ready = state.phase === 'running' && state.recovered && state.storage && state.connection === 'connected';
    response.writeHead(request.url === '/health' || ready ? 200 : 503, { 'content-type': 'application/json', 'cache-control': 'no-store' });
    response.end(JSON.stringify({ ready, ...state }));
  });
  server.listen(port, '127.0.0.1');
  await once(server, 'listening');
  return server;
}
