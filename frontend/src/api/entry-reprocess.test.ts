import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

test('reprocessing uses the canonical requests and library rebuild encodes its target', async () => {
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
  await module.exports.rebuildCatalogLibrary('library/id')
  await module.exports.reviewLibrary('library/id', 'all')
  await module.exports.reviewLibrary('library/id', 'pending')
  assert.deepEqual(requests, [
    { url: '/api/v1/entries/failed-entry/reparse', body: undefined },
    { url: '/api/v1/entries/failed-entry/reparse', body: undefined },
    { url: '/api/v1/libraries/library%2Fid/rebuild', body: undefined },
    { url: '/api/v1/libraries/library%2Fid/review', body: { scope: 'all' } },
    { url: '/api/v1/libraries/library%2Fid/review', body: { scope: 'pending' } },
  ])
})
