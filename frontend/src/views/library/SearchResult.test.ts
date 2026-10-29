import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import { createSSRApp, defineComponent } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { loadComponent, renderer, element, descendants, click, visibleText } from './componentTestHarness'
import { entryPresentationTestModules } from './entryPresentationTestModules'
import * as bodyReference from '../../utils/bodyReference'
import * as highlight from '../../components/GlobalCommandPalette/useHighlight'
import * as markdown from '../../utils/markdownPlainText'
import * as security from '../../utils/security'
import { CONTENT_TYPES } from '../../types/entryClassification'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'

export function searchResultComponent() {
  return loadComponent(new URL('./SearchResult.vue', import.meta.url), {
    ...entryPresentationTestModules,
    '@/utils/bodyReference': bodyReference, '@/utils/markdownPlainText': markdown,
    '@/utils/security': security, '@/types/entryClassification': { CONTENT_TYPES },
    '@/components/GlobalCommandPalette/useHighlight': highlight,
    '@/components/SearchImage.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
    '@/components/EntryUpdateNotice.vue': { __esModule: true, default: loadComponent(new URL('../../components/EntryUpdateNotice.vue', import.meta.url), {}) },
  })
}

for (const locale of ['zh-CN', 'en-US'] as const) test(`${locale}: results link to real paragraph references and retain metadata without rendering unsafe markup`, async () => {
  const location = { paragraph: 7, start: 42, end: 87, body_hash: 'v1', heading_path: ['Heading'], mapping: 'exact' }
  const app = createSSRApp(searchResultComponent(), {
    query: 'needle', libraries: [{ id: 'library', display_name: 'Research', color: 6 }],
    article: { entry_id: 'entry', library_id: 'library', title: 'Real result', source: 'article', content_type: 'Custom type', score: .73, published_at: null, entities: [], topics: [], similar_reports: [],
      source_unverified: true, acquisition: 'model_written', summary: null, snippets: [{ chunk_id: 'c', content: '**needle** <script>bad()</script>', locations: [location] }] },
  })
  app.use(createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } }))
  const html = await renderToString(app)
  assert.ok(html.includes(bodyReference.entryDetailHref('entry', location).replaceAll('&', '&amp;')))
  assert.match(html, /<mark class="search-highlight">needle<\/mark>/)
  assert.match(html, /¶7/)
  for (const value of ['Research', 'Custom type', '0.73', 'var(--lib-6)', locale === 'zh-CN' ? '来源未核实' : en.review.sourceUnverified]) assert.ok(html.includes(value), value)
  assert.doesNotMatch(html, /<script>|\*\*needle|class="t-/)
})

test('folded search results expand with their own source markers and reference links', async () => {
  const location = { paragraph: 3, start: 12, end: 27, body_hash: 'nested-hash', heading_path: [], mapping: 'exact' }
  const nested = { entry_id: 'nested', title: 'Related material', score: .6, source: 'article', source_unverified: true, snippets: [{ content: 'Related passage', locations: [location] }], similar_reports: [] }
  const app = renderer.createApp(searchResultComponent(), {
    query: 'Related', libraries: [], article: { entry_id: 'main', title: 'Main result', score: .8, source: 'article', snippets: [], similar_reports: [nested] },
  })
  app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en } }))
  const root = element('root')
  try {
    app.mount(root)
    assert.ok(!visibleText(root).includes('Related material'))
    const toggle = descendants(root).find(node => node.tag === 'button' && node.props['aria-expanded'] === false)!
    await click(toggle)
    assert.ok(visibleText(root).includes('Related material'))
    assert.ok(visibleText(root).includes(en.review.sourceUnverified))
    assert.ok(descendants(root).some(node => node.tag === 'a' && node.props.href === bodyReference.entryDetailHref('nested', location)))
    await click(toggle)
    assert.ok(!visibleText(root).includes('Related material'))
  } finally { app.unmount() }
})
