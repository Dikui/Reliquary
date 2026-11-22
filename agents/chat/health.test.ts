import assert from 'node:assert/strict';
import test from 'node:test';
import { startHealth, type HealthState } from './health.js';

// Stage 4 small spec. Health probes inspect local state without external calls.
test('就绪检查反映启动、连接、存储与停止状态', async () => {
  const state: HealthState = { phase: 'starting', recovered: false, connection: 'idle', storage: true, lastError: null };
  const server = await startHealth(() => state, 0);
  const url = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  try {
    assert.equal((await fetch(url + '/ready')).status, 503);
    assert.equal((await fetch(url + '/health')).status, 200);
    Object.assign(state, { phase: 'running', recovered: true, connection: 'connected' });
    assert.equal((await fetch(url + '/ready')).status, 200);
    state.lastError = 'query_rejected';
    assert.equal((await fetch(url + '/ready')).status, 200);
    state.connection = 'reconnecting';
    assert.equal((await fetch(url + '/ready')).status, 503);
    state.connection = 'connected'; state.storage = false;
    assert.equal((await fetch(url + '/ready')).status, 503);
    state.storage = true; state.phase = 'stopping';
    assert.equal((await fetch(url + '/ready')).status, 503);
    assert.equal((await fetch(url + '/anything')).status, 404);
  } finally { server.closeAllConnections(); server.close(); }
});
