import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'
import * as scope from './entity-types'

type Request = { method: string; url: string; body?: unknown }
function loadAPI(requests: Request[], source = './entities.ts') {
  const module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL(source, import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => {
    if (name === './entity-types') return scope
    assert.equal(name, '@/utils/request')
    return Object.fromEntries(['get', 'post', 'patch', 'put', 'del'].map(method => [method, (url: string, body?: unknown) => { requests.push({ method, url, ...(body === undefined ? {} : { body }) }); return Promise.resolve({ success: true, data: [] }) }]))
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
  await api.acceptEntityMergeSuggestion('suggestion/a', 'entity/a')
  await api.rejectEntityMergeSuggestion('suggestion/b')
  await api.getEntityNameEmbeddingRebuildStatus()
  await api.rebuildEntityNameEmbeddings()
  assert.deepEqual(requests, [
    { method: 'get', url: '/api/v1/entities?library_id=library-a&entity=Aurora+Labs&entity_type=company&limit=50&offset=0' },
    { method: 'get', url: '/api/v1/entities/entity%2Fa' },
    { method: 'post', url: '/api/v1/entities/merge', body: { keep_entity_id: 'kept', merge_entity_id: 'merged' } },
    { method: 'get', url: '/api/v1/entities/entity%2Fa/merge-records' },
    { method: 'get', url: '/api/v1/entities/merge-suggestions?library_id=library-a&status=pending&limit=20' },
    { method: 'post', url: '/api/v1/entities/merge-suggestions/suggestion%2Fa/accept', body: { keep_entity_id: 'entity/a' } },
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
  await api.updateManagedEntity('entity/a', { revision: 3, name: 'New name', attributes: { power: 12 }, auto_match: { 'ab': true, 'new name': false } })
  await api.setEntryEntityLink('entry/a', 'entity/a', true)
  assert.deepEqual(requests, [
    { method: 'patch', url: '/api/v1/entities/entity%2Fa', body: { revision: 3, name: 'New name', attributes: { power: 12 }, auto_match: { 'ab': true, 'new name': false } } },
    { method: 'put', url: '/api/v1/entries/entry%2Fa/entity-links/entity%2Fa', body: { excluded: true } },
  ])
})

test('effective attributes and split selections retain their typed payloads', async () => {
  const requests: Request[] = [], api = loadAPI(requests)
  const effective_at = '2026-11-01T00:00:00.000Z'
  const attributes = { tags: ['solar', 'storage'], links: ['one', 'two'], website: 'https://example.org', product: 'related-product', file: 'attachment-id' }
  await api.updateManagedEntity('product', { revision: 4, attributes, effective_at })
  await api.splitManagedEntity('product', { revision: 5, new_entity: { name: 'Variant', kind: 'model', code: 'V2', description: '', aliases: [], attributes }, entry_ids: ['document'], relation_ids: ['edge'], copy_relation_ids: ['parent'], effective_at })
  await api.setEntryEntityLink('document', 'another-library-product', false, 'test_report')
  assert.deepEqual(requests, [
    { method: 'patch', url: '/api/v1/entities/product', body: { revision: 4, attributes, effective_at } },
    { method: 'post', url: '/api/v1/entities/product/split', body: { revision: 5, new_entity: { name: 'Variant', kind: 'model', code: 'V2', description: '', aliases: [], attributes }, entry_ids: ['document'], relation_ids: ['edge'], copy_relation_ids: ['parent'], effective_at } },
    { method: 'put', url: '/api/v1/entries/document/entity-links/another-library-product', body: { excluded: false, relation_type: 'test_report' } },
  ])
})

test('managed merge sends explicitly discarded relation IDs', async () => {
  const requests: Request[] = [], api = loadAPI(requests)
  await api.mergeManagedEntity('target', { revision: 3, source_id: 'source', source_revision: 2, attributes: {}, drop_relation_ids: ['source-edge'], document_relation_preference: 'target' })
  assert.deepEqual(requests, [{ method: 'post', url: '/api/v1/entities/target/merge', body: { revision: 3, source_id: 'source', source_revision: 2, attributes: {}, drop_relation_ids: ['source-edge'], document_relation_preference: 'target' } }])
})

// Spec (small), MEL-04: relation writes carry the caller's business timestamps.
test('managed relation requests preserve business start and end times', async () => {
  const requests: Request[] = [], api = loadAPI(requests, './relations.ts')
  const valid_from = '2025-03-01T09:30:00.000Z', valid_to = '2025-04-01T09:30:00+08:00'
  const data = { subject_id: 'model', object_id: 'line', relation_type: 'belongs', valid_from }
  await api.createManagedRelation('products/a', data)
  await api.updateManagedRelation('relation/a', { ...data, version: 2 })
  await api.deleteEntityRelation('relation/a', 2, valid_to)
  await api.deleteEntityRelation('relation/b', 3)
  assert.deepEqual(requests, [
    { method: 'post', url: '/api/v1/libraries/products%2Fa/relations', body: data },
    { method: 'patch', url: '/api/v1/relations/relation%2Fa', body: { ...data, version: 2 } },
    { method: 'del', url: '/api/v1/relations/relation%2Fa?version=2&valid_to=2025-04-01T09%3A30%3A00%2B08%3A00' },
    { method: 'del', url: '/api/v1/relations/relation%2Fb?version=3' },
  ])
})
