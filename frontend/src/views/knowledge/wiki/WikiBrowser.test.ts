import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import zhCN from '../../../i18n/locales/zh-CN'
import enUS from '../../../i18n/locales/en-US'
import { expandedWikiDirectoryPaths, expandWikiDirectoryPath } from './wikiDirectoryState'
import { click, descendants, element, hasClass, loadComponent, mockViewport, registerPrimitives, renderer, settle, visibleText } from './wikiComponentTestHarness'

const descriptions = {
  'zh-CN': ['页面“Page A”的 1 个来源分块已修改', '页面“Page A”的 2 个来源分块已修改'],
  'en-US': ["Page 'Page A' has 1 modified source chunk", "Page 'Page A' has 2 modified source chunks"],
}

async function mountBrowser(locale: keyof typeof descriptions = 'zh-CN', count = 1, width = 1440) {
  const viewport = mockViewport(width)
  const oldObserver = globalThis.IntersectionObserver
  const oldStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: () => null, setItem() {} } })
  globalThis.IntersectionObserver = class {
    observe() {} disconnect() {} unobserve() {}
  } as unknown as typeof IntersectionObserver
  const oldSetInterval = globalThis.setInterval
  const oldClearInterval = globalThis.clearInterval
  let polling: (() => void) | null = null
  globalThis.setInterval = ((callback: () => void) => { polling = callback; return 1 }) as unknown as typeof setInterval
  globalThis.clearInterval = (() => { polling = null }) as typeof clearInterval
  let changed = true
  let pending = false
  const requests: Array<{ action: string; slug?: string }> = []
  const statusCounts: number[] = []
  let sessions = 0
  const page = () => ({
    id: 'page-a', knowledge_base_id: 'kb-a', slug: 'concept/a', title: 'Page A', page_type: 'concept',
    content: 'Reader body', summary: 'Page summary', version: 1, last_edit_source: 'pipeline',
    in_links: [], out_links: [], aliases: [], source_refs: [], page_metadata: {},
    updated_at: '2026-10-08T06:20:15Z',
    source_changes: changed ? Object.fromEntries(Array.from({ length: count }, (_, i) => [`chunk-${i + 1}`, '2026-10-08T06:20:15Z'])) : {},
  })
  const api = {
    listWikiPages: async (_kbId: string, params: { page_type: string }) => ({ data: {
      pages: params.page_type.includes('concept') ? [page()] : [], total: params.page_type.includes('concept') ? 1 : 0,
    } }),
    listWikiFolders: async () => ({ data: { folders: [] } }),
    getWikiIndex: async () => ({ data: { intro: 'Wiki intro' } }),
    getWikiPage: async (_kbId: string, slug: string) => { requests.push({ action: 'page', slug }); return { data: page() } },
    getWikiStats: async () => ({ data: { pending_issues: changed ? 1 : 0, pending_tasks: pending ? 1 : 0, is_active: pending, pages_by_type: {} } }),
    listWikiIssues: async (_kbId: string, slug: string) => {
      requests.push({ action: 'issues', slug })
      return { data: changed ? [{ id: 'issue-a', slug: 'concept/a', issue_type: 'source_modified',
        modified_chunk_count: count, reported_by: 'wiki-lint', description: descriptions[locale][count - 1] }] : [] }
    },
    updateWikiIssueStatus: async () => { requests.push({ action: 'ignore-issue' }); changed = false },
    ignoreWikiSourceChanges: async () => { requests.push({ action: 'ignore-source' }); changed = false; return { data: page() } },
    regenerateWikiSourceChanges: async () => { requests.push({ action: 'regenerate' }); pending = true; return {} },
  }
  const empty = defineComponent({ setup: () => () => null })
  const modules = {
    '@/api/wiki': api,
    'tdesign-vue-next': { MessagePlugin: { success() {}, error() {} } },
  }
  const Layout = loadComponent(new URL('./WikiReadingLayout.vue', import.meta.url), {})
  const Notice = loadComponent(new URL('./WikiSourceChanges.vue', import.meta.url), modules)
  const Browser = loadComponent(new URL('./WikiBrowser.vue', import.meta.url), {
    ...modules,
    'vue-router': {
      useRouter: () => ({ resolve: ({ query }: { query: Record<string, string> }) => ({ href: `?slug=${encodeURIComponent(query.slug)}` }) }),
      useRoute: () => ({ path: '/knowledge/kb-a', query: { slug: 'concept/a' } }),
    },
    '@/stores/menu': { useMenuStore: () => ({}) },
    '@/stores/settings': { useSettingsStore: () => ({}) },
    marked: { marked: { parse: (value: string) => value } },
    'vue-virtual-scroller': { RecycleScroller: defineComponent({ props: ['items'], setup: (props, { slots }) =>
      () => h('div', props.items.map((item: unknown) => slots.default?.({ item }))) }) },
    '@/utils/security': { hydrateProtectedFileImages: async () => {}, sanitizeMarkdownHTML: (value: string) => value },
    '@/components/picture-preview.vue': empty,
    './WikiFolderActions.vue': empty,
    './WikiRevisionDrawer.vue': empty,
    './WikiSourceChanges.vue': Notice,
    './WikiReadingLayout.vue': Layout,
    './wikiDirectoryState': { expandedWikiDirectoryPaths, expandWikiDirectoryPath },
    '@/api/knowledge-base': {},
    '@/api/chat': { createSessions: async () => { sessions++; return {} } },
    '@/views/chat/index.vue': empty,
  })
  const app = renderer.createApp({ render: () => h(Browser, { knowledgeBaseId: 'kb-a', view: 'browser', canEdit: true,
    onStatusChange: ({ pendingIssues }: { pendingIssues: number }) => statusCounts.push(pendingIssues) }) })
  app.use(createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } }))
  registerPrimitives(app)
  const errors: unknown[] = []
  app.config.errorHandler = error => errors.push(error)
  const root = element('root')
  app.mount(root)
  await settle()
  const copy = (locale === 'zh-CN' ? zhCN : enUS).knowledgeEditor.wikiBrowser
  const openGlobal = async () => click(descendants(root).find(node => hasClass(node, 'wiki-global-issues-status'))!)
  const openPageIssues = async () => {
    const trigger = descendants(root).find(node => hasClass(node, 'wiki-issue-trigger'))!
    await click(trigger.parent!)
  }
  return { root, requests, statusCounts, errors, copy, openGlobal, openPageIssues, sessions: () => sessions,
    async completeRegeneration() { changed = false; pending = false; assert.ok(polling); polling(); await settle() },
    unmount() {
      app.unmount()
      assert.equal(polling, null, 'all component polling stops on unmount')
      globalThis.setInterval = oldSetInterval
      globalThis.clearInterval = oldClearInterval
      globalThis.IntersectionObserver = oldObserver
      if (oldStorage) Object.defineProperty(globalThis, 'localStorage', oldStorage)
      else delete (globalThis as any).localStorage
      viewport.restore()
    },
  }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const count of [1, 2]) {
    test(`${locale} renders the source issue server description with source count ${count} and links to its page`, async () => {
      const browser = await mountBrowser(locale, count)
      assert.equal(browser.statusCounts.at(-1), 1)
      await browser.openGlobal()
      const issue = descendants(browser.root).find(node => hasClass(node, 'wiki-issue-popup-item'))!
      assert.ok(visibleText(issue).includes(descriptions[locale][count - 1]!))
      assert.ok(visibleText(issue).includes(browser.copy.sourceChangesTitle))
      const link = descendants(issue).find(node => node.tag === 'a')!
      assert.equal(link.props.href, '?slug=concept%2Fa')
      await click(link)
      assert.deepEqual(browser.requests.filter(request => request.action === 'page').at(-1), { action: 'page', slug: 'concept/a' })
      assert.ok(visibleText(browser.root).includes(browser.copy.sourceChangesTitle))
      await browser.openPageIssues()
      const goFix = descendants(browser.root).find(node => node.tag === 'button' && visibleText(node).trim() === browser.copy.issueGoFix)!
      await click(goFix)
      assert.equal(browser.sessions(), 0, 'source issues use the page source controls')
      assert.deepEqual(browser.errors, [])
      browser.unmount()
    })
  }

  test(`${locale} ignoring a source issue clears the marker, both issue lists and the pending count`, async () => {
    const browser = await mountBrowser(locale)
    await browser.openGlobal()
    const drawer = descendants(browser.root).find(node => hasClass(node, 'wiki-global-issues-drawer'))!
    await click(descendants(drawer).find(node => node.tag === 'button' && visibleText(node).trim() === browser.copy.issueIgnore)!)
    assert.equal(browser.statusCounts.at(-1), 0)
    assert.equal(descendants(browser.root).some(node => hasClass(node, 'wiki-source-changes')), false)
    assert.equal(descendants(browser.root).some(node => hasClass(node, 'wiki-issue-trigger')), false)
    assert.equal(descendants(browser.root).some(node => hasClass(node, 'wiki-issue-popup-item')), false)
    assert.ok(visibleText(browser.root).includes(browser.copy.globalIssuesEmpty))
    assert.deepEqual(browser.errors, [])
    browser.unmount()
  })

  test(`${locale} successful regeneration refreshes the open global list, page issues and pending count`, async () => {
    const browser = await mountBrowser(locale)
    const regenerate = descendants(browser.root).find(node => node.tag === 'button' && visibleText(node).trim() === browser.copy.sourceChangesRegenerate)!
    await click(regenerate.parent!)
    await click(descendants(browser.root).find(node => node.props['data-confirm'])!)
    assert.equal(browser.statusCounts.at(-1), 1)
    await browser.openGlobal()
    assert.ok(descendants(browser.root).some(node => hasClass(node, 'wiki-issue-popup-item')))
    await browser.completeRegeneration()
    assert.equal(browser.statusCounts.at(-1), 0)
    assert.equal(descendants(browser.root).some(node => hasClass(node, 'wiki-source-changes')), false)
    assert.equal(descendants(browser.root).some(node => hasClass(node, 'wiki-issue-trigger')), false)
    assert.equal(descendants(browser.root).some(node => hasClass(node, 'wiki-issue-popup-item')), false)
    assert.deepEqual(browser.errors, [])
    browser.unmount()
  })

  test(`${locale} at 390px can select the current page, close the directory and ignore its sources`, async () => {
    const browser = await mountBrowser(locale, 1, 390)
    assert.equal(descendants(browser.root).some(node => hasClass(node, 'wiki-sidebar')), false)
    const openDirectory = () => click(descendants(browser.root).find(node => node.tag === 'button' && visibleText(node).trim() === browser.copy.openDirectory)!)
    await openDirectory()
    const item = descendants(browser.root).find(node => hasClass(node, 'wiki-page-item'))!
    assert.ok(item, 'the real browser directory includes the selected page')
    await click(item)
    assert.equal(descendants(browser.root).some(node => hasClass(node, 'wiki-sidebar')), false, 'selecting the current page also closes the directory')
    assert.ok(descendants(browser.root).some(node => hasClass(node, 'wiki-source-changes')))
    await openDirectory()
    await click(descendants(browser.root).find(node => hasClass(node, 'wiki-nav-item'))!)
    assert.equal(descendants(browser.root).some(node => hasClass(node, 'wiki-sidebar')), false, 'selecting the index closes the directory')
    const countButton = descendants(browser.root).find(node => node.tag === 'button' && visibleText(node).includes('1') && !visibleText(node).includes(browser.copy.openDirectory))!
    await click(countButton)
    const link = descendants(browser.root).find(node => hasClass(node, 'wiki-issue-page-link'))!
    await click(link)
    assert.ok(descendants(browser.root).some(node => hasClass(node, 'wiki-source-changes')))
    const notice = descendants(browser.root).find(node => hasClass(node, 'wiki-source-changes'))!
    await click(descendants(notice).find(node => node.tag === 'button' && visibleText(node).trim() === browser.copy.sourceChangesIgnore)!)
    assert.equal(browser.statusCounts.at(-1), 0)
    assert.equal(descendants(browser.root).some(node => hasClass(node, 'wiki-source-changes')), false)
    assert.equal(descendants(browser.root).some(node => hasClass(node, 'wiki-issue-trigger')), false)
    assert.ok(browser.requests.some(request => request.action === 'ignore-source'))
    assert.deepEqual(browser.errors, [])
    browser.unmount()
  })
}
