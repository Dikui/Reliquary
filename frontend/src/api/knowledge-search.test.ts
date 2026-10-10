import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'

test('article search and filter options use the shared HTTP service routes', async () => {
  const calls: any[] = []
  const module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL('./knowledge-search.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => {
    assert.equal(name, '@/utils/request')
    return Object.fromEntries(['get', 'post'].map(method => [method, (url: string, body?: unknown) => { calls.push({ method, url, body }); return Promise.resolve({ success: true }) }]))
  }, module, module.exports)
  const request = { query: 'Launch', content_types: ['event'], entity_ids: ['e1'], topic_ids: ['t1'], published_from: '2026-10-01', sources: ['article'], collapse_similar: false, limit: 20 }
  await module.exports.searchKnowledgeArticles(request)
  await module.exports.getKnowledgeSearchOptions()
  assert.deepEqual(calls, [{ method: 'post', url: '/api/v1/knowledge-search', body: request }, { method: 'get', url: '/api/v1/knowledge-search/options', body: undefined }])
})
