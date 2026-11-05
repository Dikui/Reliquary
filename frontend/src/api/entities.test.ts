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
    return Object.fromEntries(['get', 'post', 'patch', 'put'].map(method => [method, (url: string, body?: unknown) => { requests.push({ method, url, ...(body === undefined ? {} : { body }) }); return Promise.resolve({ success: true, data: [] }) }]))
  }, module, module.exports)
  return module.exports
}
test('entity resources and mutations use the dedicated entity routes', async () => {
  const requests: Request[] = [], api = loadAPI(requests)
  await api.listEntities({ library_id: 'library-a', entity: 'Aurora Labs', entity_type: 'company', limit: 50, offset: 0 })
  await api.getEntity('entity/a')
  await api.mergeEntities({ keep_entity_id: 'kept', merge_entity_id: 'merged' })
  await api.listEntityMergeRecords('entity/a')
  await api.listEntityMergeSuggestions({ library_id: 'library-a', status: 'pending', limit: 20 })
  await api.acceptEntityMergeSuggestion('suggestion/a')
  await api.rejectEntityMergeSuggestion('suggestion/b')
  await api.getEntityNameEmbeddingRebuildStatus()
  await api.rebuildEntityNameEmbeddings()
  assert.deepEqual(requests, [
    { method: 'get', url: '/api/v1/entities?library_id=library-a&entity=Aurora+Labs&entity_type=company&limit=50&offset=0' },
    { method: 'get', url: '/api/v1/entities/entity%2Fa' },
    { method: 'post', url: '/api/v1/entities/merge', body: { keep_entity_id: 'kept', merge_entity_id: 'merged' } },
    { method: 'get', url: '/api/v1/entities/entity%2Fa/merge-records' },
    { method: 'get', url: '/api/v1/entities/merge-suggestions?library_id=library-a&status=pending&limit=20' },
    { method: 'post', url: '/api/v1/entities/merge-suggestions/suggestion%2Fa/accept', body: {} },
    { method: 'post', url: '/api/v1/entities/merge-suggestions/suggestion%2Fb/reject', body: {} },
    { method: 'get', url: '/api/v1/entities/name-embeddings/rebuild' },
    { method: 'post', url: '/api/v1/entities/name-embeddings/rebuild', body: {} },
  ])
})
test('entity list requires the owning library', async () => {
  const requests: Request[] = [], api = loadAPI(requests)
  await api.listEntities({ library_id: 'library-a', limit: 20 })
  assert.equal(requests[0]?.url, '/api/v1/entities?library_id=library-a&limit=20')
  assert.equal('entityScopeParams' in scope, false)
})

test('managed entities keep revision and manual exclusion in their write requests', async () => {
  const requests: Request[] = [], api = loadAPI(requests)
  await api.updateManagedEntity('entity/a', { revision: 3, name: 'New name', attributes: { power: 12 } })
  await api.setEntryEntityLink('entry/a', 'entity/a', true)
  assert.deepEqual(requests, [
    { method: 'patch', url: '/api/v1/entities/entity%2Fa', body: { revision: 3, name: 'New name', attributes: { power: 12 } } },
    { method: 'put', url: '/api/v1/entries/entry%2Fa/entity-links/entity%2Fa', body: { excluded: true } },
  ])
})

test('effective attributes and split selections retain their typed payloads', async () => {
  const requests: Request[] = [], api = loadAPI(requests)
  const effective_at = '2026-11-01T00:00:00.000Z'
  const attributes = { tags: ['solar', 'storage'], links: ['one', 'two'], website: 'https://example.org', product: 'related-product', file: 'attachment-id' }
  await api.updateManagedEntity('product', { revision: 4, attributes, effective_at })
  await api.splitManagedEntity('product', { revision: 5, new_entity: { name: 'Variant', kind: 'model', code: 'V2', description: '', aliases: [], attributes }, entry_ids: ['document'], relation_ids: ['edge'], effective_at })
  await api.setEntryEntityLink('document', 'another-library-product', false, 'test_report')
  assert.deepEqual(requests, [
    { method: 'patch', url: '/api/v1/entities/product', body: { revision: 4, attributes, effective_at } },
    { method: 'post', url: '/api/v1/entities/product/split', body: { revision: 5, new_entity: { name: 'Variant', kind: 'model', code: 'V2', description: '', aliases: [], attributes }, entry_ids: ['document'], relation_ids: ['edge'], effective_at } },
    { method: 'put', url: '/api/v1/entries/document/entity-links/another-library-product', body: { excluded: false, relation_type: 'test_report' } },
  ])
})
