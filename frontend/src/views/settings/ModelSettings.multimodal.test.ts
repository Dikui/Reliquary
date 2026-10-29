import { settingsTestUi } from './settingsTestUi'
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
  test(`${locale}: the actual model settings load the image capability and persist it through the model API`, async () => {
    const calls: { path: string; payload?: any }[] = []
    let model = { id: 'image-model', name: 'local-siglip', display_name: 'Image model', type: 'Embedding', source: 'remote', parameters: { pricing: { input_per_million: 2, image_input_per_million: 8, output_per_million: null, currency: 'USD' }, base_url: 'http://localhost:18092/v1', provider: 'generic', embedding_parameters: { dimension: 512, supports_dimension_override: false, supports_images: true, multimodal_format: 'jina' } } }
    const i18n = createI18n({ legacy: false, locale, messages })
    const request = {
      get: async (path: string) => { calls.push({ path }); return { success: true, data: [structuredClone(model)] } },
      put: async (path: string, payload: any) => { calls.push({ path, payload }); model = { ...model, ...payload }; return { success: true, data: model } },
    }
    const modules: Record<string, unknown> = {
      '../utils/request': request,
      '@/i18n': { __esModule: true, default: i18n },
      './model-usage': { ModelInUseError: class extends Error {} },
    '@/components/ui': settingsTestUi(),
      'vue-router': { useRouter: () => ({ push: async () => {} }) },
      '@/stores/resources': { useResourcesStore: () => ({ replaceModels() {} }) },
      '@/stores/ui': { useUIStore: () => ({ settingsInitialSubSection: 'chat' }) },
      '@/utils/contextWindow': contextWindow,
    }
    function evaluate(code: string) {
      const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
      const module = { exports: {} as any }
      new Function('require', 'module', 'exports', output)((name: string) => name.endsWith('.vue') ? { __esModule: true, default: stub } : modules[name] ?? (name === '@/styles/settings-form.css' ? {} : require(name)), module, module.exports)
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
        assert.equal(state.activeTypeFilter.value, 'embedding')
        await state.loadModels()
        const loaded = state.allLegacyModels.value[0]
        assert.deepEqual(loaded.pricing, { input_per_million: 2, image_input_per_million: 8, output_per_million: null, currency: 'USD' })
        assert.equal(loaded.supportsImages, true)
        assert.equal(loaded.multimodalFormat, 'jina')
        state.editModel('embedding', loaded)
        await state.handleModelSave({ ...state.editingModel.value, displayName: 'Updated image model', pricing: { input_per_million: 3, image_input_per_million: 9, output_per_million: null, currency: 'CNY' } })
        assert.equal(state.allLegacyModels.value[0].supportsImages, true)
        assert.deepEqual(state.allLegacyModels.value[0].pricing, { input_per_million: 3, image_input_per_million: 9, output_per_million: null, currency: 'CNY' })
        await state.handleModelSave({ ...state.allLegacyModels.value[0], pricing: null })
        assert.equal(state.allLegacyModels.value[0].pricing, null)
      })
      return state
    }
    const app = createSSRApp(component, { initialSection: 'embedding' }); app.use(i18n)
    for (const name of new Set(descriptor.template!.content.match(/t-[a-z-]+(?=[\s>])/g))) app.component(name, stub)
    const failures: unknown[] = [], warnings: string[] = []
    app.config.errorHandler = error => failures.push(error); app.config.warnHandler = warning => warnings.push(warning)
    const html = await renderToString(app)
    assert.deepEqual(failures, []); assert.deepEqual(warnings, [])
    const save = calls.find(call => call.payload)!
    assert.deepEqual(save.payload.parameters.pricing, { input_per_million: 3, image_input_per_million: 9, output_per_million: null, currency: 'CNY' })
    assert.equal(calls.filter(call => call.payload).at(-1)!.payload.parameters.pricing, null)
    assert.equal(save.path, '/api/v1/models/image-model')
    assert.equal(save.payload.parameters.embedding_parameters.supports_images, true)
    assert.equal(save.payload.parameters.embedding_parameters.multimodal_format, 'jina')
    assert.equal(save.payload.parameters.embedding_parameters.dimension, 512)
    assert.ok(calls.filter(call => call.path === '/api/v1/models').length >= 2)
    assert.ok(html.includes('Updated image model'))
    assert.ok(html.includes(messages[locale].model.editor.supportsImagesLabel))
  })
}
