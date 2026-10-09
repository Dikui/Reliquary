import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'
import * as scope from './knowledge-context-scope'

test('historical backfill uses dedicated estimate, batch and creation resources', async () => {
  const requests: Array<{ method: string; url: string; body?: unknown }> = []
  const module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL('./knowledge-context.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => {
    if (name === './knowledge-context-scope') return scope
    assert.equal(name, '@/utils/request')
    return Object.fromEntries(['get', 'post'].map(method => [method, (url: string, body?: unknown) => {
      requests.push({ method, url, ...(body === undefined ? {} : { body }) })
      return Promise.resolve({ success: true })
    }]))
  }, module, module.exports)
  const api = module.exports
  await api.getKnowledgeBackfillEstimate({ entity_id: 'entity/1' })
  await api.getKnowledgeBackfillEstimate({ topic_id: 'topic?1' })
  await api.startKnowledgeBackfill({ topic_id: 'topic?1' })
  await api.listKnowledgeBackfillBatches({ entity_id: 'entity/1' })
  await api.getKnowledgeBackfillBatch('batch/1')
  assert.deepEqual(requests, [
    { method: 'get', url: '/api/v1/knowledge-context/backfill/estimate?entity_id=entity%2F1' },
    { method: 'get', url: '/api/v1/knowledge-context/backfill/estimate?topic_id=topic%3F1' },
    { method: 'post', url: '/api/v1/knowledge-context/backfill', body: { topic_id: 'topic?1' } },
    { method: 'get', url: '/api/v1/knowledge-context/backfill/batches?entity_id=entity%2F1' },
    { method: 'get', url: '/api/v1/knowledge-context/backfill/batches/batch%2F1' },
  ])
})
