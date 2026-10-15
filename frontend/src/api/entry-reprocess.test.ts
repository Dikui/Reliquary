import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

test('detail compatibility API and list API share the canonical resume request', async () => {
  const requests: { url: string; body: unknown }[] = []
  const module = { exports: {} as any }
  const source = readFileSync(new URL('./library.ts', import.meta.url), 'utf8')
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => {
    assert.equal(name, '../utils/request')
    return { post: (url: string, body: unknown) => { requests.push({ url, body }); return Promise.resolve({ success: true }) } }
  }, module, module.exports)
  await module.exports.reprocessEntry('failed-entry')
  await module.exports.reparseEntry('failed-entry')
  assert.deepEqual(requests, [
    { url: '/api/v1/entries/failed-entry/reparse', body: undefined },
    { url: '/api/v1/entries/failed-entry/reparse', body: undefined },
  ])
})
