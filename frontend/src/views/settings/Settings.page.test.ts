import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import * as routerApi from 'vue-router'
import { createI18n } from 'vue-i18n'
import { doc, renderer, settle } from '../../components/ui/__tests__/dom'
import { loadComponent } from '../library/componentTestHarness'
import * as settingsRoute from '../../config/settingsRoute'
import { INTEGRATION_TAB_CAPABILITY } from '../../config/integrations'
import en from '../../i18n/locales/en-US'
import zh from '../../i18n/locales/zh-CN'

for (const locale of ['en-US', 'zh-CN']) test(`${locale}: direct settings route mounts grouped navigation and protects drafts on section and page changes`, async t => {
  let mayLeave = false, confirmations = 0
  const modules: Record<string, unknown> = {
    'vue-router': routerApi,
    '@/config/settingsRoute': settingsRoute,
    '@/config/integrations': { INTEGRATION_TAB_CAPABILITY },
    '@/stores/deploymentCapabilities': { useDeploymentCapabilitiesStore: () => ({ loaded: true, isSupported: () => true }) },
    '@/components/ui': { toast: { warning() {} } },
  }
  const components = ['GeneralSettings', 'LibrarySettings', 'ModelSettings', 'OllamaSettings', 'VaultStatus', 'SystemInfo', 'SpaceInfo', 'UserProfile']
  for (const name of components) modules[`./${name}.vue`] = { __esModule: true, default: defineComponent({
    props: ['initialSection'], setup: (props, { expose }) => { expose({ confirmLeave: async () => { confirmations++; return mayLeave } }); return () => h('article', { 'data-settings-content': name }, props.initialSection) },
  }) }
  for (const [path, name] of [['library/OperationLogs', 'logs'], ['system/SystemSettings', 'system'], ['system/RuntimeQueues', 'queues'], ['integrations/IntegrationSettingsSection', 'api']]) {
    modules[`@/views/${path}.vue`] = { __esModule: true, default: defineComponent({ render: () => h('article', { 'data-settings-content': name }) }) }
  }
  const component = loadComponent(new URL('./Settings.vue', import.meta.url), modules)
  const router = routerApi.createRouter({ history: routerApi.createMemoryHistory(), routes: [
    { path: '/platform/settings', component }, { path: '/platform/entries', component: { render: () => null } },
  ] })
  await router.push('/platform/settings?section=knowledge&subsection=models')
  const root = doc.createElement('main'); doc.body.appendChild(root)
  const app = renderer.createApp({ render: () => h(routerApi.RouterView) })
  app.use(router).use(createI18n({ legacy: false, locale, messages: { 'en-US': en, 'zh-CN': zh } }))
  app.mount(root); t.after(() => { app.unmount(); root.remove() })
  await new Promise(resolve => setImmediate(resolve)); await settle()
  assert.equal(router.currentRoute.value.query.section, 'library')
  assert.equal(root.querySelectorAll('a').length, 10)
  assert.equal(root.querySelector('[data-settings-content="LibrarySettings"]')?.textContent, 'models')
  assert.equal(root.querySelector('[role="dialog"]'), null)
  await router.push('/platform/settings?section=models'); await settle()
  assert.equal(router.currentRoute.value.query.section, 'library')
  await router.push('/platform/entries')
  assert.equal(router.currentRoute.value.path, '/platform/settings')
  assert.equal(confirmations, 2)
  mayLeave = true
  await router.push('/platform/settings?section=runtime-queues'); await settle()
  assert.ok(root.querySelector('[data-settings-content="queues"]'))
  await router.push('/platform/settings?section=integrations&tab=api')
  await new Promise(resolve => setImmediate(resolve)); await settle()
  assert.equal(router.currentRoute.value.query.section, 'integration-api')
  assert.equal(router.currentRoute.value.query.tab, undefined)
  assert.ok(root.querySelector('[data-settings-content="api"]'))
})
