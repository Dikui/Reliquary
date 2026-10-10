import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { createSSRApp, defineComponent, h, onServerPrefetch } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import enUS from '../../i18n/locales/en-US'
import zhCN from '../../i18n/locales/zh-CN'
import { copyModelTiers } from '../../utils/modelTiers'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('./KnowledgeSettings.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename: 'KnowledgeSettings.vue' })
const script = compileScript(descriptor, { id: 'knowledge-settings-test' })
const template = compileTemplate({ id: 'knowledge-settings-test', filename: 'KnowledgeSettings.vue', source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
assert.deepEqual(template.errors, [])

async function scenario(locale: 'zh-CN' | 'en-US', action: (state: any, requests: any[]) => Promise<void>, failSave = false) {
  const requests: any[] = [], events: string[] = []
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const originalWindow = globalThis.window
  globalThis.window = { addEventListener() {}, removeEventListener() {}, dispatchEvent(event: Event) { events.push(event.type); return true } } as any
  const stub = defineComponent({ setup(_, { slots }) { return () => h('div', slots.default?.()) } })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
    'tdesign-vue-next': { MessagePlugin: { success() {} }, DialogPlugin: {} },
    '@/utils/modelTiers': { copyModelTiers },
    '@/stores/ui': { useUIStore: () => ({ settingsInitialSubSection: null }) },
    '@/stores/resources': { useResourcesStore: () => ({ allModels: [], ensureModels: async () => {}, invalidateKnowledgeBaseDetail() { requests.push({ cacheInvalidated: true }) } }) },
    '@/api/knowledge-base': {
      getKnowledgeBase: async () => { requests.push({ method: 'get' }); return { success: true, data: { id: 'unique-kb', name: 'Knowledge Base', model_tiers: { generation: 'generation-old', embedding: 'embedding-a' }, chunking_config: { chunk_size: 512, chunk_overlap: 0, separators: ['\n\n'], enable_parent_child: false }, indexing_strategy: { vector_enabled: true, keyword_enabled: true }, vlm_config: { enabled: false } } } },
      listKnowledgeFiles: async (id: string) => { requests.push({ articleListId: id }); return { data: [], total: 0 } },
      updateKnowledgeBase: async (body: unknown) => { requests.push({ method: 'put', body }); return failSave ? { success: false, message: 'Save unavailable' } : { success: true } },
    },
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', compiled)((name: string) => name.endsWith('.vue') ? { __esModule: true, default: stub } : modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  component.setup = (props: object, context: object) => { const state = setup(props, context); onServerPrefetch(async () => { await state.load(); await action(state, requests) }); return state }
  const app = createSSRApp(component)
  app.use(i18n)
  for (const name of ['t-alert', 't-button', 't-loading', 't-tabs', 't-tab-panel', 't-input', 't-checkbox', 't-switch', 't-select', 't-option']) app.component(name, stub)
  const errors: unknown[] = []
  app.config.errorHandler = error => errors.push(error)
  try { const html = await renderToString(app); assert.deepEqual(errors, []); return { html, requests, events } }
  finally { globalThis.window = originalWindow }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: the settings page saves model tiers and chunking together on the unique knowledge base`, async () => {
    const { requests, events } = await scenario(locale, async state => {
      assert.equal(state.dirty.value, false)
      assert.equal(state.formData.value.chunkingConfig.chunkOverlap, 0)
      state.formData.value.modelTiers.generation = 'generation-new'
      state.formData.value.chunkingConfig.chunkSize = 128
      await state.handleSubmit()
      assert.equal(state.dirty.value, false)
    })
    assert.equal(requests.filter(item => item.method === 'get').length, 1)
    assert.equal(requests.find(item => item.articleListId)?.articleListId, 'unique-kb')
    const saves = requests.filter(item => item.method === 'put')
    assert.equal(saves.length, 1)
    assert.equal(saves[0].body.config.model_tiers.generation, 'generation-new')
    assert.equal(saves[0].body.config.chunking_config.chunk_size, 128)
    assert.equal(saves[0].body.config.chunking_config.chunk_overlap, 0)
    assert.equal(saves[0].body.knowledge_base_id, undefined)
    assert.deepEqual(events, ['reliquary:knowledge-settings-saved'])
  })
  test(`${locale}: a failed settings save preserves the edited values and shows an inline error`, async () => {
    const { html, events } = await scenario(locale, async state => {
      state.formData.value.chunkingConfig.chunkSize = 128
      await state.handleSubmit()
      assert.equal(state.formData.value.chunkingConfig.chunkSize, 128)
      assert.equal(state.dirty.value, true)
      assert.equal(state.error.value, 'Save unavailable')
    }, true)
    assert.match(html, /Save unavailable/)
    assert.deepEqual(events, [])
  })
}
