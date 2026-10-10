import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'
import { createApplicationRoutes } from '../router/routes'

type Request = { method: string; url: string; body?: unknown }
function loadAPI(path: string, requests: Request[]) {
  const module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => {
    if (name === '@/i18n') return { default: { global: { t: (key: string) => key } }, __esModule: true }
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
  await api.getRuntimeQueues(); await api.getRuntimeTasks('entity', 'pending'); await api.mutateRuntimeTask('entity', 'task/a', 'cancel'); await api.purgeArchivedRuntimeTasks('entity')
  await api.getParserEngineConfig(); await api.updateParserEngineConfig({ mineru_endpoint: 'http://localhost' })
  const tiers = loadAPI('./model-tiers.ts', requests)
  await tiers.getModelTierSettings(); await tiers.updateGlobalModelTiers({ light: 'model-a' })
  assert.deepEqual(requests.map(({ url }) => url), [
    '/api/v1/system/settings', '/api/v1/system/settings/key%2Fa', '/api/v1/system/settings/key%2Fa', '/api/v1/system/settings/key%2Fa',
    '/api/v1/system/runtime/queues', '/api/v1/system/runtime/queues/entity/tasks', '/api/v1/system/runtime/queues/entity/tasks/task%2Fa/actions/cancel', '/api/v1/system/runtime/queues/entity/archived',
    '/api/v1/space/kv/parser-engine-config', '/api/v1/space/kv/parser-engine-config', '/api/v1/space/model-tiers', '/api/v1/space/model-tiers',
  ])
})

test('the platform knowledge list remains the entry point after removing the standalone home route', () => {
  const routes = createApplicationRoutes()
  assert.equal(routes.find(route => route.path === '/')?.redirect, '/platform/knowledge-bases')
  assert.ok(!routes.some(route => route.path === '/knowledgeBase' || route.name === 'home'))
  assert.ok(routes.find(route => route.path === '/platform')?.children?.some(route => route.path === 'knowledge-bases'))
})
