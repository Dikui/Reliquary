import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import { descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../knowledge-base/componentTestHarness'

const require = createRequire(import.meta.url)
const moduleOf = (value: unknown) => ({ __esModule: true, default: value })
for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const page of ['RuntimeQueues', 'SystemSettings'] as const) {
    for (const alert of [false, true]) {
      test(`${locale}: ${page} loads reconnect ${alert ? 'alert' : 'notice'} through actual health API`, async () => {
        const calls: string[] = []
        const api = { exports: {} as any }
        const code = ts.transpileModule(readFileSync(new URL('../../api/system.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
        new Function('require', 'module', 'exports', code)((name: string) => name === '@/utils/request' ? {
          get: async (url: string) => {
            calls.push(url)
            assert.equal(url, '/health', 'database-dependent requests must wait during reconnect')
            return { status: 'ok', queue_connection: { state: 'reconnecting', last_error: 'PostgreSQL queue connection is unavailable', alert, alert_after_seconds: 300 } }
          },
        } : require(name), api, api.exports)
        const notice = loadComponent(new URL('./QueueConnectionNotice.vue', import.meta.url), { '@/api/system': api.exports })
        const component = loadComponent(new URL(`./${page}.vue`, import.meta.url), {
          './QueueConnectionNotice.vue': moduleOf(notice), '@/api/system': api.exports,
          '@/stores/deploymentCapabilities': { useDeploymentCapabilitiesStore: () => ({}) },
          './systemSettingsEdit': require('./systemSettingsEdit'),
          './runtimeTaskPagination': require('./runtimeTaskPagination'), '@/i18n/countCopy': require('../../i18n/countCopy'),
          'tdesign-vue-next': { MessagePlugin: {} },
          '@/api/chatgpt': {}, '@/components/ChatGPTUsageStatus.vue': moduleOf(defineComponent({ setup: () => () => null })),
          '@/components/settings/SettingDrawer.vue': moduleOf(defineComponent({ setup: () => () => null })),
        })
        const app = renderer.createApp(component)
        const messages = { 'zh-CN': zh, 'en-US': en }
        app.use(createI18n({ legacy: false, locale, messages })); registerPrimitives(app)
        for (const name of ['t-switch', 't-skeleton', 't-loading']) app.component(name, defineComponent({ setup: () => () => null }))
        const errors: unknown[] = [], root = element('root')
        app.config.errorHandler = error => errors.push(error)
        try {
          app.mount(root); await settle()
          const banner = descendants(root).find(node => node.props.role === 'alert')
          assert.ok(banner)
          const text = messages[locale].system.globalSettings.runtime
          assert.ok(visibleText(banner).includes(alert ? text.reconnectAlert : text.reconnecting))
          assert.ok(visibleText(banner).includes(text.latestConnectionError))
          assert.ok(visibleText(banner).includes('PostgreSQL queue connection is unavailable'))
          assert.ok(calls.includes('/health'))
          assert.deepEqual(errors, [])
        } finally { app.unmount() }
      })
    }
  }
}
