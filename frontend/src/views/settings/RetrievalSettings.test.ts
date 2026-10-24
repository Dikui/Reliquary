import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zh, 'en-US': en }
async function scenario(locale: keyof typeof messages, stored: object, action: (state: any, requests: any[]) => Promise<void>) {
  const requests: any[] = []
  const { descriptor } = parse(readFileSync(new URL('./RetrievalSettings.vue', import.meta.url), 'utf8'), { filename: 'RetrievalSettings.vue' })
  const script = compileScript(descriptor, { id: 'retrieval-test' })
  const template = compileTemplate({ id: 'retrieval-test', filename: 'RetrievalSettings.vue', source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const modules: Record<string, unknown> = {
    '@/api/retrieval': { getSpaceRetrievalConfig: async () => ({ data: stored }), updateSpaceRetrievalConfig: async (payload: object) => { requests.push(payload); return { data: payload } } },
    'tdesign-vue-next': { MessagePlugin: { success() {} } },
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  component.setup = (props: object, context: object) => {
    const state = setup(props, context)
    onServerPrefetch(async () => { await state.loadConfig(); await action(state, requests) })
    return state
  }
  const app = createSSRApp(component)
  app.use(createI18n({ legacy: false, locale, messages }))
  const stub = defineComponent({ setup: (_props, { attrs }) => () => h('div', attrs) })
  app.component('t-slider', stub); app.component('t-input-number', stub)
  const warnings: string[] = [], errors: unknown[] = []
  app.config.warnHandler = warning => warnings.push(warning); app.config.errorHandler = error => errors.push(error)
  const html = await renderToString(app)
  assert.deepEqual(warnings, []); assert.deepEqual(errors, [])
  return { html, requests }
}
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: retrieval settings preserve similarity and fusion parameters when editing an existing setting`, async () => {
    const stored = { embedding_top_k: 50, vector_threshold: 0.15, keyword_threshold: 0.3, rerank_top_k: 10, rerank_threshold: 0.2,   rrf_k: 80, rrf_vector_weight: 0.6, rrf_keyword_weight: 0.4, rrf_image_weight: 0.15 }
    const result = await scenario(locale, stored, async (state, requests) => {

      assert.equal(state.localConfig.rrf_image_weight, 0.15)
      state.localConfig.embedding_top_k = 60
      await state.saveConfig()
      assert.deepEqual(requests, [{ ...stored, embedding_top_k: 60 }])
    })
    assert.ok(result.html.includes(messages[locale].retrievalSettings.imageWeightLabel))
  })
  for (const enabled of [true, false]) test(`${locale}: editing image weight returns every loaded field including image vector ${enabled}`, async () => {
    const stored = { image_vector_enabled: enabled, embedding_top_k: 50, vector_threshold: 0.15, keyword_threshold: 0.3, rerank_top_k: 10, rerank_threshold: 0.2,   rrf_k: 80, rrf_vector_weight: 0.6, rrf_keyword_weight: 0.4, rrf_image_weight: 0.25, future_option: { retained: true } }
    await scenario(locale, stored, async (state, requests) => {
      state.localConfig.rrf_image_weight = 0.3
      await state.saveConfig()
      assert.deepEqual(requests, [{ ...stored, rrf_image_weight: 0.3 }])
    })
  })
  test(`${locale}: similarity settings save changes and preserve zero as the server default image weight`, async () => {
    await scenario(locale, {}, async (state, requests) => {

      assert.equal(state.localConfig.rrf_image_weight, 0.2)
      state.localConfig.rrf_image_weight = 0

      await state.saveConfig()

      assert.equal(requests[0].rrf_image_weight, 0)
    })
  })
  test(`${locale}: the image weight hint explains that zero uses the server default`, async () => {
    const result = await scenario(locale, { rrf_image_weight: 0 }, async state => {
      assert.equal(state.localConfig.rrf_image_weight, 0)
    })
    assert.ok(result.html.includes(messages[locale].retrievalSettings.imageWeightHint))
    assert.match(messages[locale].retrievalSettings.imageWeightHint, locale === 'zh-CN' ? /0 时使用默认权重 0\.2/ : /0 uses the default weight of 0\.2/)
  })
}
