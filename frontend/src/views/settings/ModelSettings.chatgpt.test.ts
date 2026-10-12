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
import * as contextWindow from '../../utils/contextWindow'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zh, 'en-US': en }
const stub = defineComponent({ setup: (_, { attrs, slots }) => () => h('div', attrs, slots.default?.()) })

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: the actual model settings load ChatGPT account metadata and save only the selected registration and slug`, async () => {
    const calls: { path: string; payload?: any }[] = []
    let model = { id: 'chatgpt-model', name: 'account-slug', display_name: 'Account model', type: 'KnowledgeQA', source: 'chatgpt', parameters: { supports_vision: true, extra_config: { chatgpt_account_id: 'selected-account', chatgpt_display_name: 'Account model' } } }
    const i18n = createI18n({ legacy: false, locale, messages })
    const request = {
      get: async (path: string) => { calls.push({ path }); return { success: true, data: [structuredClone(model)] } },
      put: async (path: string, payload: any) => { calls.push({ path, payload }); model = { ...model, ...payload }; return { success: true, data: model } },
    }
    const modules: Record<string, unknown> = {
      '../utils/request': request,
      '@/i18n': { __esModule: true, default: i18n },
      './model-usage': { ModelInUseError: class extends Error {} },
      'tdesign-vue-next': { MessagePlugin: { success() {}, error() {}, warning() {} } },
      'vue-router': { useRouter: () => ({ push: async () => {} }) },
      '@/stores/resources': { useResourcesStore: () => ({ replaceModels() {} }) },
      '@/stores/ui': { useUIStore: () => ({ settingsInitialSubSection: 'chat' }) },
      '@/utils/contextWindow': contextWindow,
    }
    function evaluate(code: string) {
      const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
      const module = { exports: {} as any }
      new Function('require', 'module', 'exports', output)((name: string) => name.endsWith('.vue') ? { __esModule: true, default: stub } : modules[name] ?? require(name), module, module.exports)
      return module.exports
    }
    modules['@/api/model'] = evaluate(readFileSync(new URL('../../api/model.ts', import.meta.url), 'utf8'))
    const { descriptor } = parse(readFileSync(new URL('./ModelSettings.vue', import.meta.url), 'utf8'), { filename: 'ModelSettings.vue' })
    const script = compileScript(descriptor, { id: 'image-model-settings' })
    const template = compileTemplate({ id: 'image-model-settings', filename: 'ModelSettings.vue', source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
    assert.deepEqual(template.errors, [])
    const component = evaluate(script.content).default
    component.ssrRender = evaluate(template.code).ssrRender
    const setup = component.setup
    component.setup = (props: object, context: object) => {
      const state = setup(props, context)
      onServerPrefetch(async () => {
        await state.loadModels()
        const loaded = state.allLegacyModels.value[0]
        assert.equal(loaded.chatgptAccountID, 'selected-account')
        assert.equal(loaded.modelName, 'account-slug')
        state.editModel('chat', loaded)
        await state.handleModelSave({ ...state.editingModel.value, displayName: 'Updated account model' })
        assert.equal(state.allLegacyModels.value[0].chatgptAccountID, 'selected-account')
      })
      return state
    }
    const app = createSSRApp(component); app.use(i18n)
    for (const name of new Set(descriptor.template!.content.match(/t-[a-z-]+(?=[\s>])/g))) app.component(name, stub)
    const failures: unknown[] = [], warnings: string[] = []
    app.config.errorHandler = error => failures.push(error); app.config.warnHandler = warning => warnings.push(warning)
    const html = await renderToString(app)
    assert.deepEqual(failures, []); assert.deepEqual(warnings, [])
    const save = calls.find(call => call.payload)!
    assert.equal(save.path, '/api/v1/models/chatgpt-model')
    assert.equal(save.payload.name, 'account-slug')
    assert.equal(save.payload.source, 'chatgpt')
    assert.equal(save.payload.parameters.extra_config.chatgpt_account_id, 'selected-account')
    assert.equal(save.payload.parameters.extra_config.chatgpt_display_name, 'Updated account model')
    for (const field of ['base_url', 'api_key', 'custom_headers', 'provider', 'temperature', 'max_output_tokens']) assert.equal(field in save.payload.parameters, false)
    assert.ok(calls.filter(call => call.path === '/api/v1/models').length >= 2)
    assert.ok(html.includes('Updated account model'))
    assert.ok(html.includes(messages[locale].chatgpt.usingPlan))
    const usageLinks = html.match(/<a\b[^>]*class="model-card__usage"[^>]*>[^<]*<\/a>/g) || []
    assert.equal(usageLinks.length, 1)
    assert.match(usageLinks[0], /href="https:\/\/chatgpt\.com\/settings\/usage"/)
    assert.match(usageLinks[0], /target="_blank"/)
    assert.match(usageLinks[0], /rel="noopener noreferrer"/)
    assert.ok(usageLinks[0].includes(messages[locale].chatgpt.manageUsage))
  })
}
