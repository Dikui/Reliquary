import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

type Request = { method: string; url: string; body?: unknown }
function loadAPI(requests: Request[]) {
  const module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL('./entities.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => {
    assert.equal(name, '@/utils/request')
    return Object.fromEntries(['get', 'post', 'patch', 'put', 'del'].map(method => [method, (url: string, body?: unknown) => { requests.push({ method, url, ...(body === undefined ? {} : { body }) }); return Promise.resolve({ success: true, data: [] }) }]))
  }, module, module.exports)
  return module.exports
}
test('entity resources and mutations use the dedicated entity routes', async () => {
  const requests: Request[] = [], api = loadAPI(requests)
  await api.listEntities({ entity: 'Aurora Labs', limit: 50, offset: 0 })
  await api.getEntity('entity/a')
  await api.createEntity({ canonical_name: 'Claude Code', aliases: ['claudecode'] })
  await api.updateEntity('entity/a', { canonical_name: 'Claude Code', aliases: [] })
  await api.mergeEntities({ keep_entity_id: 'kept', merge_entity_id: 'merged' })
  await api.listEntityMergeRecords('entity/a')
  assert.deepEqual(requests, [
    { method: 'get', url: '/api/v1/entities?entity=Aurora+Labs&limit=50&offset=0' },
    { method: 'get', url: '/api/v1/entities/entity%2Fa' },
    { method: 'post', url: '/api/v1/entities', body: { canonical_name: 'Claude Code', aliases: ['claudecode'] } },
    { method: 'patch', url: '/api/v1/entities/entity%2Fa', body: { canonical_name: 'Claude Code', aliases: [] } },
    { method: 'post', url: '/api/v1/entities/merge', body: { keep_entity_id: 'kept', merge_entity_id: 'merged' } },
    { method: 'get', url: '/api/v1/entities/entity%2Fa/merge-records' },
  ])
})
test('entity routes do not send a library range', async () => {
  const requests: Request[] = [], api = loadAPI(requests)
  await api.listEntities({ limit: 20 })
  assert.equal(requests[0]?.url, '/api/v1/entities?limit=20')
})
