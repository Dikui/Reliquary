import assert from 'node:assert/strict'
import * as entrySourceLabels from '../../utils/entrySourceLabels'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { defineComponent, h, onMounted, reactive } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'

import * as highlight from '../../components/GlobalCommandPalette/useHighlight'
import * as markdownPlainText from '../../utils/markdownPlainText'
import { click as clickNode, descendants, element, hasClass, loadComponent, registerPrimitives, renderer, settle as settleNodes, visibleText, type Element } from './componentTestHarness'

const require = createRequire(import.meta.url), messages = { 'zh-CN': zh, 'en-US': en }
const moduleOf = (value: unknown) => ({ __esModule: true, default: value })
async function settle() { await settleNodes(); await new Promise<void>(resolve => setImmediate(resolve)); await settleNodes() }
async function click(node: Element) { await clickNode(node); await settle() }
const empty = moduleOf(defineComponent({ setup: () => () => null }))
const entity = { id: 'entity-a', canonical_name: 'Aurora', aliases: ['Aurora Labs'], created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z' }
const secondEntity = { id: 'entity-b', name: 'Space Labs', description: 'Space missions', entry_count: 2, created_at: '', updated_at: '' }
const follow = (type: 'entity', id: string, name: string) => ({ target_type: type, target_id: id, name, created_at: '2026-10-01T00:00:00Z' })
const entityFollow = follow('entity', entity.id, entity.canonical_name), secondEntityFollow = follow('entity', secondEntity.id, secondEntity.name)
const openedAt = '2026-10-11T12:00:00Z'
const article = (id: string, collected: string) => ({ entry_id: id, title: `Article ${id}`, source: 'article', published_at: collected, processing_mode: 'summary_only', summary: { overview: `Overview ${id}`, points: ['A useful point'], key_terms: ['Aurora'] }, entities: [entity],  snippets: [], score: 0,  vault_path: `articles/2026-10/${id}.md`, original_url: `https://example.com/${id}` })
function evaluateFile(path: string, modules: Record<string, unknown>) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8')
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const value = { exports: {} as any }
  new Function('require', 'module', 'exports', code)((name: string) => (name === './BodyReference.vue' ? { __esModule: true, default: defineComponent({ setup: () => () => null }) } : modules[name]) ?? require(name), value, value.exports)
  return value.exports
}

function environment(locale: keyof typeof messages, initialTargets: any[] = []) {
  const calls: { method: string; url: string; body?: any }[] = [], warnings: string[] = [], errors: unknown[] = []
  const transport = {
    targets: structuredClone(initialTargets),
    articles: [article('new', '2026-10-11T10:00:00Z')],
    fail: (_method: string, _url: string, _body?: any) => false,
    intercept: (_method: string, _url: string, _body?: any): Promise<any> | undefined => undefined,
  }
  const envelope = (data: any) => ({ success: true, data: structuredClone(data) })
  async function request(method: string, url: string, body?: any) {
    calls.push({ method, url, ...(body !== undefined ? { body: structuredClone(body) } : {}) })
    const delayed = transport.intercept(method, url, body)
    if (delayed) return delayed
    if (transport.fail(method, url, body)) throw new Error('transport unavailable')
    if (method === 'get' && url === '/api/v1/follows') return envelope(transport.targets)
    const target = url.match(/^\/api\/v1\/follows\/(entity)\/(.+)$/)
    if (target && ['put', 'del'].includes(method)) {
      const [type, id] = [target[1], decodeURIComponent(target[2])]
      const value = follow(type as any, id, type === 'entity' ? entity.canonical_name : secondEntity.name)
      transport.targets = transport.targets.filter((entry: any) => entry.target_type !== type || entry.target_id !== id)
      if (method === 'put') transport.targets.push(value)
      return envelope(method === 'put' ? value : null)
    }
    if (method === 'get' && url.startsWith('/api/v1/entities?')) return envelope([entity])
    if (method === 'get' && url === `/api/v1/entities/${entity.id}`) return envelope({ entity, entry: [{ id: 'new', title: 'Article new', created_at: openedAt }] })
    if (method === 'post' && url === '/api/v1/entities/merge') { transport.targets = transport.targets.filter((value: any) => value.target_id !== body.merge_entity_id); transport.targets.push(follow('entity', body.keep_entity_id, 'Retained entity')); return envelope(null) }
    if (method === 'get' && url.startsWith('/api/v1/entries?')) return { ...envelope([{ ...transport.articles[0], id: 'new', file_name: 'Article new', type: 'manual', parse_status: 'completed', updated_at: openedAt }]), total: 1 }
    if (method === 'get' && url.startsWith('/api/v1/chunks/new?')) return { ...envelope([]), total: 0 }
    if (method === 'get' && url === '/api/v1/entries/new') return envelope({ ...transport.articles[0], id: 'new', title: 'Article new', file_name: 'Article new', updated_at: openedAt, type: 'manual', parse_status: 'completed', structured_summary: transport.articles[0].summary, source: 'https://example.com/new', md: [], total: 0, metadata: {}, source_kind: 'article' })
    throw new Error(`Unexpected request ${method} ${url}`)
  }
  const requestModule = Object.fromEntries(['get', 'post', 'put', 'del'].map(method => [method, (url: string, body?: any) => request(method, url, body)]))
  const followsAPI = evaluateFile('../../api/follows.ts', { '@/utils/request': requestModule })

  const entitiesAPI = evaluateFile('../../api/entities.ts', { '@/utils/request': requestModule })
  const entryAPI = evaluateFile('../../api/library.ts', { '../utils/request': requestModule })
  const storeModule = evaluateFile('../../stores/follows.ts', { '@/api/follows': followsAPI })
  const pinia = createPinia(); setActivePinia(pinia)
  const store = storeModule.useFollowStore(pinia)
  const route = reactive({ path: '/platform/follows', name: 'follows', query: {} as Record<string, any> })
  const urls: any[] = []
  const router = { replace: async (value: any) => { urls.push(value); route.query = value.query || {} }, push: async (value: any) => { urls.push(value) } }
  const i18n = createI18n({ legacy: false, locale, messages })
  const modules: Record<string, any> = { '@/api/follows': followsAPI,  '@/api/entities': entitiesAPI, '@/api/library': entryAPI, '@/stores/follows': storeModule, '@/types/entryClassification': { }, '@/utils/security': { createProtectedFileImageScope: () => ({ signal: new AbortController().signal, dispose() {} }), isValidURL: (value: string) => /^https?:\/\//.test(value), hydrateProtectedFileImages: async () => {}, safeMarkdownToHTML: (value: string) => value, sanitizeHTML: (value: string) => value }, 'vue-router': { useRoute: () => route, useRouter: () => router }, 'tdesign-vue-next': { MessagePlugin: { success() {}, warning() {}, error() {} } }, './GlobalCommandPalette/useHighlight': highlight, '@/components/SafeTextarea.vue': empty, './EntityEditor.vue': empty, './EntityActivity.vue': empty }
  modules['@/components/EntryUpdateNotice.vue'] = modules['./EntryUpdateNotice.vue'] = empty
  modules['./BodyReference.vue'] = empty
  modules['@/components/EntryEntitiesEditor.vue'] = empty
  modules['@/components/EntryDomainsEditor.vue'] = empty
  modules['@/utils/entrySourceLabels'] = entrySourceLabels
  modules['@/utils/markdownPlainText'] = markdownPlainText
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
  const followButton = (root: Element, type: 'entity', following = false) => descendants(root).find(node => node.tag === 'button' && node.props['aria-label'] === i18n.global.t(following ? 'follows.unfollowNamed' : 'follows.followNamed', { name: type === 'entity' ? entity.canonical_name : secondEntity.name }))!
  const change = async (node: Element, value: any) => { node.props['onUpdate:modelValue'](value); node.props.onChange(value); await settle() }
  return { transport, calls, envelope, store, modules, route, urls, component, mount, button, followButton, change, i18n, errors, warnings }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: entity follow buttons load through their actual APIs and shared store`, async () => {
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

  })
  test(`${locale}: material detail labels expose the entity follow control after an actual load`, async () => {
    const env = environment(locale)
    const oldWindow = globalThis.window, oldDocument = globalThis.document, oldStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
    globalThis.window = { innerWidth: 1200, addEventListener() {}, removeEventListener() {} } as any
    globalThis.document = { body: { style: {} }, addEventListener() {}, removeEventListener() {}, querySelector: () => null } as any
    Object.defineProperty(globalThis, 'localStorage', { value: { getItem: () => null, setItem() {} }, configurable: true })
    Object.assign(env.modules, { marked: { marked: { use() {}, Renderer: class {}, parse: (value: string) => value } }, 'marked-katex-extension': moduleOf(() => ({})), 'katex/dist/katex.min.css': {}, 'highlight.js': moduleOf({}), 'highlight.js/styles/github.css': {}, mermaid: moduleOf({ initialize() {} }), '@/utils/markdownTableNormalize': { normalizeSpuriousTablePrefixes: (value: string) => value }, '@/utils/mermaidViewer': {}, '@/components/DocumentPreview.vue': empty, '@/components/ReviewActions.vue': empty, '@/components/EntryArticleJudgments.vue': empty, '@/components/EntryStarToggle.vue': empty, '@/components/ReportAssociations.vue': empty, '@/components/PersonalDocumentDelete.vue': empty, '@/components/EntryProcessingTimeline.vue': empty, '@/views/library/components/RepostSources.vue': empty, '@/views/library/entryDownloadFileName': {}, '@/utils/filePreview': {}, '@/utils/api-base': {}, '@/utils/clipboard': {} })
    Object.assign(env.modules, { '@/components/EntryProcessingStatus.vue': empty, '@/components/MissingMaterials.vue': empty, '@/components/IncompleteActions.vue': empty })
    const detail = env.component('../../components/DocContent.vue')
    const entryStoreModule = evaluateFile('../../stores/entry.ts', {})
    const detailHook = evaluateFile('../../composables/useLibrary.ts', { '../utils/index': { formatStringDate: () => '2026-10-11' }, 'tdesign-vue-next': env.modules['tdesign-vue-next'], '@/api/library': env.modules['@/api/library'], '@/stores/entry': entryStoreModule, '@/stores/resources': { useResourcesStore: () => ({}) } })
    const parent = defineComponent({ setup() { const hook = detailHook.default('library-follows'); onMounted(async () => { await hook.loadEntries(); hook.getCardDetails(hook.cardList.value[0]) }); return () => hook.details.id ? h(detail, { visible: true, details: hook.details }) : null } })
    const mounted = env.mount(parent)
    try {
      await settle()
      assert.ok(env.calls.some(call => call.method === 'get' && call.url === '/api/v1/entries/new'))
      assert.ok(env.calls.some(call => call.url === '/api/v1/entries?page=1&page_size=35'))
      assert.ok(env.calls.some(call => call.url.startsWith('/api/v1/chunks/new?')))
      assert.equal(entryStoreModule.entryStore().cardList[0].id, 'new')
      await click(env.followButton(mounted.root, 'entity'))
      assert.equal(env.store.isFollowing('entity', entity.id), true)
      await click(env.followButton(mounted.root, 'entity', true))
      assert.equal(env.store.targets.length, 0)
      assert.deepEqual(env.errors, [])
    } finally { mounted.close(); globalThis.window = oldWindow; globalThis.document = oldDocument; if (oldStorage) Object.defineProperty(globalThis, 'localStorage', oldStorage); else delete (globalThis as any).localStorage }
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

test('entity merge refreshes the shared target list with the retained entity', async () => {
  const env = environment('en-US', [entityFollow, secondEntityFollow]), mounted = env.mount(env.component('./components/EntryWorkbench.vue'))
  try {
    await settle()
    await click(descendants(mounted.root).find(node => node.tag === 'button' && hasClass(node, 'entity-list__item'))!)
    await click(env.button(mounted.root, messages['en-US'].knowledgeWorkbench.entities.merge))
    const targetControl = descendants(mounted.root).find(node => node.tag === 't-select' && node.props['aria-label'] === messages['en-US'].knowledgeWorkbench.entities.mergeTarget)!
    await env.change(targetControl, 'entity-retained')
    await click(descendants(mounted.root).filter(node => node.tag === 'button' && visibleText(node).trim() === messages['en-US'].knowledgeWorkbench.entities.merge).at(-1)!)
    assert.equal(env.store.isFollowing('entity', entity.id), false)
    assert.equal(env.store.isFollowing('entity', 'entity-retained'), true)
    assert.ok(env.calls.some(call => call.method === 'post' && call.url === '/api/v1/entities/merge'))
    assert.deepEqual(env.errors, [])
  } finally { mounted.close() }
})

test('entity related material is grouped by library with group counts', async () => {
  const env = environment('zh-CN')
  env.transport.intercept = (_method, url) => url === `/api/v1/entities/${entity.id}` ? Promise.resolve(env.envelope({ entity, entries: [
    { id: 'one-a', library_id: 'one', library_name: '机构发布', title: 'First', created_at: openedAt },
    { id: 'two', library_id: 'two', library_name: '人物观点', title: 'Second', created_at: openedAt },
    { id: 'one-b', library_id: 'one', library_name: '机构发布', title: 'Third', created_at: openedAt },
  ] })) : undefined
  const mounted = env.mount(env.component('./components/EntryWorkbench.vue'))
  try {
    await settle()
    await click(descendants(mounted.root).find(node => node.tag === 'button' && hasClass(node, 'entity-list__item'))!)
    const groups = descendants(mounted.root).filter(node => hasClass(node, 'entity-library-group'))
    assert.equal(groups.length, 2)
    assert.ok(visibleText(groups[0]!).includes('机构发布 (2)'))
    assert.ok(visibleText(groups[0]!).includes('Third'))
    assert.ok(visibleText(groups[1]!).includes('人物观点 (1)'))
    assert.deepEqual(env.errors, [])
  } finally { mounted.close() }
})
