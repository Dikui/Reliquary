import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import enUS from '../i18n/locales/en-US'
import zhCN from '../i18n/locales/zh-CN'
import { DEFAULT_MODEL_CONTEXT_WINDOW } from '../utils/contextWindow'
import { defaultThinkingControl, resolveThinkingControl } from '../utils/thinkingControl'
import { shouldShowOllamaUnavailableTip } from './modelEditorSourceState'
import { click, descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../views/knowledge-base/componentTestHarness'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const originalModel = { slug: 'original-slug', display_name: 'Original catalog model' }
const addedModel = { slug: 'refreshed-slug', display_name: 'Refreshed catalog model' }

async function mountEditor(locale: 'zh-CN' | 'en-US', edit: boolean, noAccounts = false) {
  const calls: string[] = []
  let catalog = [originalModel]
  let pending: { resolve: (value: unknown) => void; reject: (error: Error) => void } | undefined
  let deferCatalog = false
  const request = { get: async (path: string) => {
    calls.push(path)
    if (path === '/api/v1/chatgpt/accounts') return { success: true, data: { accounts: noAccounts ? [] : ['account-a', 'account-b'].map(id => ({ id, email: `${id}@example.com`, subject: id, client_id: id, status: 'connected', scopes: [] })), encryption_ready: true, callback_url: '' } }
    assert.equal(path, `/api/v1/chatgpt/accounts/${edit ? 'account-b' : 'account-a'}/models`)
    if (deferCatalog) return new Promise((resolve, reject) => { pending = { resolve, reject } })
    return { success: true, data: structuredClone(catalog) }
  } }
  const wrapper = defineComponent({ props: ['visible'], setup: (props, { slots }) => () => props.visible ? h('section', slots.default?.()) : null })
  const modules: Record<string, unknown> = {
    '@/utils/request': request,
    '@/api/initialization': { checkOllamaStatus: async () => ({ available: true }), listModelProviders: async () => [], listOllamaModels: async () => [] },
    '@/api/model': {},
    'tdesign-vue-next': { MessagePlugin: { warning() {}, error() {} }, DialogPlugin: {} },
    '@/stores/ui': { useUIStore: () => ({}) },
    '@/utils/contextWindow': { DEFAULT_MODEL_CONTEXT_WINDOW },
    '@/utils/thinkingControl': { defaultThinkingControl, resolveThinkingControl },
    '@/components/modelEditorSourceState': { shouldShowOllamaUnavailableTip },
    '@/components/settings/SettingDrawer.vue': { __esModule: true, default: wrapper },
    '@/components/credentials/CredentialResource.vue': { __esModule: true, default: wrapper },
  }
  const module = { exports: {} as any }
  const api = ts.transpileModule(readFileSync(new URL('../api/chatgpt.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', api)((name: string) => modules[name] ?? require(name), module, module.exports)
  modules['@/api/chatgpt'] = module.exports
  const view = loadComponent(new URL('./ModelEditorDialog.vue', import.meta.url), modules)
  const visible = ref(false)
  const modelData = edit ? { id: 'saved-model', source: 'chatgpt', modelName: originalModel.slug, displayName: originalModel.display_name, chatgptAccountID: 'account-b', customHeaders: [] } : null
  const app = renderer.createApp(defineComponent({ setup: () => () => h(view, { visible: visible.value, modelType: 'chat', modelData }) }))
  app.use(createI18n({ legacy: false, locale, messages }))
  registerPrimitives(app)
  const passthrough = defineComponent({ setup: (_, { slots }) => () => h('div', slots.default?.()) })
  for (const name of ['t-form', 't-form-item', 't-input-number', 't-switch', 't-radio-group', 't-radio', 't-slider']) app.component(name, passthrough)
  delete app._context.components['t-select']
  delete app._context.components['t-option']
  app.component('t-select', defineComponent({ props: ['modelValue', 'loading'], setup: (props, { attrs, slots }) => () => h('select', { ...attrs, value: props.modelValue, 'data-loading': props.loading }, slots.default?.()) }))
  app.component('t-option', defineComponent({ props: ['value', 'label'], setup: props => () => h('option', { value: props.value }, props.label) }))
  const root = element('root'), errors: unknown[] = [], warnings: string[] = []
  app.config.errorHandler = error => errors.push(error)
  app.config.warnHandler = warning => warnings.push(warning)
  app.mount(root)
  visible.value = true
  await settle()
  if (!edit) await click(descendants(root).find(node => node.props.role === 'radio' && visibleText(node).includes(messages[locale].chatgpt.source))!)
  await settle()
  return { app, root, calls, errors, warnings,
    refresh: () => descendants(root).find(node => node.tag === 'button' && (visibleText(node).includes(messages[locale].chatgpt.refreshModels) || visibleText(node).includes(messages[locale].common.loading)))!,
    delayNext: () => { deferCatalog = true },
    finish: async () => { catalog = [originalModel, addedModel]; deferCatalog = false; assert.ok(pending); pending.resolve({ success: true, data: structuredClone(catalog) }); await settle() },
    fail: async () => { deferCatalog = false; assert.ok(pending); pending.reject(new Error('fixture catalog refresh failed')); await settle() },
  }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const edit of [false, true]) {
    test(`${locale}: ${edit ? 'editing' : 'adding'} a ChatGPT model refreshes the current account catalog through the real API`, async () => {
      const view = await mountEditor(locale, edit)
      try {
        assert.ok(visibleText(view.root).includes(originalModel.display_name))
        assert.ok(!visibleText(view.root).includes(addedModel.display_name))
        const count = view.calls.length
        view.delayNext()
        await click(view.refresh())
        assert.equal(view.calls.length, count + 1)
        assert.equal(view.calls.at(-1), `/api/v1/chatgpt/accounts/${edit ? 'account-b' : 'account-a'}/models`)
        assert.equal(view.refresh().props.disabled, true)
        assert.equal(view.refresh().props['data-loading'], true)
        assert.ok(visibleText(view.refresh()).includes(messages[locale].common.loading))
        await view.finish()
        assert.equal(view.refresh().props.disabled, false)
        assert.equal(view.refresh().props['data-loading'], false)
        assert.ok(descendants(view.root).some(node => node.tag === 'option' && node.props.value === addedModel.slug && visibleText(node) === addedModel.display_name))
        if (edit) assert.equal(descendants(view.root).find(node => node.props.id === 'chatgpt-model-slug')!.props.value, originalModel.slug)
        assert.deepEqual(view.errors, [])
        assert.deepEqual(view.warnings, [])
      } finally { view.app.unmount() }
    })
  }
  test(`${locale}: catalog refresh failure is visible and the action can retry`, async () => {
    const view = await mountEditor(locale, true)
    try {
      view.delayNext()
      await click(view.refresh())
      await view.fail()
      assert.ok(descendants(view.root).some(node => node.props.role === 'alert' && visibleText(node).includes('fixture catalog refresh failed')))
      assert.equal(view.refresh().props.disabled, false)
      view.delayNext()
      await click(view.refresh())
      assert.ok(!visibleText(view.root).includes('fixture catalog refresh failed'))
      await view.finish()
      assert.ok(visibleText(view.root).includes(addedModel.display_name))
      assert.deepEqual(view.errors, [])
      assert.deepEqual(view.warnings, [])
    } finally { view.app.unmount() }
  })
}

test('catalog refresh is disabled until a ChatGPT account is selected', async () => {
  const view = await mountEditor('en-US', false, true)
  try {
    assert.equal(view.refresh().props.disabled, true)
    await click(view.refresh())
    assert.deepEqual(view.calls, ['/api/v1/chatgpt/accounts'])
  } finally { view.app.unmount() }
})
