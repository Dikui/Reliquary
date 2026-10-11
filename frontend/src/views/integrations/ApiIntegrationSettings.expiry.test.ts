import assert from 'node:assert/strict'
import { readFileSync, appendFileSync } from 'node:fs'
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
import * as searchHelpers from '../../utils/knowledgeSearch'

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

async function scenario(locale: 'zh-CN' | 'en-US', action: (state: any) => Promise<void>, options: { baseUrl?: string; origin?: string; keys?: unknown[] } = {}) {
  const requests: Array<{ id?: number; payload: object }> = []
  const copies: Array<{ value: string; successKey: string }> = []
  const buttons: Array<{ label: unknown; click: (() => unknown) | undefined }> = []
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
    'tdesign-vue-next': { MessagePlugin: { success() {} }, DialogPlugin: {} },
    '@/utils/personalServerError': personalServerError,
    '@/utils/api-base': { getApiBaseUrl: () => options.baseUrl ?? 'http://localhost' },
    '@/utils/clipboard': { copyWithToast(value: string, successKey: string) { copies.push({ value, successKey }) } },
    '@/utils/knowledgeSearch': searchHelpers,
    '@/api/knowledge-search': { getKnowledgeSearchOptions: async () => ({ success: true, data: { entities: [], topics: [] } }) },
    '@/api/space': {
      listAPIKeys: async () => ({ success: true, data: options.keys ?? [] }),
      createAPIKey: async (payload: object) => { requests.push({ payload }); return { success: true, data: { token: 'token' } } },
      updateAPIKey: async (id: number, payload: object) => { requests.push({ id, payload }); return { success: true } },
    },
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true } }).outputText
    new Function('require', 'module', 'exports', 'window', compiled)((name: string) => name.endsWith('.vue')
      ? { __esModule: true, default: stub } : modules[name] ?? require(name), module, module.exports, { location: { origin: options.origin ?? 'http://localhost' } })
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
  for (const name of ['t-form', 't-form-item', 't-icon', 't-alert', 't-empty', 't-loading', 't-textarea', 't-select']) app.component(name, stub)
  app.component('t-button', defineComponent({ setup(_, { attrs, slots }) {
    buttons.push({ label: attrs['aria-label'], click: attrs.onClick as (() => unknown) | undefined })
    return () => h('button', attrs, slots.default?.())
  } }))
  app.component('t-table', defineComponent({ props: ['data'], setup(props, { slots }) { return () => h('section', { 'data-api-key-table': '' }, props.data?.map((row: unknown) => h('div', Object.values(slots).flatMap(slot => slot?.({ row }) ?? [])))) } }))
  app.component('t-input', Input)
  app.component('t-date-picker', picker)
  const warnings: string[] = []
  const errors: unknown[] = []
  app.config.warnHandler = warning => warnings.push(warning)
  app.config.errorHandler = error => errors.push(error)
  const html = await renderToString(app)
  assert.deepEqual(errors, [])
  return { state, requests, copies, buttons, pickerProps, warnings, html }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: external Agent endpoints have localized guidance and copy their addresses`, async () => {
    const messages = (locale === 'zh-CN' ? zh : en).integrations.api
    const result = await scenario(locale, async () => {})
    assert.deepEqual(result.warnings, [])
    for (const text of [messages.ingestUrl, messages.mcpUrl, 'POST http://localhost/api/v1/ingest', 'http://localhost/mcp', 'Streamable HTTP', 'Authorization: Bearer API_KEY']) assert.ok(result.html.includes(text), text)
    assert.ok(result.html.includes('pending_review'))
    assert.ok(result.html.includes(locale === 'zh-CN' ? '调用方的判断不替代审核' : 'judgment does not replace review'))
    for (const label of [messages.copyIngestUrl, messages.copyMcpUrl]) {
      const button = result.buttons.find(button => button.label === label)
      assert.ok(button?.click, label)
      await button.click()
    }
    assert.deepEqual(result.copies, [
      { value: 'http://localhost/api/v1/ingest', successKey: 'common.copySuccess' },
      { value: 'http://localhost/mcp', successKey: 'common.copySuccess' },
    ])
    assert.deepEqual(result.requests, [])
  })
  test(`${locale}: external Agent endpoints preserve the reverse proxy path and use an absolute origin`, async () => {
    const result = await scenario(locale, async () => {}, { baseUrl: '/app/reliquary/', origin: 'https://reliquary.internal' })
    assert.deepEqual(result.warnings, [])
    assert.ok(result.html.includes('POST https://reliquary.internal/app/reliquary/api/v1/ingest'))
    assert.ok(result.html.includes('https://reliquary.internal/app/reliquary/mcp'))
  })
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
  test(`${locale}: Playground searches the unique knowledge base without range parameters`, async () => {
    const originalFetch = globalThis.fetch, calls: any[] = []
    globalThis.fetch = async (_url, options) => { calls.push(options); return { ok: true, json: async () => ({ data: [] }) } as Response }
    try {
      await scenario(locale, async state => {
        state.openPlayground()
        state.playground.apiKey = ' secret '
        state.playground.query = ' question '
        await state.runQuery()
      })
      assert.equal(calls.length, 1)
      assert.deepEqual(JSON.parse(calls[0].body), { query: 'question', collapse_similar: true, limit: 20 })
      assert.equal(calls[0].headers['X-API-Key'], 'secret')
    } finally { globalThis.fetch = originalFetch }
  })
  test(`${locale}: Playground requires an API key and query before requesting search`, async () => {
    const originalFetch = globalThis.fetch
    let calls = 0
    globalThis.fetch = async () => { calls++; throw new Error('must not request') }
    try {
      await scenario(locale, async state => {
        state.openPlayground()
        await state.runQuery()
        assert.equal(state.playgroundError.value, (locale === 'zh-CN' ? zh : en).integrations.api.playgroundNeedQuestionAndKey)
        state.playground.apiKey = 'secret'
        await state.runQuery()
        assert.ok(state.playgroundError.value)
      })
      assert.equal(calls, 0)
    } finally { globalThis.fetch = originalFetch }
  })
  test(`${locale}: Playground sends every article filter and previews the request without the API key`, async () => {
    const originalFetch = globalThis.fetch, calls: any[] = []
    globalThis.fetch = async (_url, options) => { calls.push(options); return { ok: true, json: async () => ({ success: true, data: { results: [], excluded_missing_published_at: 1 } }) } as Response }
    try {
      const result = await scenario(locale, async state => {
        state.openPlayground()
        state.playground.apiKey = 'secret'
        state.playground.query = ' Launch '
        state.playgroundFilters.value = { ...searchHelpers.defaultSearchFilters(), content_types: ['event', 'research'], entity_ids: ['e1', 'e2'], topic_ids: ['t1'], sources: ['article', 'mine'], published_from: '2026-10-01', published_to: '2026-10-11', collapse_similar: false, limit: 50 }
        await state.runQuery()
        const expected = { query: 'Launch', content_types: ['event', 'research'], entity_ids: ['e1', 'e2'], topic_ids: ['t1'], sources: ['article', 'mine'], published_from: '2026-10-01', published_to: '2026-10-11', collapse_similar: false, limit: 50 }
        assert.deepEqual(JSON.parse(calls[0].body), expected)
        assert.deepEqual(JSON.parse(state.playgroundRequestPreview.value), expected)
        assert.doesNotMatch(state.playgroundRequestPreview.value, /secret|apiKey/)
        assert.equal(JSON.parse(state.playgroundResult.value).data.excluded_missing_published_at, 1)
      })
      assert.ok(result.html.includes((locale === 'zh-CN' ? zh : en).knowledgeSearch.requestPreview))
    } finally { globalThis.fetch = originalFetch }
  })
  test(`${locale}: Playground rejects reversed publication dates before making a request`, async () => {
    const originalFetch = globalThis.fetch
    let calls = 0
    globalThis.fetch = async () => { calls++; throw new Error('must not request') }
    try {
      await scenario(locale, async state => {
        state.playground.apiKey = 'secret'; state.playground.query = 'Launch'
        state.playgroundFilters.value.published_from = '2026-10-12'; state.playgroundFilters.value.published_to = '2026-10-11'
        await state.runQuery()
        assert.equal(state.playgroundError.value, (locale === 'zh-CN' ? zh : en).knowledgeSearch.invalidDateRange)
      })
      assert.equal(calls, 0)
    } finally { globalThis.fetch = originalFetch }
  })
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: Playground failure shows safe code and localized text without upstream HTML`, async () => {
    const originalFetch = globalThis.fetch
    try {
      for (const json of [async () => ({ success: false, error: { code: 2400, message: '<html>upstream secret</html>' } }), async () => { throw new SyntaxError('<html>upstream secret</html>') }]) {
        globalThis.fetch = async () => ({ ok: false, json }) as unknown as Response
        const result = await scenario(locale, async state => {
          state.openPlayground(); state.playground.apiKey = 'test'; state.playground.query = 'Launch'
          await state.runQuery()
        })
        assert.ok(result.state.playgroundError.value.includes(messagesFor(locale)))
        assert.doesNotMatch(result.state.playgroundError.value + result.html, /upstream|html&gt;/)
        assert.equal(result.state.playgroundResult.value, '')
      }
    } finally { globalThis.fetch = originalFetch }
    function messagesFor(value: typeof locale) { return value === 'zh-CN' ? '检索' : 'Search' }
  })
}

for (const locale of ['zh-CN', 'en-US'] as const) {
 test(`${locale}: restored API keys load as invalidated and offer regeneration`, async () => {
  const copy = (locale === 'zh-CN' ? zh : en).integrations.api
  const result = await scenario(locale, async state => { await state.load() }, { keys: [{id: 15, name: 'Old collector', api_key: '', status: 'migration_invalidated', created_at: '2026-01-01'}] })
  assert.ok(result.html.includes(copy.migrationInvalidated)); assert.ok(result.html.includes(copy.regenerateMigratedKey)); assert.ok(!result.html.match(/data-api-key-table[^>]*>([\s\S]*?)<\/section>/)?.[1].includes(copy.apiKeyNeverExpires))
  result.state.openCreate(); assert.equal(result.state.editingId.value, null)
 })
}

test('migration replacement Key page save creates only the requested name',async()=>{
 const {requests}=await scenario('zh-CN',async state=>{
  await state.load();state.openCreate();state.form.name='Replacement collector';await state.save()
 },{keys:[{id:1,name:'Old collector',status:'migration_invalidated'}]})
 assert.deepEqual(requests,[{payload:{name:'Replacement collector'}}])
 if(process.env.RELIQUARY_RECOVERY_REQUESTS)appendFileSync(process.env.RELIQUARY_RECOVERY_REQUESTS,JSON.stringify({kind:'new_api_key',method:'POST',body:requests[0].payload})+'\n')
})
