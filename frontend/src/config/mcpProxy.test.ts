import assert from 'node:assert/strict'
import { once } from 'node:events'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { createServer as createHTTPServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { createServer, preview, type HttpServer } from 'vite'

const configFile = fileURLToPath(new URL('../../vite.config.ts', import.meta.url))

async function closeHTTPServer(server: Server | HttpServer) {
  if ('closeAllConnections' in server) server.closeAllConnections()
  await new Promise<void>(resolve => server.close(() => resolve()))
}

test('MCP requests reach the backend through development and preview servers', async t => {
  for (const mode of ['development', 'preview'] as const) {
    await t.test(mode, async () => {
      const requests: Array<{ method?: string; url?: string; authorization?: string; accept?: string; body: unknown }> = []
      const expected = { jsonrpc: '2.0', id: 1, result: { protocolVersion: '2025-11-25', capabilities: { tools: {} }, serverInfo: { name: 'test-backend', version: '1' } } }
      const backend = createHTTPServer(async (request, response) => {
        const chunks: Buffer[] = []
        for await (const chunk of request) chunks.push(Buffer.from(chunk))
        requests.push({ method: request.method, url: request.url, authorization: request.headers.authorization, accept: request.headers.accept, body: JSON.parse(Buffer.concat(chunks).toString()) })
        response.writeHead(200, { 'Content-Type': 'application/json' })
        response.end(JSON.stringify(expected))
      })
      backend.listen(0, '127.0.0.1')
      await once(backend, 'listening')
      const previousTarget = process.env.VITE_DEV_PROXY_TARGET
      process.env.VITE_DEV_PROXY_TARGET = `http://127.0.0.1:${(backend.address() as AddressInfo).port}`
      const outputDirectory = await mkdtemp(join(tmpdir(), 'reliquary-mcp-proxy-'))
      let closeFrontend: (() => Promise<void>) | undefined
      try {
        await writeFile(join(outputDirectory, 'index.html'), '<html><body>SPA fallback</body></html>')
        const options = { configFile, logLevel: 'silent' as const, build: { outDir: outputDirectory } }
        let address: AddressInfo
        if (mode === 'development') {
          const frontend = await createServer({ ...options, server: { host: '127.0.0.1', port: 0, strictPort: false } })
          closeFrontend = () => frontend.close()
          await frontend.listen()
          address = frontend.httpServer!.address() as AddressInfo
        } else {
          const frontend = await preview({ ...options, preview: { host: '127.0.0.1', port: 0, strictPort: false } })
          closeFrontend = () => closeHTTPServer(frontend.httpServer)
          address = frontend.httpServer.address() as AddressInfo
        }
        const request = { jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2025-11-25', capabilities: {}, clientInfo: { name: 'test-client', version: '1' } } }
        const response = await fetch(`http://127.0.0.1:${address.port}/mcp?client=test`, {
          method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: 'Bearer test-key', Accept: 'application/json, text/event-stream' }, body: JSON.stringify(request),
        })
        assert.equal(response.status, 200)
        assert.equal(response.headers.get('content-type'), 'application/json')
        assert.deepEqual(await response.json(), expected)
        assert.deepEqual(requests, [{ method: 'POST', url: '/mcp?client=test', authorization: 'Bearer test-key', accept: 'application/json, text/event-stream', body: request }])
      } finally {
        await closeFrontend?.()
        await closeHTTPServer(backend)
        if (previousTarget === undefined) delete process.env.VITE_DEV_PROXY_TARGET
        else process.env.VITE_DEV_PROXY_TARGET = previousTarget
        await rm(outputDirectory, { recursive: true, force: true })
      }
    })
  }
})
