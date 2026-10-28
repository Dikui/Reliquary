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
import * as usage from '../../api/model-usage'
import * as contextWindow from '../../utils/contextWindow'
import { settingsTestUi } from './settingsTestUi'

const require = createRequire(import.meta.url)
const details: usage.ModelUsageDetails = {
  libraries: [{ id: 'library-ref', name: 'Referenced library', bindings: ['generation_model_tier'] }],
  library_total: 1,
}
const response = { status: 400, success: false, error: { code: usage.MODEL_IN_USE_ERROR_CODE, message: 'model is in use', details } }
const model = { id: 'model-ref', name: 'referenced', display_name: 'Referenced model', type: 'KnowledgeQA', source: 'remote', parameters: {} }
const stub = defineComponent({ setup: (_, { slots }) => () => h('div', slots.default?.()) })

async function deletionScenario(locale: 'zh-CN' | 'en-US', failure: unknown, resolves = false) {
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
  const errors: unknown[] = [], toasts: string[] = [], requests: string[] = []
  const modules: Record<string, unknown> = {
    '../utils/request': {
      get: async () => ({ success: true, data: [model] }),
      del: async (path: string) => { requests.push(path); if (resolves) return failure; throw failure },
    },
    './model-usage': usage,
    '@/i18n': { __esModule: true, default: i18n },
    '@/components/ui': settingsTestUi({ toast: { error: (message: string) => toasts.push(message), success() {} } }),
    '@/styles/settings-form.css': {},
    'vue-router': { useRouter: () => ({}) },
    '@/stores/resources': { useResourcesStore: () => ({ replaceModels() {} }) },
    '@/stores/ui': { useUIStore: () => ({}) },
    '@/utils/contextWindow': contextWindow,
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', compiled)((name: string) => name.endsWith('.vue') ? { __esModule: true, default: stub } : modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  modules['@/api/model'] = evaluate(readFileSync(new URL('../../api/model.ts', import.meta.url), 'utf8'))
  const { descriptor } = parse(readFileSync(new URL('./ModelSettings.vue', import.meta.url), 'utf8'))
  const script = compileScript(descriptor, { id: 'model-deletion' })
  const template = compileTemplate({ id: 'model-deletion', filename: 'ModelSettings.vue', source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  let state: any
  const setup = component.setup
  component.setup = (props: object, context: object) => {
    state = setup(props, context)
    onServerPrefetch(async () => { await state.loadModels(); await state.deleteModel('chat', model.id) })
    return state
  }
  const app = createSSRApp(component).use(i18n)
  app.config.errorHandler = error => errors.push(error)
  const html = await renderToString(app)
  assert.deepEqual(errors, [])
  return { html, state, toasts, requests }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const kind of ['http-rejection', 'typed-conflict', 'unsuccessful-response'] as const) {
    test(`${locale}: ${kind} keeps the referenced model and shows usage without console errors`, async context => {
      const logged = context.mock.method(console, 'error', () => {})
      const result = await deletionScenario(locale, kind === 'typed-conflict' ? new usage.ModelInUseError(details) : response, kind === 'unsuccessful-response')
      assert.deepEqual(result.requests, ['/api/v1/models/model-ref'])
      assert.equal(result.state.allModels.value.length, 1)
      assert.equal(result.state.showUsageDialog.value, true)
      assert.deepEqual(result.state.usageConflict.value, details)
      assert.ok(result.html.includes('Referenced library'))
      assert.ok(result.html.includes('Referenced model'))
      assert.ok(result.html.includes((locale === 'zh-CN' ? zh : en).modelSettings.usage.title))
      assert.deepEqual(result.toasts, [])
      assert.equal(logged.mock.callCount(), 0, 'neither the model API nor the page logs an expected usage conflict')
    })
  }
}

test('unexpected model deletion failure still reports diagnostics and user feedback', async context => {
  const logged = context.mock.method(console, 'error', () => {})
  const result = await deletionScenario('en-US', new Error('Deletion service unavailable'))
  assert.equal(result.state.showUsageDialog.value, false)
  assert.deepEqual(result.toasts, ['Deletion service unavailable'])
  assert.ok(logged.mock.callCount() > 0)
})
