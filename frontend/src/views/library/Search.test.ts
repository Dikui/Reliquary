import * as bodyReference from '../../utils/bodyReference'
import * as libraryScopeHelpers from '../../utils/libraryScope'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, onServerPrefetch, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'

import type { SearchEntry, SearchRequest } from '../../types/search'
import * as helpers from '../../utils/search'
import * as highlight from '../../components/GlobalCommandPalette/useHighlight'
import * as protectedAccess from '../../utils/protectedFileAccess'
import * as markdownPlainText from '../../utils/markdownPlainText'
import * as paletteCommands from '../../components/GlobalCommandPalette/commands'
import * as visibleFileChunks from '../../components/GlobalCommandPalette/visibleFileChunks'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zh, 'en-US': en }
const moduleOf = (value: any) => ({ __esModule: true, default: value })
const stub = defineComponent({ setup: (_props, { attrs, slots }) => () => h('div', attrs, [slots.default?.(), slots.operation?.()]) })
function evaluate(code: string, modules: Record<string, unknown>) {
  const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', compiled)((name: string) => (name === '@/utils/bodyReference' ? bodyReference : name === '@/utils/libraryScope' ? libraryScopeHelpers : name === '@/composables/useLibraryScope' ? { useLibraryScope: () => ref([]) } : (name.endsWith('LibraryScopeSelect.vue') ? { __esModule: true, default: defineComponent({ setup: () => () => null }) } : modules[name])) ?? require(name), module, module.exports)
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
async function render(value: any, props: Record<string, unknown>, locale: keyof typeof messages, action?: (state: any) => Promise<void>, controls?: Record<string, any>) {
  if (action) {
    const setup = value.setup
    value.setup = (props: object, context: object) => {
      const state = setup(props, context)
      onServerPrefetch(() => action(state))
      return state
    }
  }
  const app = createSSRApp(value, props)
  app.use(createI18n({ legacy: false, locale, messages }))
  app.component('SearchEntry', value)
  for (const name of ['t-tag', 't-icon', 't-button', 't-select', 't-switch', 't-date-picker', 't-input-number', 't-alert', 't-loading', 't-empty', 't-dialog', 't-tooltip', 't-drawer']) app.component(name, controls?.[name] || stub)
  const warnings: string[] = [], errors: unknown[] = []
  app.config.warnHandler = warning => warnings.push(warning)
  app.config.errorHandler = error => errors.push(error)
  const html = await renderToString(app)
  assert.deepEqual(warnings, []); assert.deepEqual(errors, [])
  return html
}
const classificationModules = { './FollowToggle.vue': moduleOf(defineComponent({ setup: () => () => null })), '@/utils/markdownPlainText': markdownPlainText, '@/types/entryClassification': { }, '@/utils/security': { isValidURL: (url: string) => /^https?:\/\//.test(url) } }
function articleComponent() {
  const preview = component('../../components/ImagePreview.vue', {
    'tdesign-vue-next': { ImageViewer: defineComponent({ setup: () => () => null }) },
  })
  return component('../../components/SearchEntry.vue', {
    './EntryUpdateNotice.vue': moduleOf(component('../../components/EntryUpdateNotice.vue', {})),
    './EntryClassification.vue': moduleOf(component('../../components/EntryClassification.vue', classificationModules)),
    './SearchImage.vue': moduleOf(component('../../components/SearchImage.vue', {
      '@/utils/protectedFileAccess': protectedAccess, './ImagePreview.vue': moduleOf(preview),
    })),
    './GlobalCommandPalette/useHighlight': highlight,
    '@/utils/markdownPlainText': markdownPlainText,
  })
}
const article: SearchEntry = {
  entry_id: 'a', title: 'Launch article', source: 'article', published_at: '2026-10-11T08:00:00Z',
   summary: { overview: 'Launch overview', points: ['Useful point'], key_terms: ['Launch term'] },
  entities: [{ id: 'e1', canonical_name: 'Launch Labs', entity_type: 'company', aliases: ['LL'] }],
  snippets: [0, 1, 2, 3].map(index => ({ chunk_id: `c${index}`, chunk_index: index, content: `Launch snippet ${index} <script>`, score: 0.9, match_type: 'keyword' })),
  score: 0.9,  vault_path: 'articles/2026-10/a.md', original_url: 'https://example.com/a',
}
const markdownExcerpt = '# Launch heading\n\n- **bold** [read](https://example.com/private)\n\n> _quote_\n\n`code` ![remote](https://example.com/chart.png) <img src="../../attachments/a/photo.png"> <b>HTML</b>'

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: API-loaded search excerpts render plain text and preserve the source Markdown`, async () => {
    const summary = { overview: '**Overview** ![chart](chart.png)', points: ['- _Point_', '\\*literal\\*', '`**literal**`'], key_terms: ['`Term`'] }
    let loadedSummary: SearchEntry['summary'] = null
    const api = ingestSearchAPI({
      summary,
      snippets: [{ ...article.snippets[0], content: markdownExcerpt }],
    })
    const result = await pageScenario(async state => {
      state.query.value = 'Launch'; await state.runSearch()
      assert.equal(state.results.value[0].snippets[0].content, markdownExcerpt)
      assert.equal(state.results.value[0].summary.overview, '**Overview** ![chart](chart.png)')
      loadedSummary = state.results.value[0].summary
    }, api.searchEntries, locale)
    const image = locale === 'zh-CN' ? '[图片]' : '[Image]'
    assert.ok(result.html.includes(`Overview ${image}`))
    assert.match(result.html, /<li[^>]*>\*literal\*<\/li>/)
    assert.match(result.html, /<li[^>]*>\*\*literal\*\*<\/li>/)
    assert.deepEqual(loadedSummary, summary)
    assert.match(result.html, /<mark class="search-highlight">Launch<\/mark> heading/)
    assert.ok(result.html.includes(`code ${image} ${image} HTML`))
    assert.match(result.html, /bold read/)
    assert.doesNotMatch(result.html, /# Launch|\*\*bold|!\[remote\]|<img|<b>HTML|example.com\/private|attachments\/a\/photo/)
  })
  test(`${locale}: actual command palette renders API-loaded Markdown snippets as localized plain text`, async () => {
    const api = ingestSearchAPI({ snippets: [{ ...article.snippets[0], content: markdownExcerpt }] })
    const hook = evaluate(readFileSync(new URL('../../components/GlobalCommandPalette/useSearch.ts', import.meta.url), 'utf8'), { '@/utils/search': helpers, '@/api/search': api })
    const store = { open: ref(true), initialQuery: ref('Launch'), recentQueries: ref([]), closePalette() {}, pushRecent() {} }
    const palette = component('../../components/GlobalCommandPalette.vue', {
      pinia: { storeToRefs: (value: unknown) => value },
      '@/stores/commandPalette': { useCommandPaletteStore: () => store },
      '@/stores/auth': { useAuthStore: () => ({}) },
      'vue-router': { useRoute: () => ({ name: 'search' }), useRouter: () => ({ push() {} }) },
      './GlobalCommandPalette/useSearch': hook,
      './GlobalCommandPalette/visibleFileChunks': visibleFileChunks,
      './GlobalCommandPalette/useHighlight': highlight,
      './GlobalCommandPalette/commands': paletteCommands,
      '@/utils/markdownPlainText': markdownPlainText,
      './GlobalCommandPalette/ResultGroup.vue': moduleOf(component('../../components/GlobalCommandPalette/ResultGroup.vue', {})),
      './GlobalCommandPalette/ResultItem.vue': moduleOf(component('../../components/GlobalCommandPalette/ResultItem.vue', {})),
      '@/components/EntryUpdateNotice.vue': moduleOf(component('../../components/EntryUpdateNotice.vue', {})),
      './SearchImage.vue': moduleOf(defineComponent({ setup: () => () => null })),
      '@/views/settings/RetrievalSettings.vue': moduleOf(defineComponent({ setup: () => () => null })),
    })
    const html = await render(palette, {}, locale, async state => {
      state.query.value = 'Launch'; await state.retry()
      assert.equal(state.fileGroups.value[0].chunks[0].content, markdownExcerpt)
    })
    const image = locale === 'zh-CN' ? '[图片]' : '[Image]'
    assert.match(html, /<mark class="search-highlight">Launch<\/mark> heading/)
    assert.ok(html.includes(`code ${image} ${image} HTML`))
    assert.doesNotMatch(html, /# Launch|\*\*bold|!\[remote\]|<img|<b>HTML|example.com\/private|attachments\/a\/photo/)
  })
}

function ingestSearchAPI(fields: Record<string, unknown>) {
  return evaluate(readFileSync(new URL('../../api/search.ts', import.meta.url), 'utf8'), {
    '@/utils/request': {
      get: async () => ({ success: true, data: [{ id: 'all-library' }] }),
      post: async (url: string) => {
        assert.equal(url, '/api/v1/search')
        return { success: true, data: { results: structuredClone([{ ...article, ...fields }]), excluded_missing_published_at: 0 } }
      },
    },
  })
}

test('search requests and URL state preserve all filters without old scope parameters', () => {
  const filters = { ...helpers.defaultSearchFilters(),  published_from: '2026-10-01', published_to: '2026-10-11',  limit: 50 }
  const mutable = { ...filters, }
  assert.deepEqual(helpers.buildSearchRequest('  Launch  ', mutable), { query: 'Launch', ...mutable })
  assert.deepEqual(helpers.searchStateFromURL(helpers.searchStateToURL('Launch', mutable)), { query: 'Launch', filters: mutable })
  assert.deepEqual(helpers.buildSearchRequest('Launch', helpers.defaultSearchFilters()), { query: 'Launch',  limit: 20 })
  const invalid = helpers.searchStateFromURL({  sources: 'mine,removed', limit: '200' })
  assert.equal('sources' in invalid.filters, false)
  assert.equal('sources' in helpers.buildSearchRequest(invalid.query, invalid.filters), false)
  assert.equal('sources' in helpers.searchStateToURL(invalid.query, invalid.filters), false)
   assert.equal(invalid.filters.limit, 20)
})
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: search markers show and hide using fields loaded through the real search API`, async () => {
    for (const flagged of [true, false, undefined]) {
      const api = ingestSearchAPI({ acquisition: flagged ? 'model_written' : 'extracted', ...(flagged !== undefined ? { incomplete: flagged } : {}) })
      const result = await pageScenario(async state => { state.query.value = 'Launch'; await state.runSearch() }, api.searchEntries, locale)
      assert.equal(result.html.includes(messages[locale].ingestStatus.modelWritten), Boolean(flagged))
      assert.equal(result.html.includes(messages[locale].ingestStatus.incomplete), false)
      for (const hint of [messages[locale].ingestStatus.modelWrittenHint]) {
        assert.equal(result.html.includes(`title="${hint.replaceAll("'", "&#39;")}"`), Boolean(flagged), hint)
      }
    }
  })

  test(`${locale}: source unverified marker reaches search results through the page API loading path`, async () => {
    const result = await pageScenario(async state => { state.query.value = 'Launch'; await state.runSearch() }, async () => ({ success: true, data: { results: [{ ...article, source_unverified: true }], excluded_missing_published_at: 0 } }), locale)
    assert.ok(result.html.includes(messages[locale].knowledgeReview.sourceUnverified))
    assert.match(result.html, /data-article-id="a"/)
  })
  test(`${locale}: real search API loading forwards image snippets to the protected thumbnail component`, async () => {
    const imagePath = '../../attachments/image-article/only-in-image.png'
    const result = await pageScenario(async state => {
      state.query.value = 'red bicycle'
      await state.runSearch()
      assert.equal(state.results.value[0].snippets[0].image_path, imagePath)
    }, async () => ({ success: true, data: { results: [{ ...article, entry_id: 'image-article', snippets: [{ chunk_id: 'image', chunk_index: 0, content: '', score: 0.99, match_type: 'image', image_path: imagePath }] }] } }), locale)
    assert.ok(result.html.includes(`data-image-path="${imagePath}"`))
    assert.ok(result.html.includes(messages[locale].knowledgeSearch.imageMatch))
  })
  test(`${locale}: article results show summary, attribution, safe link and at most three highlighted snippets`, async () => {
    const html = await render(articleComponent(), { article, query: 'Launch' }, locale)
    for (const text of ['Launch article', 'Launch overview', 'Useful point', 'Launch term', 'Launch Labs', article.vault_path, messages[locale].knowledgeSearch.sources.article]) assert.ok(html.includes(text), text)
    assert.match(html, /href="https:\/\/example.com\/a"/)
    assert.match(html, /<mark class="search-highlight">Launch<\/mark> snippet 0/)
    assert.doesNotMatch(html, /snippet 3|<script>/)
  })



}

async function pageScenario(action: (state: any, calls: SearchRequest[], pushes: any[]) => Promise<void>, search?: (request: SearchRequest) => Promise<any>, locale: keyof typeof messages = 'zh-CN') {
  const calls: SearchRequest[] = [], pushes: any[] = []
  const page = component('./SearchView.vue', {
    'vue-router': { useRoute: () => ({ query: {} }), useRouter: () => ({ replace: (value: unknown) => Promise.resolve(pushes.push(value)), push: (value: unknown) => Promise.resolve(pushes.push(value)) }) },
    '@/api/search': { searchEntries: (request: SearchRequest) => { calls.push(request); return search ? search(request) : Promise.resolve({ success: true, data: { results: [article], excluded_missing_published_at: 2 } }) }, getSearchOptions: async () => ({ success: true, data: { entities: article.entities, } }) },
    '@/components/SearchFilters.vue': moduleOf(stub), '@/components/SearchEntry.vue': moduleOf(articleComponent()), '@/utils/search': helpers,
  })
  const html = await render(page, {}, locale, state => action(state, calls, pushes))
  return { calls, pushes, html }
}
test('search page submits all filters, renders exclusion count and opens the article detail', async () => {
  const result = await pageScenario(async (state, calls, pushes) => {
    state.query.value = ' Launch '
    state.filters.value = { ...helpers.defaultSearchFilters(),  published_from: '2026-10-01', published_to: '2026-10-11', }
    await state.runSearch()
    await state.loadOptions()
    assert.equal(calls.length, 1);

    state.openArticle(article)
    assert.deepEqual(pushes.at(-1), { path: '/platform/entries', query: { entry_id: 'a' } })
    state.openEntity('e1')
    assert.deepEqual(pushes.at(-1), { path: '/platform/entries', query: { tab: 'entities', entity_id: 'e1' } })
  })
  assert.ok(result.html.includes('已排除 2 篇缺失发布时间的资料。')); assert.match(result.html, /data-article-id="a"/)
})
test('search page rejects invalid date order and ignores an older response after a new query', async () => {
  let resolveOld!: (value: any) => void
  const old = new Promise(resolve => { resolveOld = resolve })
  await pageScenario(async (state, calls) => {
    state.query.value = 'invalid'
    state.filters.value.published_from = '2026-10-12'; state.filters.value.published_to = '2026-10-11'
    await state.runSearch(); assert.equal(calls.length, 0); assert.equal(state.error.value, zh.knowledgeSearch.invalidDateRange)
    state.filters.value = helpers.defaultSearchFilters()
    state.query.value = 'old'
    const first = state.runSearch()
    state.query.value = 'new'
    await state.runSearch()
    resolveOld({ success: true, data: { results: [{ ...article, entry_id: 'old' }], excluded_missing_published_at: 99 } })
    await first
    assert.equal(state.results.value[0].entry_id, 'a'); assert.equal(state.excludedMissingPublishedAt.value, 0)
    state.clearQuery(); assert.equal(state.results.value.length, 0); assert.equal(state.hasSearched.value, false)
  }, request => request.query === 'old' ? old : Promise.resolve({ success: true, data: { results: [article], excluded_missing_published_at: 0 } }))
})

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: command palette markers use response fields through the real search API and hook`, async () => {
    for (const flagged of [true, false, undefined]) {
      const api = ingestSearchAPI({ acquisition: flagged ? 'model_written' : 'verbatim', ...(flagged !== undefined ? { incomplete: flagged } : {}) })
      const hook = evaluate(readFileSync(new URL('../../components/GlobalCommandPalette/useSearch.ts', import.meta.url), 'utf8'), { '@/utils/search': helpers, '@/api/search': api })
      const item = component('../../components/GlobalCommandPalette/ResultItem.vue', {})
      const palette = defineComponent({ setup() {
        const state = hook.useCmdkSearch({})
        onServerPrefetch(async () => { state.query.value = 'Launch'; await state.retry() })
        return () => state.fileGroups.value.length ? h(item, { index: 0, title: state.fileGroups.value[0].title, acquisition: state.fileGroups.value[0].acquisition, incomplete: state.fileGroups.value[0].incomplete }) : null
      } })
      const html = await render(palette, {}, locale)
      assert.equal(html.includes(messages[locale].ingestStatus.modelWritten), Boolean(flagged))
      assert.equal(html.includes(messages[locale].ingestStatus.incomplete), false)
      for (const hint of [messages[locale].ingestStatus.modelWrittenHint]) {
        assert.equal(html.includes(`title="${hint.replaceAll("'", "&#39;")}"`), Boolean(flagged), hint)
      }
    }
    const template = readFileSync(new URL('../../components/GlobalCommandPalette.vue', import.meta.url), 'utf8')
    assert.match(template, /:acquisition="item.file.acquisition"/)
    assert.doesNotMatch(template, /:incomplete=/)
  })
  test(`${locale}: dependency failures render a localized message and retain retry without upstream text`, async () => {
    let attempts = 0
    const result = await pageScenario(async state => {
      state.query.value = 'Launch'
      await state.runSearch()
      assert.equal(state.error.value, messages[locale].knowledgeSearch.unavailable)
    }, async () => { attempts++; throw { error: { code: 2400, message: '<html>EmbedBatch upstream secret</html>' } } }, locale)
    assert.ok(result.html.includes(messages[locale].knowledgeSearch.unavailable))
    assert.ok(result.html.includes(messages[locale].common.retry))
    assert.doesNotMatch(result.html, /EmbedBatch|upstream|html&gt;/)
    assert.equal(attempts, 1)
    await pageScenario(async state => {
      state.query.value = 'Launch'; await state.runSearch()
      assert.equal(state.error.value, messages[locale].knowledgeSearch.searchFailed)
      await state.runSearch(); assert.equal(state.error.value, ''); assert.equal(state.results.value.length, 1)
    }, async () => { if (attempts++ === 1) throw new Error('<html>upstream secret</html>'); return { success: true, data: { results: [article] } } }, locale)
  })
}
for (const count of [1, 2]) {
  test(`English quantities use correct article and report nouns for ${count}`, async () => {
    const reports = Array.from({ length: count }, (_, index) => ({ ...article, entry_id: `b${index}`, }))
    const result = await pageScenario(async state => { state.query.value = 'Launch'; await state.runSearch() }, async () => ({ success: true, data: { results: reports, excluded_missing_published_at: count } }), 'en-US')
    const articleNoun = count === 1 ? 'material' : 'materials'
    assert.ok(result.html.includes(`Showing ${count} ${articleNoun}`))
    assert.ok(result.html.includes(`Excluded ${count} ${articleNoun} without a publication date.`))
  })
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: command palette API loading retains a matched image behind the representative text snippet`, async () => {
    const imagePath = '../../attachments/palette-image/chart.png'
    const hook = evaluate(readFileSync(new URL('../../components/GlobalCommandPalette/useSearch.ts', import.meta.url), 'utf8'), {
      '@/utils/search': helpers,
      '@/api/search': { searchEntries: async () => ({ success: true, data: { results: [{ ...article, snippets: [article.snippets[0], { chunk_id: '', chunk_index: -1, content: '', match_type: 'image', score: 0.5, image_path: imagePath }] }] } }) },
    })
    const palette = defineComponent({ setup() {
      const state = hook.useCmdkSearch({})
      onServerPrefetch(async () => { state.query.value = 'Launch'; await state.retry(); assert.equal(state.fileGroups.value[0].imagePath, imagePath); assert.equal(state.fileGroups.value[0].chunks[0].content, article.snippets[0].content) })
      return () => null
    } })
    await render(palette, {}, locale)
    assert.match(readFileSync(new URL('../../components/GlobalCommandPalette.vue', import.meta.url), 'utf8'), /:path="item.file.imagePath"/)
  })
  test(`${locale}: command palette API loading preserves and renders the source unverified marker`, async () => {
    const hook = evaluate(readFileSync(new URL('../../components/GlobalCommandPalette/useSearch.ts', import.meta.url), 'utf8'), {
      '@/utils/search': helpers,
      '@/api/search': { searchEntries: async () => ({ success: true, data: { results: [{ ...article, source_unverified: true }] } }) },
    })
    const item = component('../../components/GlobalCommandPalette/ResultItem.vue', {})
    const palette = defineComponent({ setup() {
      const state = hook.useCmdkSearch({})
      onServerPrefetch(async () => { state.query.value = 'Launch'; await state.retry(); assert.equal(state.fileGroups.value[0].source_unverified, true) })
      return () => state.fileGroups.value.length ? h(item, { index: 0, title: state.fileGroups.value[0].title, sourceUnverified: state.fileGroups.value[0].source_unverified }) : null
    } })
    const html = await render(palette, {}, locale)
    assert.ok(html.includes(messages[locale].knowledgeReview.sourceUnverified))
    assert.match(readFileSync(new URL('../../components/GlobalCommandPalette.vue', import.meta.url), 'utf8'), /:source-unverified="item.file.source_unverified"/)
  })
  test(`${locale}: command palette failure is localized and retry restores article results`, async () => {
    let attempts = 0
    const hook = evaluate(readFileSync(new URL('../../components/GlobalCommandPalette/useSearch.ts', import.meta.url), 'utf8'), {
      '@/utils/search': helpers,
      '@/api/search': { searchEntries: async () => {
        if (attempts++ === 0) throw { error: { code: 2400, message: '<html>upstream secret</html>' } }
        return { success: true, data: { results: [article] } }
      } },
    })
    const palette = defineComponent({ setup() {
      const state = hook.useCmdkSearch({})
      onServerPrefetch(async () => {
        state.query.value = 'Launch'
        await state.retry()
        assert.equal(state.searchError.value, messages[locale].knowledgeSearch.unavailable)
        assert.equal(state.loading.value, false)
        await state.retry()
        assert.equal(state.searchError.value, '')
        assert.equal(state.fileGroups.value[0].entryId, article.entry_id)
      })
      return () => h('div', state.searchError.value)
    } })
    const html = await render(palette, {}, locale)
    assert.doesNotMatch(html, /upstream|html&gt;/)
    assert.equal(attempts, 2)
  })
}


test('search results render their library display name', async () => {
  const html = await render(articleComponent(), { article: { ...article, library_id: 'lib-a', library_name: '机构资料库' }, query: '' }, 'zh-CN')
  assert.ok(html.includes('机构资料库'))
  assert.ok(html.includes('article-library'))
  const groups = evaluate(readFileSync(new URL('../../components/GlobalCommandPalette/useSearch.ts', import.meta.url), 'utf8'), {
    '@/utils/search': helpers, '@/api/search': {},
  }).articleFileGroups([{ ...article, library_id: 'lib-a', library_name: '机构资料库' }])
  assert.equal(groups[0].libraryName, '机构资料库')
  assert.equal(groups[0].libraryId, 'lib-a')
})



for (const locale of ['zh-CN','en-US'] as const) test(`${locale}: real search results show an update link and global grouping retains the latest ID`,async()=>{
 const api=ingestSearchAPI({source:'mine',has_update:true,latest_entry_id:'latest-record'})
 const result=await pageScenario(async state=>{state.query.value='Launch';await state.runSearch()},api.searchEntries,locale)
 assert.ok(result.html.includes(messages[locale].ikb18.hasUpdate))
 assert.ok(result.html.includes('/platform/entries?entry_id=latest-record'))
 const groups=evaluate(readFileSync(new URL('../../components/GlobalCommandPalette/useSearch.ts',import.meta.url),'utf8'),{'@/utils/search':helpers,'@/api/search':{}}).articleFileGroups([{...article,source:'mine',has_update:true,latest_entry_id:'latest-record'}])
 assert.equal(groups[0].has_update,true);assert.equal(groups[0].latest_entry_id,'latest-record')
})
