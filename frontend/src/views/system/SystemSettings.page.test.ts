import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { createSSRApp, onServerPrefetch } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import * as edits from './systemSettingsEdit'
import { settingsTestUi } from '../settings/settingsTestUi'

const require = createRequire(import.meta.url)
const { descriptor } = parse(readFileSync(new URL('./SystemSettings.vue', import.meta.url), 'utf8'))
const script = compileScript(descriptor, { id: 'settings-api' })
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: system setting changes preserve types, failed edits and confirmed whitelist deltas`, async () => {
    const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
    const calls: any[] = []
    let confirmResult = false
    const items = [
      { key: 'queue.core_concurrency', value: 8, value_type: 'int' },
      { key: 'feature.enabled', value: true, value_type: 'bool' },
      { key: 'ssrf.whitelist', value: ['localhost'], value_type: 'string_list' },
    ]
    const modules: Record<string, unknown> = {
      'vue-i18n': { useI18n: () => i18n.global }, '@/styles/settings-form.css': {},
      '@/components/ui': settingsTestUi({ confirm: (options: any) => { calls.push({ confirmation: options }); return { result: Promise.resolve(confirmResult) } } }),
      './QueueConnectionNotice.vue': { default: {} },
      './systemSettingsEdit': edits,
      '@/stores/deploymentCapabilities': { useDeploymentCapabilitiesStore: () => ({}) },
      '@/api/system': {
        getQueueConnectionStatus: async () => ({ state: 'active' }),
        listSystemSettings: async () => structuredClone(items),
        updateSystemSetting: async (key: string, value: unknown) => {
          calls.push({ key, value }); if (value === 99) throw new Error('fixture rejected')
          return { ...items.find(item => item.key === key), value }
        },
        resetSystemSetting: async (key: string) => { calls.push({ reset: key }) },
      },
    }
    const module = { exports: {} as any }
    const code = ts.transpileModule(script.content, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', code)((name: string) => modules[name] ?? require(name), module, module.exports)
    const component = module.exports.default, setup = component.setup
    component.setup = (props: object, context: object) => {
      const state = setup(props, context)
      onServerPrefetch(async () => {
        await state.loadSettings()
        state.editValues['queue.core_concurrency'] = 12
        await state.onChange(state.settings.value[0])
        assert.deepEqual(calls.at(-1), { key: 'queue.core_concurrency', value: 12 })
        state.editValues['feature.enabled'] = false
        await state.onChange(state.settings.value[1])
        assert.deepEqual(calls.at(-1), { key: 'feature.enabled', value: false })
        state.editValues['queue.core_concurrency'] = 99
        await state.onChange(state.settings.value[0])
        assert.equal(state.editValues['queue.core_concurrency'], 12)
        state.editValues['ssrf.whitelist'] = ['localhost', 'example.test']
        await state.onSsrfWhitelistChange()
        assert.deepEqual(state.editValues['ssrf.whitelist'], ['localhost'])
        assert.equal(calls.filter(call => call.key === 'ssrf.whitelist').length, 0)
        confirmResult = true
        state.editValues['ssrf.whitelist'] = ['example.test']
        await state.onSsrfWhitelistChange()
        assert.deepEqual(calls.at(-1), { key: 'ssrf.whitelist', value: ['example.test'] })
        assert.equal(calls.filter(call => call.confirmation).length, 3)
        await state.resetSetting(state.settings.value[0])
        assert.deepEqual(calls.at(-1), { reset: 'queue.core_concurrency' })
        assert.equal(state.editValues['queue.core_concurrency'], 8)
      })
      return () => null
    }
    await renderToString(createSSRApp(component).use(i18n))
  })
}
