import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test, { type TestContext } from 'node:test'
import axios, { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { createPinia, defineStore, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import { translatePersonalServerMessage } from './personalServerError'
import { element, loadComponent, renderer, settle, visibleText } from '../views/knowledge-base/componentTestHarness'

const require = createRequire(import.meta.url)
function evaluate(path: string, modules: Record<string, any>) {
 const code = ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
 const value = { exports: {} as any }
 new Function('require', 'module', 'exports', code)((name: string) => modules[name] ?? require(name), value, value.exports)
 return value.exports
}
function fixture(t: TestContext, locale: 'zh-CN' | 'en-US' = 'zh-CN', refresh = false, previousCache = false) {
 const priorStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage'), priorWindow = Object.getOwnPropertyDescriptor(globalThis, 'window')
 const storage = new Map([['reliquary_token', 'valid-session'], ['reliquary_user', JSON.stringify({ id: 'user-1', space_id: 1 })]])
 if (previousCache) {
  storage.set('reliquary_user', JSON.stringify({ id: 'user-1', 'tenant_id': 1 }))
  storage.set('reliquary_tenant', JSON.stringify({ id: '1', name: 'Original personal space' }))
 }
 if (refresh) storage.set('reliquary_refresh_token', 'valid-refresh')
 Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: (key: string) => storage.get(key) ?? null, setItem: (key: string, value: string) => storage.set(key, value), removeItem: (key: string) => storage.delete(key) } })
 const location = { pathname: '/platform/settings', href: '/platform/settings' }
 Object.defineProperty(globalThis, 'window', { configurable: true, value: { location } })
 const transport = { mode: 'healthy', refreshMode: '503', spaceName: 'Personal' }, calls: InternalAxiosRequestConfig[] = [], modules: Record<string, any> = {}
 const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
 const network = { __esModule: true, default: { create: (config: object) => axios.create({ ...config, adapter: async (packet: InternalAxiosRequestConfig) => {
  calls.push(packet)
  const mode = packet.url?.includes('/auth/refresh') ? transport.refreshMode : transport.mode
  if (mode !== 'healthy') {
   if (mode === 'network' || mode === 'timeout') throw new AxiosError('offline', mode === 'timeout' ? AxiosError.ECONNABORTED : AxiosError.ERR_NETWORK, packet)
   const status = Number(mode)
   throw new AxiosError('failed', AxiosError.ERR_BAD_RESPONSE, packet, undefined, { status, statusText: 'Unavailable', headers: {}, config: packet, data: { success: false, error: { code: status === 503 ? 1008 : 1001, message: 'Authentication temporarily unavailable' } } })
  }
  if (packet.url?.startsWith('/api/v1/knowledge-bases/kb-entries/entries')) {
    return { status: 200, statusText: 'OK', headers: {}, config: packet, data: { success: true, data: [{ id: 'loaded-entry', source_kind: 'mine', title: 'Original document', updated_at: '2026-10-01T00:00:00Z' }], total: 1 } }
  }
  if (packet.url === '/api/v1/entries/loaded-entry/spans') {
    return { status: 200, statusText: 'OK', headers: {}, config: packet, data: { success: true, data: { entry_id: 'loaded-entry', spans: [] } } }
  }
  if (packet.url === '/api/v1/space') {
   if (packet.method === 'put') transport.spaceName = JSON.parse(packet.data).name
   return { status: 200, statusText: 'OK', headers: {}, config: packet, data: { success: true, data: { id: 1, space_id: 1, name: transport.spaceName } } }
  }
  return { status: 200, statusText: 'OK', headers: {}, config: packet, data: { success: true, data: { user: { id: 'user-1', space_id: 1, username: 'owner' }, space: { id: 1, name: 'Personal' } } } }
 } }) } }
 Object.assign(modules, { pinia: { defineStore }, axios: network, '@/i18n': { __esModule: true, default: i18n }, './index': { generateRandomString: () => 'availability-test', MAX_FILE_SIZE_MB: 100 }, './api-base': { getApiBaseUrl: () => 'http://availability.test' }, './personalServerError': { translatePersonalServerMessage }, '@/composables/useFont': { reloadFontFromStorage() {} }, '@/composables/useTheme': { reloadThemeFromStorage() {} }, '@/composables/preferenceStorage': { resetMigrationLatch() {} }, '@/stores/resources': { useResourcesStore: () => ({ invalidate() {} }) }, '@/stores/editorResources': { useEditorResourcesStore: () => ({ invalidate() {} }) } })
 const request = evaluate('./request.ts', modules)
 modules['@/utils/request'] = request
 const spaceApi = evaluate('../api/space.ts', modules)
 const api = evaluate('../api/auth.ts', modules)
 modules['@/api/auth'] = api; modules['../api/auth'] = api
 setActivePinia(createPinia())
 const authModule = evaluate('../stores/auth.ts', modules)
 modules['@/stores/auth'] = authModule
 const store = authModule.useAuthStore()
 const notice = loadComponent(new URL('../components/ServiceAvailabilityNotice.vue', import.meta.url), { '@/utils/request': request })
 const app = renderer.createApp(notice), root = element('root')
 app.use(i18n); app.mount(root)
 const noticeRoot = (app as any)._instance.subTree.target ?? root
 t.after(() => {
  app.unmount(); request.stopServiceAvailabilityRetry()
  if (priorStorage) Object.defineProperty(globalThis, 'localStorage', priorStorage); else delete (globalThis as any).localStorage
  if (priorWindow) Object.defineProperty(globalThis, 'window', priorWindow); else delete (globalThis as any).window
 })
 return { request, store, spaceApi, transport, storage, location, calls, modules, i18n, root: noticeRoot }
}
for (const locale of ['zh-CN', 'en-US'] as const) {
 for (const mode of ['503', 'network', 'timeout']) {
  test(`${locale}: actual auth store and request interceptor preserve session on ${mode} and clear global notice after recovery`, async t => {
   const f = fixture(t, locale); f.transport.mode = mode
   assert.equal(await f.store.refreshFromAuthMe(), false); await settle()
   assert.equal(f.store.isLoggedIn, true); assert.equal(f.storage.get('reliquary_token'), 'valid-session')
   assert.equal(f.location.href, '/platform/settings')
   assert.ok(visibleText(f.root).includes((locale === 'zh-CN' ? zh : en).error.serviceUnavailable))
   f.transport.mode = 'healthy'; await f.request.retryServiceAvailability(); await settle()
   assert.equal(f.request.serviceUnavailable.value, false); assert.equal(visibleText(f.root), '')
   assert.equal(f.store.isLoggedIn, true); assert.equal(f.calls.at(-1)?.url, '/api/v1/auth/me')
   assert.equal(f.calls.at(-1)?.headers.Authorization, 'Bearer valid-session')
  })
 }
}
for (const refreshMode of ['503', 'network', 'timeout', '401']) {
 test(`expired access token with ${refreshMode} refresh preserves session except explicit 401`, async t => {
  const f = fixture(t, 'zh-CN', true); f.transport.mode = '401'; f.transport.refreshMode = refreshMode
  assert.equal(await f.store.refreshFromAuthMe(), false)
  const invalid = refreshMode === '401'
  assert.equal(f.store.isLoggedIn, !invalid); assert.equal(f.storage.has('reliquary_token'), !invalid)
  assert.equal(f.location.href, invalid ? '/login' : '/platform/settings')
  assert.ok(f.calls.some(packet => packet.url === '/api/v1/auth/refresh'))
 })
}
test('explicit protected 401 without refresh clears the real auth store and redirects', async t => {
 const f = fixture(t); f.transport.mode = '401'
 assert.equal(await f.store.refreshFromAuthMe(), false)
 assert.equal(f.store.isLoggedIn, false); assert.equal(f.storage.has('reliquary_token'), false); assert.equal(f.location.href, '/login')
})

for (const locale of ['zh-CN', 'en-US'] as const) {
 test(`${locale}: persisted browser session upgrades before loading current HTTP data`, async t => {
  const f = fixture(t, locale, false, true)
  assert.equal(f.store.user.space_id, '1')
  assert.equal(f.store.effectiveSpaceId, 1)
  assert.equal(f.store.currentSpaceName, 'Original personal space')
  assert.equal(await f.store.refreshFromAuthMe(), true)
  assert.equal(f.store.user.space_id, '1')
  assert.equal(JSON.parse(f.storage.get('reliquary_user')!).space_id, '1')
  assert.equal(JSON.parse(f.storage.get('reliquary_tenant')!).name, 'Personal')
 })
 test(`${locale}: personal space data loads through Axios interceptors, API and the real auth store with space_id`, async t => {
  const f = fixture(t, locale)
  assert.equal(await f.store.refreshFromAuthMe(), true)
  assert.equal(f.store.user.space_id, '1')
  assert.equal(f.store.effectiveSpaceId, 1)
  assert.equal(JSON.parse(f.storage.get('reliquary_user')!).space_id, '1')
  const loaded = await f.spaceApi.getSpaceInfo()
  assert.equal(loaded.data.space_id, 1)
  await f.spaceApi.updateSpace({ name: 'Updated personal space' })
  assert.equal((await f.spaceApi.getSpaceInfo()).data.name, 'Updated personal space')
  assert.equal(f.calls.at(-1)?.headers.Authorization, 'Bearer valid-session')
  assert.ok(f.calls.some(packet => packet.url === '/api/v1/space' && packet.method === 'put'))
 })
}

for (const locale of ['zh-CN', 'en-US'] as const) {
 test(`${locale}: entry requests load through Axios interceptors and the actual Pinia store`, async t => {
  const f=fixture(t,locale)
  f.modules['../utils/request']=f.request
  const api=evaluate('../api/knowledge-base.ts',f.modules)
  const storeModule=evaluate('../stores/entry.ts',f.modules)
  const modules={...f.modules,pinia:require('pinia'),'@/api/knowledge-base':api,'@/stores/entry':storeModule,
    '../utils/index': { formatStringDate:()=> '2026-10-01', kbFileTypeVerification:()=>true },
    'tdesign-vue-next':{MessagePlugin:{}},'vue-i18n':{useI18n:()=>({t:f.i18n.global.t})},
    './useEntryDuplicates':{useEntryDuplicates:()=>({})}}
  const hook=evaluate('../composables/useKnowledgeBase.ts',modules).default('kb-entries')
  await hook.loadEntries()
  assert.equal(storeModule.entryStore().cardList[0].id,'loaded-entry')
  assert.equal(hook.total.value,1)
  const spans=await api.getEntrySpans('loaded-entry')
  assert.equal(spans.data.entry_id,'loaded-entry')
  const list=f.calls.find(packet=>packet.url?.startsWith('/api/v1/knowledge-bases/kb-entries/entries'))
  assert.ok(list)
  assert.equal(list.headers.Authorization,'Bearer valid-session')
  assert.equal(f.calls.at(-1)?.url,'/api/v1/entries/loaded-entry/spans')
  assert.ok(!f.calls.some(packet=>packet.url?.startsWith('/api/v1/knowledge/')))
 })
}
