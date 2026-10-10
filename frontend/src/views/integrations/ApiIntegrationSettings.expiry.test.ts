import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { createSSRApp, defineComponent, h, onServerPrefetch } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import * as personalServerError from '../../utils/personalServerError'

const require = createRequire(import.meta.url)
const { DatePicker, Input, ConfigProvider } = require('tdesign-vue-next')
const localeConfigs = {
  'zh-CN': require('tdesign-vue-next/cjs/locale/zh_CN').default,
  'en-US': require('tdesign-vue-next/cjs/locale/en_US').default,
}
const source = readFileSync(process.env.RELIQUARY_API_SETTINGS_SOURCE || new URL('./ApiIntegrationSettings.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename: 'ApiIntegrationSettings.vue' })
const script = compileScript(descriptor, { id: 'api-expiry-test' })
const template = compileTemplate({
  id: 'api-expiry-test', filename: 'ApiIntegrationSettings.vue', source: descriptor.template!.content,
  ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings },
})
assert.deepEqual(template.errors, [])
const stub = defineComponent({ setup(_, { slots }) { return () => h('div', slots.default?.()) } })

async function scenario(locale: 'zh-CN' | 'en-US', action: (state: any) => Promise<void>, knowledgeBases: Array<{ id: string; name: string }> | Error = []) {
  const requests: Array<{ id?: number; payload: object }> = []
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
    'tdesign-vue-next': { MessagePlugin: { success() {} }, DialogPlugin: {} },
    '@/utils/personalServerError': personalServerError,
    '@/utils/api-base': { getApiBaseUrl: () => 'http://localhost' },
    '@/utils/clipboard': { copyWithToast() {} },
    '@/api/knowledge-base': { listKnowledgeBases: async () => { if (knowledgeBases instanceof Error) throw knowledgeBases; return { success: true, data: knowledgeBases } } },
    '@/api/tenant': {
      listTenantAPIKeys: async () => ({ success: true, data: [] }),
      createTenantAPIKey: async (payload: object) => { requests.push({ payload }); return { success: true, data: { token: 'token' } } },
      updateTenantAPIKey: async (id: number, payload: object) => { requests.push({ id, payload }); return { success: true } },
    },
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true } }).outputText
    new Function('require', 'module', 'exports', compiled)((name: string) => name.endsWith('.vue')
      ? { __esModule: true, default: stub } : modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  let state: any
  component.setup = (props: object, context: object) => {
    state = setup(props, context)
    onServerPrefetch(async () => {
      state.openCreate()
      state.formRef.value = { validate: async () => true }
      await action(state)
    })
    return state
  }
  let pickerProps: any
  const picker = defineComponent({ props: DatePicker.props, setup(props, { attrs }) {
    pickerProps = props
    return () => h(DatePicker, { ...attrs, ...props })
  } })
  const app = createSSRApp({ render: () => h(ConfigProvider, { globalConfig: localeConfigs[locale] }, () => h(component)) })
  for (const name of ['t-form', 't-form-item', 't-button', 't-icon', 't-alert', 't-empty', 't-loading', 't-table', 't-textarea', 't-select']) app.component(name, stub)
  app.component('t-input', Input)
  app.component('t-date-picker', picker)
  const warnings: string[] = []
  const errors: unknown[] = []
  app.config.warnHandler = warning => warnings.push(warning)
  app.config.errorHandler = error => errors.push(error)
  const html = await renderToString(app)
  assert.deepEqual(errors, [])
  return { state, requests, pickerProps, warnings, html }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: expiry control uses real TDesign date and time picker without Vue prop warnings`, async () => {
    const result = await scenario(locale, async state => { state.form.expiresAt = '2035-01-15T08:45' })
    assert.deepEqual(result.warnings, [])
    assert.ok(result.pickerProps, 'the expiry field renders the date picker')
    assert.equal(result.pickerProps.enableTimePicker, true)
    assert.equal(result.pickerProps.clearable, true)
    assert.equal(result.pickerProps.format, 'YYYY-MM-DD HH:mm')
    assert.equal(result.pickerProps.valueType, 'YYYY-MM-DD HH:mm')
    assert.equal(result.pickerProps.timePickerProps.format, 'HH:mm')
    assert.equal(result.pickerProps.modelValue, '2035-01-15 08:45')
    assert.equal(result.pickerProps.popupProps.zIndex, 6200)
    assert.ok(result.html.includes('2035-01-15 08:45'))
    assert.ok(result.html.includes(locale === 'zh-CN' ? '有效期' : 'Expiry'))
    assert.ok(result.html.includes(localeConfigs[locale].datePicker.placeholder.date))
    assert.doesNotMatch(source, /type=["']datetime-local["']/)
  })

  test(`${locale}: selecting local minute expiry submits the same Unix value as before`, async () => {
    const result = await scenario(locale, async state => {
      state.form.name = ' Automation '
      state.expiryPickerValue.value = '2035-07-15 08:45'
      assert.equal(state.form.expiresAt, '2035-07-15T08:45')
      await state.save()
    })
    assert.deepEqual(result.requests, [{ payload: { name: 'Automation', expires_at_unix: Math.floor(new Date('2035-07-15T08:45').getTime() / 1000) } }])
  })

  test(`${locale}: editing preserves the original local-time conversion and clearing omits expiry`, async () => {
    const expiresAt = '2035-01-15T08:45:37Z'
    const expected = new Date(new Date(expiresAt).getTime() - new Date(expiresAt).getTimezoneOffset() * 60000).toISOString().slice(0, 16)
    const result = await scenario(locale, async state => {
      state.openEdit({ id: 17, name: 'Existing', expires_at: expiresAt })
      assert.equal(state.form.expiresAt, expected)
      assert.equal(state.expiryPickerValue.value, expected.replace('T', ' '))
      await state.save()
      state.openEdit({ id: 17, name: 'Existing', expires_at: expiresAt })
      state.expiryPickerValue.value = ''
      assert.equal(state.form.expiresAt, '')
      await state.save()
    })
    assert.deepEqual(result.requests, [
      { id: 17, payload: { name: 'Existing', expires_at_unix: Math.floor(new Date(expected).getTime() / 1000) } },
      { id: 17, payload: { name: 'Existing' } },
    ])
  })
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: Playground includes the selected knowledge scope in its request`, async () => {
    const originalFetch = globalThis.fetch
    const calls: any[] = []
    globalThis.fetch = async (_url, options) => { calls.push(options); return { ok: true, json: async () => ({ data: [] }) } as Response }
    try {
      await scenario(locale, async state => {
        if (state.openPlayground) await state.openPlayground(); else state.playgroundVisible.value = true
        if (state.openPlayground) assert.equal(state.playground.knowledgeBaseId, 'kb-1')
        state.playground.apiKey = ' secret '
        state.playground.query = ' question '
        await state.runQuery()
      }, [{ id: 'kb-1', name: 'Knowledge' }])
      await scenario(locale, async state => {
        if (state.openPlayground) await state.openPlayground(); else state.playgroundVisible.value = true
        if (state.openPlayground) assert.equal(state.playground.knowledgeBaseId, '')
        state.playground.knowledgeBaseId = 'kb-2'
        state.playground.apiKey = 'secret'; state.playground.query = 'question'
        await state.runQuery()
      }, [{ id: 'kb-1', name: 'One' }, { id: 'kb-2', name: 'Two' }])
      assert.equal(calls.length, 2)
      assert.deepEqual(JSON.parse(calls[1].body), { query: 'question', knowledge_base_ids: ['kb-2'] })
      assert.deepEqual(JSON.parse(calls[0].body), { query: 'question', knowledge_base_ids: ['kb-1'] })
      assert.equal(calls[0].headers['X-API-Key'], 'secret')
    } finally { globalThis.fetch = originalFetch }
  })
  test(`${locale}: Playground missing scope, empty libraries or load failure shows a hint without requesting search`, async () => {
    const originalFetch = globalThis.fetch
    let calls = 0
    globalThis.fetch = async () => { calls++; throw new Error('must not request') }
    try {
      for (const libraries of [[], [{ id: 'a', name: 'A' }, { id: 'b', name: 'B' }], new Error('offline')]) {
        await scenario(locale, async state => {
          if (state.openPlayground) await state.openPlayground(); else state.playgroundVisible.value = true
          if (state.openPlayground) assert.ok(state.playgroundError.value || state.knowledgeBaseOptions.value.length === 2)
          state.playground.apiKey = 'secret'; state.playground.query = 'question'
          await state.runQuery()
          assert.ok(state.playgroundError.value)
          const messages = (locale === 'zh-CN' ? zh : en).integrations.api
          const expected = libraries instanceof Error ? messages.playgroundLoadKnowledgeBasesFailed
            : libraries.length ? messages.playgroundNeedKnowledgeBase : messages.playgroundNoKnowledgeBases
          assert.equal(state.playgroundError.value, expected)
          if (!(libraries instanceof Error) && libraries.length) {
            state.playground.knowledgeBaseId = 'deleted-kb'
            await state.runQuery()
            assert.equal(state.playgroundError.value, expected)
          }
        }, libraries)
      }
      assert.equal(calls, 0)
    } finally { globalThis.fetch = originalFetch }
  })
}
