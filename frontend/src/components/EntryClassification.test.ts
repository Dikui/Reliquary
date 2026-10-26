import assert from 'node:assert/strict'
import * as entrySourceLabels from '../utils/entrySourceLabels'
import * as markdownPlainText from '../utils/markdownPlainText'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zhCN from '../i18n/locales/zh-CN'
import enUS from '../i18n/locales/en-US'

import { safeTextareaTestModule } from './__tests__/safeTextareaStub'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const emptyModule = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => name === '@/components/SelectionCheckbox.vue'
    ? { __esModule: true, default: component('./SelectionCheckbox.vue', {}) }
    : name === '@/utils/entrySourceLabels' ? entrySourceLabels : name.endsWith('.css') ? {} : name.endsWith('.png') ? '/image.png' : modules[name] ?? require(name), module, module.exports)
  return module.exports
}
function component(path: string, modules: Record<string, unknown>) {
  const filename = path.split('/').at(-1)!
  const { descriptor, errors } = parse(readFileSync(new URL(path, import.meta.url), 'utf8'), { filename })
  assert.deepEqual(errors, [])
  const script = compileScript(descriptor, { id: filename })
  const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const result = evaluate(script.content, modules).default
  result.ssrRender = evaluate(template.code, modules).ssrRender
  return result
}
const classification = component('./EntryClassification.vue', {
  './FollowToggle.vue': emptyModule,
  '@/utils/markdownPlainText': markdownPlainText,
  '@/types/entryClassification': { }, '@/utils/security': { isValidURL: (value: string) => /^https?:\/\//.test(value) },
})
const reviewModule = { __esModule: true, default: component('./ReviewActions.vue', { '@/types/entryClassification': { }, '@/api/entry-review': {}, '@/utils/security': { isValidURL: (value: string) => /^https?:\/\//.test(value) }, 'tdesign-vue-next': { MessagePlugin: {} } }) }
const classificationModule = { __esModule: true, default: classification }
async function render(value: any, props: Record<string, unknown>, locale: keyof typeof messages) {
  const app = createSSRApp(value, props)
  app.use(createI18n({ legacy: false, locale, messages }))
  const stub = defineComponent({ props: ['visible'], setup(props, { attrs, slots }) { return () => props.visible === false ? null : h('div', attrs, [slots.header?.(), slots.default?.()]) } })
  for (const name of ['t-drawer', 't-button', 't-icon', 't-tag', 't-tooltip', 't-loading', 't-select', 't-input', 't-textarea', 't-checkbox', 't-popconfirm', 't-popup', 't-empty', 't-switch', 't-pagination', 't-radio', 't-dialog']) app.component(name, stub)
  const errors: unknown[] = []; const warnings: string[] = []
  app.config.errorHandler = error => errors.push(error); app.config.warnHandler = warning => warnings.push(warning)
  const html = await renderToString(app)
  assert.deepEqual(errors, []); assert.deepEqual(warnings, [])
  return html
}
const article = {
  id: 'article', file_name: 'Example.md', type: 'manual', parse_status: 'completed',  processing_mode: 'full',
  structured_summary: { overview: 'A product was announced.', points: ['Launch date', 'Main feature', 'Availability'], key_terms: ['Example Company', '42'] },
  entities: [{ id: 'entity-1', canonical_name: 'Example Company', }],  md: [], total: 0, metadata: {},
}
const listModules = {
  '@/components/EntryProcessingStatus.vue': emptyModule,
  '@/components/EntryUpdateNotice.vue': emptyModule,
  '@/utils/markdownPlainText': markdownPlainText,
  '@/utils/files': { formatFileSize: () => '100 B', getFileIcon: () => 'file' }, './DocumentActionMenu.vue': emptyModule, './DocumentMenuDelete.vue': emptyModule,
  '@/components/PersonalDocumentDelete.vue': emptyModule, '@/components/EntryProcessingTimeline.vue': emptyModule, '@/components/EntryDomainsEditor.vue': { __esModule: true, default: { setup: () => () => null } }, '@/components/EntryEntitiesEditor.vue': { __esModule: true, default: { setup: () => () => null } }, '@/components/EntryClassification.vue': classificationModule, '@/components/EntryStarToggle.vue': emptyModule,
}
const props = { selectedIds: new Set(), batchMode: false, traceAvailableById: {}, traceVisibleIds: {} }

function articleLoadingHook(record: Record<string, unknown>) {
  return evaluate(readFileSync(new URL('../composables/useLibrary.ts', import.meta.url), 'utf8'), {
    pinia: { storeToRefs: () => ({ cardList: ref([]), total: ref(0) }) },
    '../utils/index': { formatStringDate: () => '2026-10-12' },
    'tdesign-vue-next': { MessagePlugin: {} },
    '@/api/library': { listEntryFiles: async () => ({ data: [record], total: 1 }) },
    '@/stores/entry': { entryStore: () => ({}) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    '@/stores/resources': { useResourcesStore: () => ({}) },
  }).default('library-articles')
}
for (const locale of ['zh-CN', 'en-US'] as const) {
  const copy = messages[locale].library
  test(`${locale}: cards and rows show and hide ingest markers through the production list loading hook`, async () => {
    for (const parse_status of ['completed', 'processing', 'failed']) for (const flagged of [true, false, undefined]) {
      const hook = articleLoadingHook({ ...article, parse_status, acquisition: flagged ? 'model_written' : 'verbatim', ...(flagged !== undefined ? { incomplete: flagged } : {}), updated_at: '2026-10-12T00:00:00Z' })
      await hook.loadEntries()
      for (const path of ['../views/library/components/DocumentCardView.vue', '../views/library/components/DocumentListView.vue']) {
        const html = await render(component(path, listModules), { ...props, items: hook.cardList.value }, locale)
        assert.equal(html.includes(messages[locale].ingestStatus.modelWritten), Boolean(flagged), path)
        assert.equal(html.includes(messages[locale].ingestStatus.incomplete), false, path)
        for (const hint of [messages[locale].ingestStatus.modelWrittenHint]) {
          assert.equal(html.includes(`title="${hint.replaceAll("'", "&#39;")}"`), Boolean(flagged), `${path}: ${hint}`)
        }
      }
    }
  })



}


for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: historical RSS and web article channels keep their localized labels`, async () => {
    const card = component('../views/library/components/DocumentCardView.vue', listModules)
    const setup = card.setup
    card.setup = (props: object, context: object) => {
      const state = setup(props, context)
      onServerPrefetch(() => {
        assert.equal(state.getEntryType({ ...article, channel: 'rss' }), 'Markdown')
        assert.equal(state.getChannelLabel({ channel: 'rss' }), messages[locale].library.channelRSS)
        assert.equal(state.getChannelLabel({ channel: 'web' }), messages[locale].library.channelWeb)
      })
      return state
    }
    await render(card, { ...props, items: [{ ...article, channel: 'rss' }, { ...article, channel: 'web' }] }, locale)
    const row = component('../views/library/components/DocumentListView.vue', listModules)
    const rowSetup = row.setup
    row.setup = (props: object, context: object) => {
      const state = rowSetup(props, context)
      onServerPrefetch(() => {
        assert.equal(state.getSourceInfo({ ...article, channel: 'rss' }).label, messages[locale].library.channelRSS)
        assert.equal(state.getSourceInfo({ ...article, type: 'url', channel: 'web' }).label, messages[locale].library.channelWeb)
      })
      return state
    }
    await render(row, { ...props, items: [{ ...article, channel: 'rss' }, { ...article, type: 'url', channel: 'web' }] }, locale)
  })
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: AI reports display their trust marker in classification, cards and rows`, async () => {
    const report = { ...article, source_kind: 'report', cannot_be_fact: true,  structured_summary: null, entities: [], }
    const html = await render(classification, { entry: report }, locale)
    assert.ok(html.includes(messages[locale].ingestStatus.cannotBeFact))
    const ordinary = await render(classification, { entry: article }, locale)
    assert.ok(!ordinary.includes(messages[locale].ingestStatus.cannotBeFact))
    for (const path of ['../views/library/components/DocumentCardView.vue', '../views/library/components/DocumentListView.vue']) {
      const html = await render(component(path, listModules), { ...props, items: [report] }, locale)
      assert.ok(html.includes(messages[locale].ingestStatus.cannotBeFact), path)
    }
  })

  test(`${locale}: report association relations can move to the dedicated detail section`, async () => {
    const html = await render(classification, { entry: article, hideRelations: true }, locale)
    assert.ok(!html.includes(messages[locale].library.articleEntities))
  })
}
