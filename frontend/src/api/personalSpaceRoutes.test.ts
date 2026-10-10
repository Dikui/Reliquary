import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'
import * as scope from './knowledge-context-scope'
import { normalizeToolNames } from '../utils/toolNames'
import { evaluateToolRequirement, deriveKbFilterFromTools } from '../utils/tool-capabilities'
import { createApplicationRoutes } from '../router/routes'

type Request = { method: string; url: string; body?: unknown }
function loadAPI(path: string, requests: Request[]) {
  const module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => {
    if (name === '@/i18n') return { default: { global: { t: (key: string) => key } }, __esModule: true }
    if (name === './knowledge-context-scope') return scope
    if (name === '@/types/modelTiers') return {}
    assert.equal(name, '@/utils/request')
    return Object.fromEntries(['get', 'post', 'put', 'del'].map(method => [method, (url: string, body?: unknown) => {
      requests.push({ method, url, ...(body === undefined ? {} : { body }) })
      return Promise.resolve({ success: true, data: [] })
    }]))
  }, module, module.exports)
  return module.exports
}

test('space info and API key operations use singular paths without a tenant ID', async () => {
  const requests: Request[] = [], api = loadAPI('./tenant/index.ts', requests)
  await api.getTenantInfo()
  await api.updateTenant({ name: 'Personal space' })
  await api.listTenantAPIKeys()
  await api.createTenantAPIKey({ name: 'Automation' })
  await api.updateTenantAPIKey(7, { name: 'Renamed automation' })
  await api.deleteTenantAPIKey(7)
  assert.deepEqual(requests, [
    { method: 'get', url: '/api/v1/space' },
    { method: 'put', url: '/api/v1/space', body: { name: 'Personal space' } },
    { method: 'get', url: '/api/v1/space/api-keys' },
    { method: 'post', url: '/api/v1/space/api-keys', body: { name: 'Automation' } },
    { method: 'put', url: '/api/v1/space/api-keys/7', body: { name: 'Renamed automation' } },
    { method: 'del', url: '/api/v1/space/api-keys/7' },
  ])
})

test('settings, runtime queues and space key values use the current API routes', async () => {
  const requests: Request[] = [], api = loadAPI('./system/index.ts', requests)
  await api.listSystemSettings(); await api.getSystemSetting('key/a'); await api.updateSystemSetting('key/a', true); await api.resetSystemSetting('key/a')
  await api.getRuntimeQueues(); await api.getRuntimeTasks('context', 'pending'); await api.mutateRuntimeTask('context', 'task/a', 'cancel'); await api.purgeArchivedRuntimeTasks('context')
  await api.getPromptTemplates(); await api.getParserEngineConfig(); await api.updateParserEngineConfig({ mineru_endpoint: 'http://localhost' })
  const tiers = loadAPI('./model-tiers.ts', requests)
  await tiers.getModelTierSettings(); await tiers.updateGlobalModelTiers({ light: 'model-a' })
  assert.deepEqual(requests.map(({ url }) => url), [
    '/api/v1/system/settings', '/api/v1/system/settings/key%2Fa', '/api/v1/system/settings/key%2Fa', '/api/v1/system/settings/key%2Fa',
    '/api/v1/system/runtime/queues', '/api/v1/system/runtime/queues/context/tasks', '/api/v1/system/runtime/queues/context/tasks/task%2Fa/actions/cancel', '/api/v1/system/runtime/queues/context/archived',
    '/api/v1/space/kv/prompt-templates', '/api/v1/space/kv/parser-engine-config', '/api/v1/space/kv/parser-engine-config', '/api/v1/space/model-tiers', '/api/v1/space/model-tiers',
  ])
})

test('new workflow writes keep the all scope without mutating the draft', async () => {
  const requests: Request[] = [], api = loadAPI('./knowledge-workflows.ts', requests)
  const draft = { name: 'Object tracking', enabled: false, schedule: {}, steps: [{ id: 'facts', type: 'fact_query', config: { scope: 'all', entity: 'Object' } }] }
  await api.createKnowledgeWorkflow('kb/a', draft)
  await api.updateKnowledgeWorkflow('kb/a', 'workflow/a', draft)
  assert.equal(draft.steps[0].config.scope, 'all')
  for (const request of requests) assert.equal((request.body as any).steps[0].config.scope, 'all')
})

test('context query rejects the removed team scope before sending a request', async () => {
  const requests: Request[] = [], api = loadAPI('./knowledge-context.ts', requests)
  await api.queryKnowledgeContext('kb/a', { scope: 'all', entity: 'Object' })
  assert.throws(() => api.queryKnowledgeContext('kb/a', { scope: 'team', entity: 'Object' }), /scope must be knowledge_base, domain or all/)
  assert.equal(requests.length, 1)
  assert.deepEqual(requests[0], { method: 'post', url: '/api/v1/knowledge-context/query', body: { scope: 'all', entity: 'Object' } })
})

test('workflow fact queries reject the removed team scope before saving', () => {
  const requests: Request[] = [], api = loadAPI('./knowledge-workflows.ts', requests)
  const draft = { name: 'Object tracking', enabled: false, schedule: {}, steps: [{ id: 'facts', type: 'fact_query', config: { scope: 'team', entity: 'Object' } }] }
  const error = /scope must be knowledge_base, domain or all/
  assert.throws(() => api.createKnowledgeWorkflow('kb/a', draft), error)
  assert.throws(() => api.updateKnowledgeWorkflow('kb/a', 'workflow/a', draft), error)
  assert.equal(draft.steps[0].config.scope, 'team')
  assert.equal(requests.length, 0)
})

test('legacy agent tool selections retain the context capability requirements and save the current name', () => {
  const capabilities = { vector: false, keyword: false, context: true }
  assert.deepEqual(evaluateToolRequirement('query_knowledge_graph', capabilities, true), evaluateToolRequirement('query_knowledge_context', capabilities, true))
  assert.deepEqual(deriveKbFilterFromTools(['query_knowledge_graph']), deriveKbFilterFromTools(['query_knowledge_context']))
  assert.deepEqual(normalizeToolNames(['thinking', 'query_knowledge_graph', 'query_knowledge_context']), ['thinking', 'query_knowledge_context'])
})

test('the platform knowledge list remains the entry point after removing the standalone home route', () => {
  const routes = createApplicationRoutes()
  assert.equal(routes.find(route => route.path === '/')?.redirect, '/platform/knowledge-bases')
  assert.ok(!routes.some(route => route.path === '/knowledgeBase' || route.name === 'home'))
  assert.ok(routes.find(route => route.path === '/platform')?.children?.some(route => route.path === 'knowledge-bases'))
})
