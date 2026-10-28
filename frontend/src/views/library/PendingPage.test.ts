import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import { defineComponent, h, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import { loadUiComponent } from '../../components/ui/uiTestLoader'
import { click, descendants, element, loadComponent, renderer, settle, visibleText, type Element } from './componentTestHarness'
import { confirm as sharedConfirm, useFeedback, type ConfirmOptions } from '../../components/ui/feedback'
import * as reference from '../../utils/bodyReference'
import * as security from '../../utils/security'
import * as classification from '../../types/entryClassification'
import { ENTITY_TYPES } from '../../api/entity-types'

const suggestion = (id: string, status = 'pending') => ({ id, status, new_entity_id: `fresh-${id}`, candidate_entity_id: `existing-${id}`, new_entity_name: `Fresh ${id}`, candidate_entity_name: `Existing ${id}`, reason: `Reason ${id}`, entry_id: `entry-${id}`, created_at: '2026-10-01T00:00:00Z' })
const review = (id: string, url = 'https://example.com/original') => ({ id, title: `Review ${id}`, library_id: 'a', library_name: 'Library A', content_type: 'research', source_kind: 'article', metadata: { intake: { reason: 'Actual review reason', source_info: { original_url: url, author: 'Writer' } } } })
const find = (root: Element, tag: string, text: string) => descendants(root).find(node => node.tag === tag && visibleText(node).includes(text))!

function mount(t: TestContext, options: { merges?: any[]; reviews?: any[]; query?: Record<string, string>; language?: string; api?: Record<string, any> } = {}) {
  const data = { merges: options.merges ?? [suggestion('one'), suggestion('two')], reviews: options.reviews ?? [review('old')] }
  const calls = { decisions: [] as any[], confirmations: [] as ConfirmOptions[], notices: [] as string[], scopes: [] as any[], counts: 0 }
  const route = reactive({ query: { ...options.query }, hash: '' })
  const library = {
    listLibraries: async () => ({ success: true, data: [{ id: 'a', name: 'Library A', color: 1 }, { id: 'b', name: 'Library B', color: 2 }] }),
    getEntryEditableBody: async () => ({ content: 'Heading\n\nFresh one is supported by the stored source.', bodyHash: 'hash' }),
  }
  const entities = {
    ENTITY_TYPES,
    listEntityMergeSuggestions: async (query: any) => { calls.scopes.push(query); return { success: true, data: query.library_id === 'a' ? data.merges : [] } },
    getEntity: async (id: string) => ({ success: true, data: { entity: { id, canonical_name: id.startsWith('fresh') ? `Fresh ${id.slice(6)}` : `Existing ${id.slice(9)}`, entity_type: 'company', aliases: ['Alias'], description: 'Actual description', library_id: 'a' }, entries: [{ id: 'source-full-identifier', title: 'Source evidence' }] } }),
    acceptEntityMergeSuggestion: async (id: string) => { calls.decisions.push(['accept', id]); data.merges.find(item => item.id === id)!.status = 'accepted'; return { success: true } },
    rejectEntityMergeSuggestion: async (id: string) => { calls.decisions.push(['reject', id]); data.merges.find(item => item.id === id)!.status = 'rejected'; return { success: true } },
    ...options.api,
  }
  const reviews = {
    listPendingEntry: async (query: any) => { calls.scopes.push(query); return { success: true, data: data.reviews.slice((query.page - 1) * 20, query.page * 20), total: data.reviews.length } },
    resolveIntakeReview: async (id: string, input: any) => { calls.decisions.push([id, input]); data.reviews = data.reviews.filter(item => item.id !== id); return { success: true, data: undefined } },
    ...options.api,
  }
  const ui = Object.fromEntries(['Button', 'Input', 'Select', 'Tag', 'LibMark', 'EmptyState', 'ErrorState', 'Skeleton'].map(name => [name, loadUiComponent(name)]))
  ui.Tabs = defineComponent({ props: ['modelValue', 'items', 'label'], emits: ['update:modelValue'], setup: (props, { slots, emit }) => () => h('div', [h('div', { role: 'tablist' }, (props.items as any[]).map(item => h('button', { role: 'tab', onClick: () => emit('update:modelValue', item.value) }, item.label))), slots[String(props.modelValue)]?.()]) })
  const modules: Record<string, unknown> = {
    '@/components/ui': ui,
    '@/components/ui/feedback': { confirm: (input: ConfirmOptions) => { calls.confirmations.push(input); return sharedConfirm(input) }, toast: { success: (value: string) => calls.notices.push(value) } },
    '@/api/entities': entities, '@/api/entry-review': reviews, '@/api/library': library,
    '@/utils/bodyReference': reference, '@/utils/security': security, '@/types/entryClassification': classification,
    '@/utils/search': { searchErrorCode: (cause: any) => cause?.error?.code ?? cause?.code },
    '@/stores/pendingCounts': { usePendingCountsStore: () => ({ refresh: async () => { calls.counts++ } }) },
    'vue-router': { useRoute: () => route, useRouter: () => ({ replace: async (target: any) => { route.query = target.query } }) },
  }
  for (const name of ['MergeCandidate', 'PendingReviewActions', 'MergeSuggestions', 'PendingReviews']) {
    modules[`./${name}.vue`] = { __esModule: true, default: loadComponent(new URL(`./components/${name}.vue`, import.meta.url), modules) }
    modules[`./components/${name}.vue`] = modules[`./${name}.vue`]
  }
  const component = loadComponent(new URL('./PendingPage.vue', import.meta.url), modules)
  const app = renderer.createApp(component)
  app.use(createI18n({ legacy: false, locale: options.language ?? 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } }))
  const root = element('root'); app.mount(root); t.after(() => { app.unmount(); for (const dialog of useFeedback().confirmations) dialog.cancel() })
  return { root, route, calls, data }
}

test('pending page counts both queues and hides tabs when only one category remains', async t => {
  const { root, calls, data } = mount(t)
  await settle()
  assert.ok(find(root, 'button', '待合并建议 2'))
  assert.ok(find(root, 'button', '历史待审 1'))
  await click(find(root, 'button', '历史待审 1'))
  assert.ok(find(root, 'a', 'Review old'))
  await click(find(root, 'button', '收录'))
  await settle()
  assert.equal(data.reviews.length, 0)
  assert.equal(descendants(root).some(node => node.props.role === 'tablist'), false)
  assert.ok(visibleText(root).includes('Fresh one'))
  assert.equal(calls.counts, 1)
})


test('merge decisions retain the target identity, confirm acceptance and advance without losing history', async t => {
  const { root, calls } = mount(t, { reviews: [] })
  await settle()
  await click(find(root, 'button', '合并到 Existing one'))
  assert.equal(calls.decisions.length, 0)
  assert.equal(useFeedback().confirmations.length, 1)
  assert.equal(descendants(root).find(node => node.tag === 'select' && node.props['aria-label'] === zh.mergeSuggestions.statusFilter)?.props.disabled, true)
  await useFeedback().confirmations[0]!.accept(); await settle()
  assert.deepEqual(calls.decisions, [['accept', 'one']])
  assert.ok(visibleText(root).includes('Fresh two'))
  await click(find(root, 'button', '保持独立'))
  assert.deepEqual(calls.decisions, [['accept', 'one'], ['reject', 'two']])
  assert.ok(visibleText(root).includes(zh.mergeSuggestions.empty.pending))
  const status = descendants(root).find(node => node.tag === 'select' && node.props['aria-label'] === zh.mergeSuggestions.statusFilter)!
  status.props.onChange({ target: { value: 'accepted' } }); await settle()
  assert.ok(visibleText(root).includes('Fresh one'))
  assert.equal(calls.counts, 2)
})

test('historical rejection requires confirmation, preserves failed decisions for retry and refreshes counts', async t => {
  let attempts = 0
  const { root, calls } = mount(t, { query: { tab: 'reviews' }, api: {
    resolveIntakeReview: async (id: string, input: any) => { calls.decisions.push([id, input]); if (++attempts === 1) throw { error: { code: 1005 } }; return { success: true } },
  } })
  await settle(); await click(find(root, 'button', '拒绝并删除'))
  assert.equal(attempts, 0)
  await useFeedback().confirmations[0]!.accept(); await settle()
  assert.equal(attempts, 1)
  assert.match(visibleText(root), /状态已改变/)
  assert.equal(useFeedback().confirmations.length, 1)
  await useFeedback().confirmations[0]!.accept(); await settle()
  assert.deepEqual(calls.decisions.at(-1), ['old', { action: 'reject', confirmed: true }])
  assert.equal(calls.counts, 1)
})

test('source controls validate URLs, keep provenance edits and confirm unverified admission', async t => {
  const { root, calls } = mount(t, { query: { tab: 'reviews' }, reviews: [review('missing', '')] })
  await settle()
  assert.ok(find(root, 'summary', '编辑来源信息'))
  const inputs = descendants(root).filter(node => node.tag === 'input')
  for (const input of inputs) Object.assign(input, { focus() {} })
  inputs[0]!.props.onInput({ target: { value: 'javascript:alert(1)' } })
  await click(find(root, 'button', '补充链接并收录'))
  assert.match(visibleText(root), /有效的 HTTP/)
  assert.equal(calls.decisions.length, 0)
  inputs[1]!.props.onInput({ target: { value: 'Actual author' } })
  inputs[0]!.props.onInput({ target: { value: 'https://example.com/new' } })
  await click(find(root, 'button', '补充链接并收录'))
  assert.deepEqual(calls.decisions[0], ['missing', { action: 'supplement_url', source_info: { author: 'Actual author', original_url: 'https://example.com/new' } }])
})

test('unverified admission and personal material approval use their existing distinct contracts', async t => {
  const one = mount(t, { query: { tab: 'reviews' }, reviews: [review('missing', '')] })
  await settle(); await click(find(one.root, 'button', '无链接入库'))
  assert.equal(one.calls.decisions.length, 0)
  await useFeedback().confirmations[0]!.accept(); await settle()
  assert.deepEqual(one.calls.decisions[0], ['missing', { action: 'admit_unverified', confirmed: true }])
  const two = mount(t, { query: { tab: 'reviews' }, reviews: [{ ...review('mine', ''), source_kind: 'mine' }] })
  await settle()
  assert.equal(descendants(two.root).some(node => node.tag === 'input'), false)
  await click(find(two.root, 'button', '通过并收录'))
  assert.deepEqual(two.calls.decisions[0], ['mine', { action: 'approve', source_info: { original_url: '', author: 'Writer' } }])
})

test('source excerpts highlight literal entity names and link to the real paragraph', async t => {
  const { root } = mount(t, { reviews: [] })
  await settle()
  const mark = find(root, 'mark', 'Fresh one')
  assert.ok(mark)
  const link = find(root, 'a', 'Source evidence')
  assert.equal(link.props.title, 'source-full-identifier')
  const reference = JSON.parse(new URL(link.props.href, 'https://example.com').searchParams.get('reference')!)
  assert.equal(reference.paragraph, 2)
  assert.equal(reference.body_hash, 'hash')
  assert.ok(visibleText(root).includes('Actual description'))
  assert.equal(visibleText(root).includes('source-full-identifier'), false)
})

test('library filters preserve multiple selections in URLs and both queue requests', async t => {
  const { root, route, calls } = mount(t, { query: { libraries: 'a,b', tab: 'reviews' } })
  await settle()
  assert.ok(visibleText(root).includes('Library A'))
  assert.ok(visibleText(root).includes('Library B'))
  assert.ok(calls.scopes.some(query => query.kind === 'intake' && query.libraries.join(',') === 'a,b'))
  await click(descendants(root).find(node => node.tag === 'button' && node.props['aria-label'] === '移除库筛选：Library A')!)
  assert.equal(route.query.libraries, 'b')
  await settle()
  assert.ok(calls.scopes.some(query => query.kind === 'intake' && query.libraries.join(',') === 'b'))
})

test('queue failures remain retryable and never become the all-clear state', async t => {
  let failed = true
  const { root } = mount(t, { reviews: [], api: { listEntityMergeSuggestions: async () => { if (failed) throw new Error('offline'); return { success: true, data: [] } } } })
  await settle()
  assert.ok(visibleText(root).includes(zh.pendingPage.loadFailed))
  assert.equal(visibleText(root).includes(zh.pendingPage.empty), false)
  failed = false; await click(find(root, 'button', '重试'))
  assert.ok(visibleText(root).includes(zh.pendingPage.empty))
})

test('merge confirmation cancellation makes no change and duplicate confirmation cannot submit twice', async t => {
  let release!: (value: unknown) => void, submissions = 0
  const result = new Promise(resolve => { release = resolve })
  const { root } = mount(t, { reviews: [], api: { acceptEntityMergeSuggestion: () => { submissions++; return result } } })
  await settle(); await click(find(root, 'button', '合并到 Existing one'))
  useFeedback().confirmations[0]!.cancel(); await settle()
  assert.equal(submissions, 0)
  await click(find(root, 'button', '合并到 Existing one'))
  const confirmation = useFeedback().confirmations[0]!
  const first = confirmation.accept(), second = confirmation.accept()
  assert.equal(submissions, 1)
  release({ success: true }); await Promise.all([first, second]); await settle()
  assert.ok(visibleText(root).includes('Fresh two'))
})

test('review pagination clamps after resolving the last record on a page', async t => {
  const records = Array.from({ length: 21 }, (_, index) => review(`old-${index}`))
  const { root, calls } = mount(t, { query: { tab: 'reviews' }, reviews: records })
  await settle(); await click(find(root, 'button', '下一条'))
  assert.ok(visibleText(root).includes('Review old-20'))
  await click(find(root, 'button', '通过并收录')); await settle()
  assert.ok(visibleText(root).includes('Review old-0'))
  assert.ok(calls.scopes.some(query => query.kind === 'intake' && query.page === 2))
  assert.equal(calls.scopes.at(-1).page, 1)
  assert.equal(descendants(root).some(node => node.props['aria-label'] === zh.pendingPage.pagination), false)
})

test('scope changes discard late queue results and aggregate every merge page', async t => {
  let release!: (value: unknown) => void
  const old = new Promise(resolve => { release = resolve })
  const { root, route, calls } = mount(t, { query: { libraries: 'a' }, reviews: [], api: {
    listEntityMergeSuggestions: async (query: any) => {
      calls.scopes.push(query)
      if (query.library_id === 'a') return old
      return { success: true, data: query.offset === 0 ? Array.from({ length: 100 }, (_, i) => suggestion(`b-${i}`)) : [suggestion('last')] }
    },
  } })
  await settle(); route.query = { libraries: 'b' }; await settle()
  assert.ok(visibleText(root).includes('1 / 101'))
  release({ success: true, data: [suggestion('stale')] }); await settle()
  assert.equal(visibleText(root).includes('Fresh stale'), false)
  assert.ok(calls.scopes.some(query => query.library_id === 'b' && query.offset === 100))
})

test('missing merge suggestions show a clear stale state while retaining the retry action', async t => {
  const { root, calls } = mount(t, { reviews: [], api: { rejectEntityMergeSuggestion: async () => { throw { error: { code: 1003 } } } } })
  await settle(); await click(find(root, 'button', '保持独立'))
  assert.ok(visibleText(root).includes(zh.pendingPage.notFound))
  assert.ok(find(root, 'button', '保持独立'))
  assert.equal(calls.counts, 0)
})


test('a single historical queue hides the category bar and keeps its decisions available', async t => {
  const { root } = mount(t, { merges: [] })
  await settle()
  assert.equal(descendants(root).some(node => node.props.role === 'tablist'), false)
  assert.ok(find(root, 'a', 'Review old'))
})


test('keyboard queue navigation moves between suggestions and leaves editable fields alone', async t => {
  const { root } = mount(t, { reviews: [] })
  await settle()
  const queue = descendants(root).find(node => node.props.class === 'merge-suggestions')!
  queue.props.onKeydown({ key: 'j', target: { closest: () => null }, preventDefault() {} }); await settle()
  assert.ok(visibleText(root).includes('Fresh two'))
  queue.props.onKeydown({ key: 'k', target: { closest: () => ({}) }, preventDefault() {} }); await settle()
  assert.ok(visibleText(root).includes('Fresh two'))
  queue.props.onKeydown({ key: 'k', target: { closest: () => null }, preventDefault() {} }); await settle()
  assert.ok(visibleText(root).includes('Fresh one'))
})

for (const language of ['zh-CN', 'en-US']) {
  for (const status of ['accepted', 'rejected']) {
    test(`${language}: ${status} history preserves recorded identities without current entity details`, async t => {
      let detailReads = 0
      const { root } = mount(t, { language, reviews: [], merges: [suggestion('historical', status)], api: {
        getEntity: async () => {
          detailReads++
          return { success: true, data: { entity: { id: 'surviving-entity', canonical_name: 'Current surviving name', description: 'Current merged description', aliases: ['Current merged alias'], entity_type: 'company', library_id: 'a' }, entries: [{ id: 'combined-source', title: 'Current combined evidence' }] } }
        },
      } })
      await settle()
      const filter = descendants(root).find(node => node.tag === 'select' && node.props['aria-label'] === (language === 'zh-CN' ? zh : en).mergeSuggestions.statusFilter)!
      filter.props.onChange({ target: { value: status } }); await settle()
      const headings = descendants(root).filter(node => node.tag === 'h3').map(visibleText)
      assert.deepEqual(headings, ['Fresh historical', 'Existing historical'])
      assert.doesNotMatch(visibleText(root), /Current surviving name|Current merged description|Current merged alias|Current combined evidence/)
      assert.equal(descendants(root).some(node => node.tag === 'dl'), false)
      assert.equal(detailReads, 0, 'historical records never resolve current or missing entity profiles')
      if (status === 'accepted') assert.ok(visibleText(root).includes(language === 'zh-CN' ? '已合并入 Existing historical' : 'Merged into Existing historical'))
      else assert.doesNotMatch(visibleText(root), /已合并入|Merged into/)
      assert.ok(descendants(root).some(node => node.tag === 'a' && node.props.href === '/platform/entries/entry-historical'))
    })
  }
}

test('accepted history remains readable after its merged entity has disappeared', async t => {
  let reads = 0
  const { root } = mount(t, { reviews: [], merges: [suggestion('deleted', 'accepted')], api: {
    getEntity: async () => { reads++; throw { status: 404, error: { code: 1003 } } },
  } })
  await settle()
  const filter = descendants(root).find(node => node.tag === 'select' && node.props['aria-label'] === zh.mergeSuggestions.statusFilter)!
  filter.props.onChange({ target: { value: 'accepted' } }); await settle()
  assert.deepEqual(descendants(root).filter(node => node.tag === 'h3').map(visibleText), ['Fresh deleted', 'Existing deleted'])
  assert.ok(visibleText(root).includes('已合并入 Existing deleted'))
  assert.equal(descendants(root).some(node => node.props.role === 'alert'), false)
  assert.equal(reads, 0)
})
