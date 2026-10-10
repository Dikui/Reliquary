import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'
import * as scope from './entity-types'

type Request = { method: string; url: string; body?: unknown }
function loadAPI(requests: Request[]) {
  const module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL('./entities.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => {
    if (name === './entity-types') return scope
    assert.equal(name, '@/utils/request')
    return Object.fromEntries(['get', 'post'].map(method => [method, (url: string, body?: unknown) => { requests.push({ method, url, ...(body === undefined ? {} : { body }) }); return Promise.resolve({ success: true, data: [] }) }]))
  }, module, module.exports)
  return module.exports
}
test('entity resources and mutations use the dedicated entity routes', async () => {
  const requests: Request[] = [], api = loadAPI(requests)
  await api.listEntities({ entity: 'Aurora Labs', entity_type: 'company', limit: 50, offset: 0 })
  await api.getEntity('entity/a')
  await api.mergeEntities({ keep_entity_id: 'kept', merge_entity_id: 'merged' })
  await api.listEntityMergeRecords('entity/a')
  await api.listEntityMergeSuggestions({ status: 'pending', limit: 20 })
  await api.acceptEntityMergeSuggestion('suggestion/a')
  await api.rejectEntityMergeSuggestion('suggestion/b')
  await api.getEntityNameEmbeddingRebuildStatus()
  await api.rebuildEntityNameEmbeddings()
  assert.deepEqual(requests, [
    { method: 'get', url: '/api/v1/entities?entity=Aurora+Labs&entity_type=company&limit=50&offset=0' },
    { method: 'get', url: '/api/v1/entities/entity%2Fa' },
    { method: 'post', url: '/api/v1/entities/merge', body: { keep_entity_id: 'kept', merge_entity_id: 'merged' } },
    { method: 'get', url: '/api/v1/entities/entity%2Fa/merge-records' },
    { method: 'get', url: '/api/v1/entities/merge-suggestions?status=pending&limit=20' },
    { method: 'post', url: '/api/v1/entities/merge-suggestions/suggestion%2Fa/accept', body: {} },
    { method: 'post', url: '/api/v1/entities/merge-suggestions/suggestion%2Fb/reject', body: {} },
    { method: 'get', url: '/api/v1/entities/name-embeddings/rebuild' },
    { method: 'post', url: '/api/v1/entities/name-embeddings/rebuild', body: {} },
  ])
})
test('entity routes do not send a knowledge base range', async () => {
  const requests: Request[] = [], api = loadAPI(requests)
  await api.listEntities({ limit: 20 })
  assert.equal(requests[0]?.url, '/api/v1/entities?limit=20')
  assert.equal('entityScopeParams' in scope, false)
})
