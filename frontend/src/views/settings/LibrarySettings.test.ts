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
import enUS from '../../i18n/locales/en-US'
import zhCN from '../../i18n/locales/zh-CN'
import { changedSettings } from '../../utils/settingsPatch'
import { copyModelTiers } from '../../utils/modelTiers'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('./LibrarySettings.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename: 'LibrarySettings.vue' })
const script = compileScript(descriptor, { id: 'library-settings-test' })
const template = compileTemplate({ id: 'library-settings-test', filename: 'LibrarySettings.vue', source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
assert.deepEqual(template.errors, [])

async function scenario(locale: 'zh-CN' | 'en-US', action: (state: any, requests: any[]) => Promise<void>, failSave = false, emptyLibrary = false) {
  const requests: any[] = [], events: string[] = []
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const originalWindow = globalThis.window
  globalThis.window = { addEventListener() {}, removeEventListener() {}, dispatchEvent(event: Event) { events.push(event.type); return true } } as any
  const stub = defineComponent({ setup(_, { slots }) { return () => h('div', slots.default?.()) } })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
    '@/components/ui': settingsTestUi(),
    '@/utils/settingsPatch': { changedSettings },
    '@/utils/modelTiers': { copyModelTiers },
    '@/stores/ui': { useUIStore: () => ({ settingsInitialSubSection: null }) },
    '@/stores/resources': { useResourcesStore: () => ({ allModels: [], ensureModels: async () => {}, invalidateLibraryDetail() { requests.push({ cacheInvalidated: true }) } }) },
    '@/api/library': {
      getLibrary: async () => { requests.push({ method: 'get' }); return { success: true, data: emptyLibrary ? null : { id: 'unique-library', name: 'Library', model_tiers: { generation: '', embedding: 'embedding-a' }, chunking_config: { chunk_size: 512, chunk_overlap: 0, separators: ['\n\n'], enable_parent_child: false, languages: null, parser_engine_rules: null }, indexing_strategy: { vector_enabled: true, keyword_enabled: true } } } },
      listEntryFiles: async (id: string) => { requests.push({ articleListId: id }); return { data: [], total: 0 } },
      updateLibrary: async (body: unknown) => { requests.push({ method: 'put', body }); return failSave ? { success: false, message: 'Save unavailable' } : { success: true } },
    },
    '@/api/embedding-rebuild': {
      getEmbeddingRebuild: async () => { requests.push({ method: 'embedding-status' }); return { success: true, data: { latest_run: { id: 'run-1', model_id: 'embedding-new', model_name: 'New image model', dimension: 512, status: 'paused', total_count: 7, completed_count: 3, failed_count: 1 }, failed_items: [], current_model_matches: true } } },
    },
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', compiled)((name: string) => modules[name] ?? (name === '@/styles/settings-form.css' ? {} : name.endsWith('.vue') ? { __esModule: true, default: stub } : require(name)), module, module.exports)
    return module.exports
  }
  const { descriptor: rebuildDescriptor } = parse(readFileSync(new URL('../../components/EmbeddingRebuild.vue', import.meta.url), 'utf8'), { filename: 'EmbeddingRebuild.vue' })
  const rebuildScript = compileScript(rebuildDescriptor, { id: 'settings-rebuild' })
  const rebuildTemplate = compileTemplate({ id: 'settings-rebuild', filename: 'EmbeddingRebuild.vue', source: rebuildDescriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: rebuildScript.bindings } })
  assert.deepEqual(rebuildTemplate.errors, [])
  const rebuildComponent = evaluate(rebuildScript.content).default
  rebuildComponent.ssrRender = evaluate(rebuildTemplate.code).ssrRender
  const rebuildSetup = rebuildComponent.setup
  rebuildComponent.setup = (props: object, context: object) => { const state = rebuildSetup(props, context); onServerPrefetch(() => state.load()); return state }
  modules['@/components/EmbeddingRebuild.vue'] = { __esModule: true, default: rebuildComponent }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  component.setup = (props: object, context: object) => { const state = setup(props, context); onServerPrefetch(async () => { await state.load(); await action(state, requests) }); return state }
  const app = createSSRApp(component)
  app.use(i18n)
  for (const name of ['t-alert', 't-button', 't-popconfirm', 't-loading', 't-tabs', 't-tab-panel', 't-input', 't-checkbox', 't-switch', 't-select', 't-option']) app.component(name, stub)
  const errors: unknown[] = []
  app.config.errorHandler = error => errors.push(error)
  try { const html = await renderToString(app); assert.deepEqual(errors, []); return { html, requests, events } }
  finally { globalThis.window = originalWindow }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: entry settings save a replacement embedding model and load the real rebuild entry and progress`, async () => {
    const { html, requests } = await scenario(locale, async state => {
      state.currentSection.value = 'models'
      state.hasFiles.value = true
      state.formData.value.modelTiers.embedding = 'embedding-new'
      await state.handleSubmit()
      assert.equal(state.dirty.value, false)
    })
    const copy = (locale === 'zh-CN' ? zhCN : enUS).embeddingRebuild
    assert.ok(html.includes(copy.title))
    assert.ok(html.includes(copy.status.paused))
    assert.ok(html.includes(copy.resume))
    assert.ok(html.includes('New image model'))
    assert.match(html, /<progress[^>]*value="4"[^>]*max="7"/)
    assert.equal(requests.find(item => item.method === 'put').body.config.model_tiers.embedding, 'embedding-new')
    assert.deepEqual(Object.keys(requests.find(item=>item.method==='put').body.config),['model_tiers'])
    assert.equal(requests.filter(item => item.method === 'embedding-status').length, 1)
    assert.doesNotMatch(source, /:embedding-locked=/)
  })

  test(`${locale}: a failed settings save preserves the edited values and shows an inline error`, async () => {
    const { html, events } = await scenario(locale, async state => {
      state.currentSection.value = 'models'
      state.formData.value.modelTiers.embedding = 'embedding-edited'
      await state.handleSubmit()
      assert.equal(state.formData.value.modelTiers.embedding, 'embedding-edited')
      assert.equal(state.dirty.value, true)
      assert.equal(state.error.value, 'Save unavailable')
    }, true)
    assert.match(html, /Save unavailable/)
    assert.deepEqual(events, [])
  })
}

test('unchanged knowledge settings do not rewrite loaded null defaults', async () => {
 const {requests}=await scenario('zh-CN',async state=>{await state.handleSubmit()})
 assert.equal(requests.filter(x=>x.method==='put').length,0)
})

test('zero libraries leave settings available without a load error or entry-list request', async () => {
  const { requests } = await scenario('zh-CN', async state => {
    assert.equal(state.formData.value, null)
    assert.equal(state.error.value, '')
    assert.equal(state.dirty.value, false)
    await state.handleSubmit()
  }, false, true)
  assert.equal(requests.some(item => item.articleListId || item.method === 'put'), false)
})
