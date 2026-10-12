import assert from 'node:assert/strict'
import * as entrySourceLabels from '../../utils/entrySourceLabels'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'
import { computed, defineComponent, h, onMounted, reactive, ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import { CONTENT_TYPES } from '../../types/entryClassification'
import { ENTITY_TYPES } from '../../api/entity-types'
import * as highlight from '../../components/GlobalCommandPalette/useHighlight'
import { click as clickNode, descendants, element, hasClass, loadComponent, registerPrimitives, renderer, settle as settleNodes, visibleText, type Element } from './componentTestHarness'

const require = createRequire(import.meta.url), messages = { 'zh-CN': zh, 'en-US': en }
const moduleOf = (value: unknown) => ({ __esModule: true, default: value })
async function settle() { await settleNodes(); await new Promise<void>(resolve => setImmediate(resolve)); await settleNodes() }
async function click(node: Element) { await clickNode(node); await settle() }
const empty = moduleOf(defineComponent({ setup: () => () => null }))
const entity = { id: 'entity-a', canonical_name: 'Aurora', entity_type: 'company', aliases: ['Aurora Labs'], created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z' }
const topic = { id: 'topic-a', name: 'Space research', description: 'Space missions', article_count: 2, created_at: '', updated_at: '' }
const follow = (type: 'entity' | 'topic', id: string, name: string) => ({ target_type: type, target_id: id, name, created_at: '2026-10-01T00:00:00Z', recent_article_count: 2 })
const entityFollow = follow('entity', entity.id, entity.canonical_name), topicFollow = follow('topic', topic.id, topic.name)
const previous = '2026-10-10T08:00:00Z', openedAt = '2026-10-11T12:00:00Z', markedAt = '2026-10-11T14:00:00Z'
const article = (id: string, collected: string, matches = [entityFollow, topicFollow]) => ({ entry_id: id, title: `Article ${id}`, source: 'article', published_at: collected, collected_at: collected, entered_at: collected as string | null, is_new: true, matched_follows: matches, content_type: 'research', processing_mode: 'summary_only', summary: { overview: `Overview ${id}`, points: ['A useful point'], key_terms: ['Aurora'] }, entities: [entity], topics: [topic], snippets: [], score: 0, similar_reports: [], vault_path: `articles/2026-10/${id}.md`, original_url: `https://example.com/${id}` })
function evaluateFile(path: string, modules: Record<string, unknown>) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8')
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const value = { exports: {} as any }
  new Function('require', 'module', 'exports', code)((name: string) => modules[name] ?? require(name), value, value.exports)
  return value.exports
}
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }

function environment(locale: keyof typeof messages, initialTargets: any[] = []) {
  const calls: { method: string; url: string; body?: any }[] = [], warnings: string[] = [], errors: unknown[] = []
  const transport = {
    targets: structuredClone(initialTargets), checkpoint: previous as string | null,
    articles: [article('new', '2026-10-11T10:00:00Z'), article('similar', '2026-10-11T09:00:00Z', [topicFollow]), article('old', '2026-10-09T07:00:00Z', [entityFollow])],
    fail: (_method: string, _url: string, _body?: any) => false,
    intercept: (_method: string, _url: string, _body?: any): Promise<any> | undefined => undefined,
  }
  const state = () => ({ new_count: transport.articles.filter(item => item.matched_follows.some(target => transport.targets.some((actual: any) => actual.target_type === target.target_type && actual.target_id === target.target_id)) && (item.entered_at !== null && (!transport.checkpoint || item.entered_at > transport.checkpoint))).length, last_viewed_at: transport.checkpoint })
  const envelope = (data: any) => ({ success: true, data: structuredClone(data) })
  async function request(method: string, url: string, body?: any) {
    calls.push({ method, url, ...(body !== undefined ? { body: structuredClone(body) } : {}) })
    const delayed = transport.intercept(method, url, body)
    if (delayed) return delayed
    if (transport.fail(method, url, body)) throw new Error('transport unavailable')
    if (method === 'get' && url === '/api/v1/follows') return envelope(transport.targets)
    if (method === 'get' && url === '/api/v1/follows/state') return envelope(state())
    if (method === 'post' && url === '/api/v1/follows/view') { transport.checkpoint = body?.viewed_at || markedAt; return envelope(state()) }
    if (method === 'post' && url === '/api/v1/follows/feed') {
      const selected = transport.articles.filter(item => item.matched_follows.some(target => transport.targets.some((actual: any) => actual.target_type === target.target_type && actual.target_id === target.target_id)) && (!body.target_ids?.length || item.matched_follows.some(target => body.target_ids.includes(target.target_id))))
      let results: any[] = structuredClone(selected)
      for (const item of results) item.is_new = item.entered_at !== null && (!transport.checkpoint || item.entered_at > transport.checkpoint)
      if (body.collapse_similar && results.length > 1 && results[0].entry_id === 'new' && results[1].entry_id === 'similar') { results[0].similar_reports = [results[1]]; results.splice(1, 1) }
      results = results.slice(0, body.limit)
      return envelope({ ...state(), results, total: selected.length, viewed_at: openedAt })
    }
    const target = url.match(/^\/api\/v1\/follows\/(entity|topic)\/(.+)$/)
    if (target && ['put', 'del'].includes(method)) {
      const [type, id] = [target[1], decodeURIComponent(target[2])]
      const value = follow(type as any, id, type === 'entity' ? entity.canonical_name : topic.name)
      transport.targets = transport.targets.filter((entry: any) => entry.target_type !== type || entry.target_id !== id)
      if (method === 'put') transport.targets.push(value)
      return envelope(method === 'put' ? value : null)
    }
    if (method === 'get' && url === '/api/v1/topics') return envelope([topic])
    if (method === 'get' && url.endsWith('/backfill')) return envelope(null)
    if (method === 'del' && url === `/api/v1/topics/${topic.id}`) { transport.targets = transport.targets.filter((value: any) => value.target_id !== topic.id); return envelope(null) }
    if (method === 'get' && url.includes('/name-embeddings/rebuild')) return envelope({ pending_count: 0, latest_run: null })
    if (method === 'get' && url.startsWith('/api/v1/entities?')) return envelope([entity])
    if (method === 'get' && url === `/api/v1/entities/${entity.id}`) return envelope({ entity, entry: [{ id: 'new', title: 'Article new', created_at: openedAt }] })
    if (method === 'post' && url === '/api/v1/entities/merge') { transport.targets = transport.targets.filter((value: any) => value.target_id !== body.merge_entity_id); transport.targets.push(follow('entity', body.keep_entity_id, 'Retained entity')); return envelope(null) }
    if (method === 'get' && url.startsWith('/api/v1/knowledge-bases/kb-follows/entries?')) return { ...envelope([{ ...transport.articles[0], id: 'new', file_name: 'Article new', type: 'manual', parse_status: 'completed', updated_at: openedAt }]), total: 1 }
    if (method === 'get' && url.startsWith('/api/v1/chunks/new?')) return { ...envelope([]), total: 0 }
    if (method === 'get' && url === '/api/v1/entries/new') return envelope({ ...transport.articles[0], id: 'new', title: 'Article new', file_name: 'Article new', updated_at: openedAt, type: 'manual', parse_status: 'completed', structured_summary: transport.articles[0].summary, source: 'https://example.com/new', md: [], total: 0, metadata: {}, source_kind: 'article' })
    throw new Error(`Unexpected request ${method} ${url}`)
  }
  const requestModule = Object.fromEntries(['get', 'post', 'put', 'del'].map(method => [method, (url: string, body?: any) => request(method, url, body)]))
  const precision = evaluateFile('../../utils/rfc3339.ts', {})
  const followsAPI = evaluateFile('../../api/follows.ts', { '@/utils/request': requestModule })
  const topicsAPI = evaluateFile('../../api/topics.ts', { '@/utils/request': requestModule })
  const entitiesAPI = evaluateFile('../../api/entities.ts', { '@/utils/request': requestModule, './entity-types': { ENTITY_TYPES } })
  const entryAPI = evaluateFile('../../api/knowledge-base.ts', { '../utils/request': requestModule })
  const storeModule = evaluateFile('../../stores/follows.ts', { '@/api/follows': followsAPI, '@/utils/rfc3339': precision })
  const pinia = createPinia(); setActivePinia(pinia)
  const store = storeModule.useFollowStore(pinia)
  const route = reactive({ path: '/platform/follows', name: 'follows', query: {} as Record<string, any> })
  const urls: any[] = []
  const router = { replace: async (value: any) => { urls.push(value); route.query = value.query || {} }, push: async (value: any) => { urls.push(value) } }
  const i18n = createI18n({ legacy: false, locale, messages })
  const modules: Record<string, any> = { '@/utils/rfc3339': precision, '@/api/follows': followsAPI, '@/api/topics': topicsAPI, '@/api/entities': entitiesAPI, '@/api/knowledge-base': entryAPI, '@/stores/follows': storeModule, '@/types/entryClassification': { CONTENT_TYPES }, '@/utils/security': { isValidURL: (value: string) => /^https?:\/\//.test(value), hydrateProtectedFileImages: async () => {}, safeMarkdownToHTML: (value: string) => value, sanitizeHTML: (value: string) => value }, 'vue-router': { useRoute: () => route, useRouter: () => router }, 'tdesign-vue-next': { MessagePlugin: { success() {}, warning() {}, error() {} } }, './GlobalCommandPalette/useHighlight': highlight, '@/components/SafeTextarea.vue': empty, './MergeSuggestions.vue': empty }
  modules['@/utils/entrySourceLabels'] = entrySourceLabels
  const toggle = loadComponent(new URL('../../components/FollowToggle.vue', import.meta.url), modules)
  modules['@/components/FollowToggle.vue'] = modules['./FollowToggle.vue'] = moduleOf(toggle)
  const classification = loadComponent(new URL('../../components/EntryClassification.vue', import.meta.url), modules)
  modules['@/components/EntryClassification.vue'] = modules['./EntryClassification.vue'] = moduleOf(classification)
  modules['./SearchImage.vue'] = empty
  const result = loadComponent(new URL('../../components/SearchEntry.vue', import.meta.url), modules)
  modules['@/components/SearchEntry.vue'] = moduleOf(result)
  function mount(component: any, props = {}) {
    const root = element('root'), app = renderer.createApp(component, props)
    app.config.warnHandler = warning => warnings.push(warning)
    app.use(pinia); app.use(i18n); registerPrimitives(app)
    const passthrough = defineComponent({ props: ['message', 'description', 'text'], setup: (props, { attrs, slots }) => () => h('div', attrs, [props.message, props.description, props.text, slots.default?.(), slots.operation?.()]) })
    for (const name of ['t-alert', 't-loading', 't-empty']) app._context.components[name] = passthrough
    for (const name of ['t-select', 't-switch']) app._context.components[name] = defineComponent({ props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue', 'change'], setup: (props, { attrs, emit }) => () => h(name, { ...attrs, disabled: props.disabled, value: props.modelValue, options: props.options, 'onUpdate:modelValue': (value: any) => emit('update:modelValue', value), onChange: (value: any) => emit('change', value) }) })
    app.component('router-link', defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { href: props.to }, slots.default?.()) }))
    app.config.errorHandler = cause => errors.push(cause)
    app.config.warnHandler = warning => warnings.push(warning)
    app.mount(root)
    return { root, app, close: () => app.unmount() }
  }
  const component = (path: string) => loadComponent(new URL(path, import.meta.url), modules)
  const button = (root: Element, text: string) => descendants(root).find(node => node.tag === 'button' && visibleText(node).trim() === text)!
  const followButton = (root: Element, type: 'entity' | 'topic', following = false) => descendants(root).find(node => node.tag === 'button' && node.props['aria-label'] === i18n.global.t(following ? 'follows.unfollowNamed' : 'follows.followNamed', { name: type === 'entity' ? entity.canonical_name : topic.name }))!
  const change = async (node: Element, value: any) => { node.props['onUpdate:modelValue'](value); node.props.onChange(value); await settle() }
  return { transport, state, calls, envelope, store, modules, route, urls, component, mount, button, followButton, change, i18n, errors, warnings }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: follow feed markers use API response fields and refresh without article-state requests`, async () => {
    const env = environment(locale, [entityFollow, topicFollow])
    const markedArticles = env.transport.articles.slice(0, 2)
    for (const item of markedArticles) Object.assign(item, { acquisition: 'model_written', incomplete: true })
    const mounted = env.mount(env.component('./FollowFeed.vue'))
    try {
      for (const flagged of [true, false, undefined]) {
        if (flagged === true) await settle()
        else {
          for (const item of markedArticles) {
            Object.assign(item, { acquisition: 'extracted', incomplete: flagged })
            if (flagged === undefined) delete (item as any).incomplete
          }
          await click(env.button(mounted.root, messages[locale].follows.refresh))
        }
        const disclosure = descendants(mounted.root).find(node => node.tag === 'button' && hasClass(node, 'similar-toggle'))!
        if (!disclosure.props['aria-expanded']) await click(disclosure)
        for (const id of ['new', 'similar', 'old']) {
          const result = descendants(mounted.root).find(node => node.props['data-article-id'] === id)!
          assert.ok(result, `loaded feed article: ${id}`)
          const tags = descendants(result).find(node => hasClass(node, 'classification-tags'))!
          for (const [label, hint] of [
            [messages[locale].ingestStatus.modelWritten, messages[locale].ingestStatus.modelWrittenHint],
            [messages[locale].ingestStatus.incomplete, messages[locale].ingestStatus.incompleteHint],
          ]) {
            const marker = tags.children.find(node => visibleText(node) === label)
            assert.equal(Boolean(marker), id !== 'old' && Boolean(flagged), `${id}: ${label}`)
            if (marker) assert.equal(marker.props.title, hint, `${id}: ${label}`)
          }
        }
      }
      assert.ok(!env.calls.some(call => call.method === 'get' && call.url.startsWith('/api/v1/entries/')))
      assert.deepEqual(env.errors, [])
    } finally { mounted.close() }
  })
  test(`${locale}: entity and topic follow buttons load through their actual APIs and shared store`, async () => {
    const env = environment(locale)
    const workbench = env.mount(env.component('./components/EntryWorkbench.vue'))
    try {
      await settle()
      await click(descendants(workbench.root).find(node => node.tag === 'button' && hasClass(node, 'entity-list__item'))!)
      const followButton = env.followButton(workbench.root, 'entity')
      assert.ok(followButton)
      await click(followButton)
      assert.equal(env.store.isFollowing('entity', entity.id), true)
      await click(env.followButton(workbench.root, 'entity', true))
      assert.equal(env.store.isFollowing('entity', entity.id), false)
      assert.ok(env.calls.some(call => call.url === `/api/v1/entities/${entity.id}`))
    } finally { workbench.close() }
    const topics = env.mount(env.component('./TopicManagement.vue'))
    try {
      await settle()
      await click(env.followButton(topics.root, 'topic'))
      assert.equal(env.store.isFollowing('topic', topic.id), true)
      await click(env.followButton(topics.root, 'topic', true))
      assert.equal(env.store.isFollowing('topic', topic.id), false)
      assert.ok(env.calls.some(call => call.url === '/api/v1/topics'))
      assert.deepEqual(env.calls.filter(call => ['put', 'del'].includes(call.method) && call.url.startsWith('/api/v1/follows/')).map(call => [call.method, call.url]), [['put', `/api/v1/follows/entity/${entity.id}`], ['del', `/api/v1/follows/entity/${entity.id}`], ['put', `/api/v1/follows/topic/${topic.id}`], ['del', `/api/v1/follows/topic/${topic.id}`]])
      assert.deepEqual(env.errors, [])
    } finally { topics.close() }
  })
  test(`${locale}: article detail labels expose both follow buttons after an actual article load`, async () => {
    const env = environment(locale)
    const oldWindow = globalThis.window, oldDocument = globalThis.document, oldStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
    globalThis.window = { innerWidth: 1200, addEventListener() {}, removeEventListener() {} } as any
    globalThis.document = { body: { style: {} }, addEventListener() {}, removeEventListener() {}, querySelector: () => null } as any
    Object.defineProperty(globalThis, 'localStorage', { value: { getItem: () => null, setItem() {} }, configurable: true })
    Object.assign(env.modules, { marked: { marked: { use() {}, Renderer: class {}, parse: (value: string) => value } }, 'marked-katex-extension': moduleOf(() => ({})), 'katex/dist/katex.min.css': {}, 'highlight.js': moduleOf({}), 'highlight.js/styles/github.css': {}, mermaid: moduleOf({ initialize() {} }), '@/utils/markdownTableNormalize': { normalizeSpuriousTablePrefixes: (value: string) => value }, '@/utils/mermaidViewer': {}, '@/components/DocumentPreview.vue': empty, '@/components/ReviewActions.vue': empty, '@/components/EntryArticleJudgments.vue': empty, '@/components/PersonalDocumentDelete.vue': empty, '@/components/EntryProcessingTimeline.vue': empty, '@/views/knowledge-base/components/RepostSources.vue': empty, '@/views/knowledge-base/entryDownloadFileName': {}, '@/utils/filePreview': {}, '@/utils/api-base': {}, '@/utils/clipboard': {} })
    Object.assign(env.modules, { '@/components/MissingMaterials.vue': empty, '@/components/IncompleteActions.vue': empty })
    const detail = env.component('../../components/DocContent.vue')
    const entryStoreModule = evaluateFile('../../stores/entry.ts', {})
    const detailHook = evaluateFile('../../composables/useKnowledgeBase.ts', { '../utils/index': { formatStringDate: () => '2026-10-11' }, 'tdesign-vue-next': env.modules['tdesign-vue-next'], '@/api/knowledge-base': env.modules['@/api/knowledge-base'], '@/stores/entry': entryStoreModule, '@/stores/resources': { useResourcesStore: () => ({}) } })
    const parent = defineComponent({ setup() { const hook = detailHook.default('kb-follows'); onMounted(async () => { await hook.loadEntries(); hook.getCardDetails(hook.cardList.value[0]) }); return () => hook.details.id ? h(detail, { visible: true, details: hook.details }) : null } })
    const mounted = env.mount(parent)
    try {
      await settle()
      assert.ok(env.calls.some(call => call.method === 'get' && call.url === '/api/v1/entries/new'))
      assert.ok(env.calls.some(call => call.url === '/api/v1/knowledge-bases/kb-follows/entries?page=1&page_size=35'))
      assert.ok(env.calls.some(call => call.url.startsWith('/api/v1/chunks/new?')))
      assert.equal(entryStoreModule.entryStore().cardList[0].id, 'new')
      await click(env.followButton(mounted.root, 'entity'))
      await click(env.followButton(mounted.root, 'topic'))
      assert.equal(env.store.isFollowing('entity', entity.id), true)
      assert.equal(env.store.isFollowing('topic', topic.id), true)
      await click(env.followButton(mounted.root, 'entity', true))
      await click(env.followButton(mounted.root, 'topic', true))
      assert.equal(env.store.targets.length, 0)
      assert.deepEqual(env.errors, [])
    } finally { mounted.close(); globalThis.window = oldWindow; globalThis.document = oldDocument; if (oldStorage) Object.defineProperty(globalThis, 'localStorage', oldStorage); else delete (globalThis as any).localStorage }
  })
  test(`${locale}: feed loads history, preserves new markers across filters and refresh, unfolds matches and marks all seen`, async () => {
    const env = environment(locale, [entityFollow, topicFollow]), mounted = env.mount(env.component('./FollowFeed.vue'))
    try {
      await settle()
      assert.ok(visibleText(mounted.root).includes('Article old'))
      assert.equal(descendants(mounted.root).filter(node => hasClass(node, 'feed-new')).length, 1)
      assert.equal(env.store.newCount, 0)
      assert.deepEqual(env.calls.filter(call => call.url.endsWith('/follows/view')).map(call => call.body), [{ viewed_at: openedAt }])
      await click(descendants(mounted.root).find(node => node.tag === 'button' && hasClass(node, 'similar-toggle'))!)
      assert.ok(visibleText(mounted.root).includes('Article similar'))
      assert.equal(descendants(mounted.root).filter(node => hasClass(node, 'feed-new')).length, 2)
      assert.ok(visibleText(mounted.root).includes(`${topic.name} (${messages[locale].follows.targetTypes.topic})`))
      await click(env.button(mounted.root, messages[locale].follows.refresh))
      await click(descendants(mounted.root).find(node => node.tag === 'button' && hasClass(node, 'similar-toggle'))!)
      assert.equal(descendants(mounted.root).filter(node => hasClass(node, 'feed-new')).length, 2)
      await env.change(descendants(mounted.root).find(node => node.tag === 't-select' && node.props['aria-label'] === messages[locale].follows.filterTarget)!, [topic.id])
      assert.ok(visibleText(mounted.root).includes('Article new'))
      assert.ok(!visibleText(mounted.root).includes('Article old'))
      await env.change(descendants(mounted.root).find(node => node.tag === 't-switch')!, false)
      assert.ok(visibleText(mounted.root).includes('Article similar'))
      assert.equal(descendants(mounted.root).filter(node => hasClass(node, 'feed-new')).length, 2)
      const feedCalls = env.calls.filter(call => call.url.endsWith('/follows/feed'))
      assert.ok(feedCalls.every(call => call.body.include_seen === true && call.body.since === undefined))
      assert.equal(feedCalls.at(-1)!.body.collapse_similar, false)
      assert.deepEqual(feedCalls.at(-1)!.body.target_ids, [topic.id])
      assert.equal(env.calls.filter(call => call.url.endsWith('/follows/view')).length, 1)
      await click(env.button(mounted.root, messages[locale].follows.markAllViewed))
      assert.deepEqual(env.calls.filter(call => call.url.endsWith('/follows/view')).at(-1)!.body, {})
      assert.equal(descendants(mounted.root).filter(node => hasClass(node, 'feed-new')).length, 0)
      assert.equal(env.store.lastViewedAt, markedAt)
      assert.deepEqual(env.errors, [])
    } finally { mounted.close() }
  })
  test(`${locale}: sidebar badge follows the actual state API and shared viewing state`, async () => {
    const env = environment(locale, [entityFollow, topicFollow])
    const menuStore = evaluateFile('../../stores/menu.ts', { '@/i18n': moduleOf(env.i18n) })
    env.modules['@/stores/menu'] = menuStore
    env.modules['@/stores/ui'] = { useUIStore: () => reactive({ sidebarCollapsed: false, toggleSidebar() {}, openSettings() {} }) }
    env.modules['@/components/UserMenu.vue'] = empty
    env.modules['@/assets/img/reliquary.svg'] = moduleOf('/reliquary.svg')
    const mounted = env.mount(env.component('../../components/SideMenu.vue'))
    try {
      await settle()
      const navigation = descendants(mounted.root).filter(node => node.tag === 'button' && hasClass(node, 'menu_item'))
      assert.deepEqual(navigation.map(visibleText), [messages[locale].menu.knowledgeBase, messages[locale].menu.search, messages[locale].knowledgeWorkbench.title, messages[locale].topicManagement.title, `${messages[locale].follows.title}2`, messages[locale].menu.settings])
      assert.ok(env.calls.some(call => call.url === '/api/v1/follows/state'))
      assert.equal(descendants(mounted.root).find(node => hasClass(node, 'follow-badge'))!.text, '2')
      await env.store.markViewed(openedAt); await settle()
      assert.equal(descendants(mounted.root).filter(node => hasClass(node, 'follow-badge')).length, 0)
      assert.deepEqual(env.errors, [])
    } finally { mounted.close() }
  })
}

test('failed follow updates retain the confirmed state and offer a readable retry', async () => {
  const env = environment('zh-CN'), mounted = env.mount(env.modules['@/components/FollowToggle.vue'].default, { targetType: 'entity', targetId: entity.id, name: entity.canonical_name })
  try {
    await settle()
    env.transport.fail = method => method === 'put'
    await click(env.followButton(mounted.root, 'entity'))
    assert.equal(env.store.isFollowing('entity', entity.id), false)
    assert.ok(visibleText(mounted.root).includes(messages['zh-CN'].follows.saveFailed))
    env.transport.fail = () => false
    await click(env.followButton(mounted.root, 'entity'))
    assert.equal(env.store.isFollowing('entity', entity.id), true)
    assert.deepEqual(env.errors, [])
  } finally { mounted.close() }
})

test('failed feed loads keep the viewing checkpoint, and retry uses the actual loading path', async () => {
  const env = environment('en-US', [entityFollow])
  env.transport.fail = (_method, url) => url.endsWith('/follows/feed')
  const mounted = env.mount(env.component('./FollowFeed.vue'))
  try {
    await settle()
    assert.ok(visibleText(mounted.root).includes(messages['en-US'].follows.feedFailed))
    assert.equal(env.transport.checkpoint, previous)
    assert.equal(env.calls.filter(call => call.url.endsWith('/follows/view')).length, 0)
    env.transport.fail = () => false
    await click(env.button(mounted.root, messages['en-US'].common.retry))
    assert.ok(visibleText(mounted.root).includes('Article new'))
    assert.equal(env.transport.checkpoint, openedAt)
    assert.deepEqual(env.errors, [])
  } finally { mounted.close() }
})

test('no follows shows entity and topic destinations after the real feed load', async () => {
  const env = environment('zh-CN'), mounted = env.mount(env.component('./FollowFeed.vue'))
  try {
    await settle()
    assert.ok(visibleText(mounted.root).includes(messages['zh-CN'].follows.emptyTargets))
    assert.deepEqual(descendants(mounted.root).filter(node => node.tag === 'a').map(node => node.props.href), ['/platform/entries?tab=entities', '/platform/topics'])
    assert.ok(env.calls.some(call => call.url.endsWith('/follows/feed')))
    assert.deepEqual(env.errors, [])
  } finally { mounted.close() }
})

test('mark all retry preserves its action after new articles arrive', async () => {
  const env = environment('en-US', [entityFollow, topicFollow]), mounted = env.mount(env.component('./FollowFeed.vue'))
  try {
    await settle()
    env.transport.articles.unshift(article('arriving', '2026-10-11T13:00:00Z'))
    await env.store.refreshState()
    assert.equal(env.store.newCount, 1)
    env.transport.fail = (method, url, body) => method === 'post' && url.endsWith('/follows/view') && !body.viewed_at
    await click(env.button(mounted.root, messages['en-US'].follows.markAllViewed))
    assert.ok(visibleText(mounted.root).includes(messages['en-US'].follows.markFailed))
    assert.equal(env.store.newCount, 1)
    env.transport.fail = () => false
    await click(env.button(mounted.root, messages['en-US'].common.retry))
    assert.deepEqual(env.calls.filter(call => call.url.endsWith('/follows/view')).map(call => call.body), [{ viewed_at: openedAt }, {}, {}])
    assert.equal(env.store.newCount, 0)
    assert.ok(visibleText(mounted.root).includes('Article arriving'))
    assert.equal(descendants(mounted.root).filter(node => hasClass(node, 'feed-new')).length, 0)
    assert.deepEqual(env.errors, [])
  } finally { mounted.close() }
})

test('a query change during initial loading invalidates the older feed response', async () => {
  const env = environment('en-US', [entityFollow, topicFollow]), stale = deferred<any>()
  let first = true
  env.transport.intercept = (_method, url) => { if (url.endsWith('/follows/feed') && first) { first = false; return stale.promise }; return undefined }
  const mounted = env.mount(env.component('./FollowFeed.vue'))
  try {
    await settle()
    env.route.query = { target_ids: topic.id }
    await settle()
    assert.ok(visibleText(mounted.root).includes('Article new'))
    assert.ok(!visibleText(mounted.root).includes('Article old'))
    stale.resolve(env.envelope({ results: [article('stale', openedAt)], last_viewed_at: previous, viewed_at: openedAt, new_count: 1, total: 1 }))
    await settle()
    assert.ok(!visibleText(mounted.root).includes('Article stale'))
    assert.deepEqual(env.calls.filter(call => call.url.endsWith('/follows/view')).map(call => call.body), [{ viewed_at: openedAt }])
    assert.deepEqual(env.errors, [])
  } finally { mounted.close() }
})

test('bounded feed lists report all candidate articles and offer supported limits', async () => {
  const env = environment('en-US', [entityFollow])
  env.transport.articles = Array.from({ length: 105 }, (_, index) => article(`item-${index}`, '2026-10-11T10:00:00Z', [entityFollow]))
  const mounted = env.mount(env.component('./FollowFeed.vue'))
  try {
    await settle()
    assert.ok(visibleText(mounted.root).includes('Showing 20 of 105 articles.'))
    const limitControl = descendants(mounted.root).find(node => node.tag === 't-select' && node.props['aria-label'] === messages['en-US'].follows.limit)!
    assert.deepEqual(limitControl.props.options.map((option: any) => option.value), [20, 50, 100])
    await env.change(limitControl, 100)
    assert.ok(visibleText(mounted.root).includes('Showing 100 of 105 articles.'))
    assert.equal(env.calls.filter(call => call.url.endsWith('/follows/view')).length, 1)
    assert.deepEqual(env.errors, [])
  } finally { mounted.close() }
})

test('same millisecond timestamps retain precise new markers after refresh', async () => {
  const env = environment('en-US', [entityFollow])
  env.transport.checkpoint = '2026-10-11T10:00:00.123400Z'
  env.transport.articles = [article('micro-new', '2026-10-11T10:00:00.123401Z', [entityFollow]), article('micro-equal', '2026-10-11T10:00:00.123400Z', [entityFollow]), article('micro-old', '2026-10-11T10:00:00.123399Z', [entityFollow])]
  const mounted = env.mount(env.component('./FollowFeed.vue'))
  try {
    await settle()
    assert.equal(descendants(mounted.root).filter(node => hasClass(node, 'feed-new')).length, 1)
    await click(env.button(mounted.root, messages['en-US'].follows.refresh))
    assert.equal(descendants(mounted.root).filter(node => hasClass(node, 'feed-new')).length, 1)
    assert.deepEqual(env.errors, [])
  } finally { mounted.close() }
})

test('topic deletion refreshes shared follows and unread count immediately', async () => {
  const env = environment('en-US', [entityFollow, topicFollow]), mounted = env.mount(env.component('./TopicManagement.vue'))
  try {
    await settle(); await env.store.refreshState()
    assert.equal(env.store.newCount, 2)
    await click(env.button(mounted.root, messages['en-US'].common.delete))
    const confirmation = descendants(mounted.root).find(node => typeof node.props.onConfirm === 'function')!
    assert.ok(confirmation)
    confirmation.props.onConfirm(); await settle()
    assert.equal(env.store.isFollowing('topic', topic.id), false)
    assert.equal(env.store.isFollowing('entity', entity.id), true)
    assert.equal(env.store.newCount, 1)
    assert.ok(env.calls.some(call => call.method === 'del' && call.url === `/api/v1/topics/${topic.id}`))
    assert.deepEqual(env.errors, [])
  } finally { mounted.close() }
})

test('entity merge refreshes the shared target list with the retained entity', async () => {
  const env = environment('en-US', [entityFollow, topicFollow]), mounted = env.mount(env.component('./components/EntryWorkbench.vue'))
  try {
    await settle()
    await click(descendants(mounted.root).find(node => node.tag === 'button' && hasClass(node, 'entity-list__item'))!)
    await click(env.button(mounted.root, messages['en-US'].knowledgeWorkbench.entities.merge))
    const targetControl = descendants(mounted.root).find(node => node.tag === 't-select' && node.props['aria-label'] === messages['en-US'].knowledgeWorkbench.entities.mergeTarget)!
    await env.change(targetControl, 'entity-retained')
    await click(descendants(mounted.root).filter(node => node.tag === 'button' && visibleText(node).trim() === messages['en-US'].knowledgeWorkbench.entities.merge).at(-1)!)
    assert.equal(env.store.isFollowing('entity', entity.id), false)
    assert.equal(env.store.isFollowing('entity', 'entity-retained'), true)
    assert.equal(env.store.isFollowing('topic', topic.id), true)
    assert.ok(env.calls.some(call => call.method === 'post' && call.url === '/api/v1/entities/merge'))
    assert.deepEqual(env.errors, [])
  } finally { mounted.close() }
})

test('late completed and newly related old articles load as new while topic history stays seen', async () => {
  const env = environment('en-US', [entityFollow, topicFollow])
  const completed = article('late-completed', '2026-10-09T00:00:00Z', [entityFollow])
  const recognized = article('late-recognized', '2026-10-08T00:00:00Z', [entityFollow])
  const historical = article('topic-history', '2026-10-07T00:00:00Z', [topicFollow])
  completed.entered_at = '2026-10-11T10:00:00Z'
  recognized.entered_at = '2026-10-11T11:00:00Z'
  historical.entered_at = null
  env.transport.articles = [completed, recognized, historical]
  await env.store.refreshState()
  assert.equal(env.store.newCount, 2)
  const mounted = env.mount(env.component('./FollowFeed.vue'))
  try {
    await settle()
    assert.ok(visibleText(mounted.root).includes('Article topic-history'))
    assert.equal(descendants(mounted.root).filter(node => hasClass(node, 'feed-new')).length, 2)
    await click(env.button(mounted.root, messages['en-US'].follows.refresh))
    assert.equal(descendants(mounted.root).filter(node => hasClass(node, 'feed-new')).length, 2)
    await click(env.button(mounted.root, messages['en-US'].follows.markAllViewed))
    assert.equal(descendants(mounted.root).filter(node => hasClass(node, 'feed-new')).length, 0)
    assert.deepEqual(env.errors, [])
  } finally { mounted.close() }
})


for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: feed toolbar loads controls in separate fields with complete labels`, async () => {
    const env = environment(locale, [entityFollow, topicFollow])
    const mounted = env.mount(env.component('./FollowFeed.vue'))
    try {
      await settle()
      const toolbar = descendants(mounted.root).find(node => hasClass(node, 'feed-toolbar'))!
      const fields = toolbar.children.filter(node => hasClass(node, 'feed-filter-field'))
      assert.equal(fields.length, 3)
      const labels = [messages[locale].follows.filterTarget, messages[locale].follows.limit, messages[locale].knowledgeSearch.collapseSimilar]
      const tags = ['t-select', 't-select', 't-switch']
      fields.forEach((field, index) => {
        assert.equal(field.tag, 'label')
        assert.equal(field.parent, toolbar)
        assert.equal(field.children[0].tag, 'span')
        assert.equal(visibleText(field.children[0]), labels[index])
        const controls = descendants(field).filter(node => ['t-select', 't-switch'].includes(node.tag))
        assert.equal(controls.length, 1)
        assert.equal(controls[0].tag, tags[index])
        assert.equal(controls[0].props['aria-label'], labels[index])
        assert.equal(controls[0].parent, field)
      })
      const refresh = toolbar.children.find(node => hasClass(node, 'feed-refresh'))!
      assert.equal(refresh.parent, toolbar)
      assert.equal(visibleText(refresh), messages[locale].follows.refresh)
      assert.ok(env.calls.some(call => call.url.endsWith('/follows/feed')))
      assert.deepEqual(env.errors, [])
    } finally { mounted.close() }
  })
}

test('feed toolbar compiled styles use the search grid layout and stack fields on narrow screens', async () => {
  const filename = new URL('./FollowFeed.vue', import.meta.url).pathname
  const { descriptor } = parse(readFileSync(filename, 'utf8'), { filename })
  const compiled = await compileStyleAsync({ filename, id: 'data-v-feed-toolbar', source: descriptor.styles[0].content, scoped: true, preprocessLang: 'less' })
  assert.deepEqual(compiled.errors, [])
  const css = require('postcss').parse(compiled.code)
  const declarations = (selector: string, media?: string) => {
    const values: Record<string, string> = {}
    css.walkRules((rule: any) => {
      if (rule.selector !== selector) return
      if (media ? rule.parent.type !== 'atrule' || rule.parent.params !== media : rule.parent.type === 'atrule') return
      rule.walkDecls((decl: any) => { values[decl.prop] = decl.value })
    })
    return values
  }
  const toolbar = declarations('.feed-toolbar[data-v-feed-toolbar]')
  assert.equal(toolbar.display, 'grid')
  assert.equal(toolbar['grid-template-columns'], 'repeat(2, minmax(0, 1fr))')
  assert.equal(toolbar.gap, '14px 16px')
  const field = declarations('.feed-filter-field[data-v-feed-toolbar]')
  assert.equal(field.display, 'flex')
  assert.equal(field['flex-direction'], 'column')
  assert.equal(field['min-width'], '0')
  assert.equal(field['overflow-wrap'], 'anywhere')
  assert.equal(declarations('.feed-filter-field[data-v-feed-toolbar] .t-select__wrap').width, '100%')
  assert.equal(declarations('.fold-control[data-v-feed-toolbar]')['align-items'], 'flex-start')
  assert.equal(declarations('.feed-toolbar[data-v-feed-toolbar]', '(max-width: 560px)')['grid-template-columns'], 'minmax(0, 1fr)')
})
