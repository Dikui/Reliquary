import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import { defineComponent, h, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import { loadUiComponent } from '../../components/ui/uiTestLoader'
import { click, descendants, element, loadComponent, renderer, settle, visibleText, type Element } from './componentTestHarness'
import * as entryClassification from '../../types/entryClassification'
import type { Topic, TopicBackfill } from '../../api/topics'
import type { ConfirmOptions } from '../../components/ui/feedback'

const topic = (id: string, library_id = 'a'): Topic => ({ id, library_id, name: `Topic ${id}`, description: `Description ${id}`, entry_count: 2, created_at: '', updated_at: '' })
const run = (status: TopicBackfill['status'] = 'running'): TopicBackfill => ({ id: 'job', topic_id: 'one', status, total: 10, processed: 6, failed_entries: 2, failed_batches: 1, created_at: '', updated_at: '', batches: [{ id: 'batch', ordinal: 3, status: 'failed', entry_count: 2, attempts: 3, error: 'Model output invalid' }] })
const deferred = () => { let resolve!: (value: any) => void; const promise = new Promise<any>(done => { resolve = done }); return { promise, resolve } }
const find = (root: Element, tag: string, text: string) => descendants(root).find(node => node.tag === tag && visibleText(node).includes(text))!

function mount(t: TestContext, overrides: Record<string, any> = {}, query: Record<string, string> = {}, language = 'zh-CN') {
  const calls = { lists: [] as string[], entries: [] as any[], notices: [] as string[], confirmations: [] as ConfirmOptions[], creates: [] as any[], updates: [] as any[], deletes: [] as string[], retries: [] as string[] }
  const route = reactive({ query: { ...query } })
  const api = {
    listTopics: async (id: string) => { calls.lists.push(id); return { success: true, data: [topic(id === 'a' ? 'one' : 'two', id)] } },
    getTopicBackfill: async () => ({ success: true, data: null }),
    createTopic: async (input: any) => { calls.creates.push(input); return { success: true, data: topic('created', input.library_id) } },
    updateTopic: async (id: string, input: any) => { calls.updates.push([id, input]); return { success: true, data: { ...topic(id), ...input } } },
    deleteTopic: async (id: string) => { calls.deletes.push(id); return { success: true } },
    retryTopicBackfill: async (id: string) => { calls.retries.push(id); return { success: true, data: run() } },
    ...overrides,
  }
  const component = loadComponent(new URL('./TopicManagement.vue', import.meta.url), {
    'vue-router': { useRoute: () => route, useRouter: () => ({ replace: async ({ query }: any) => { route.query = query } }) },
    '@/api/topics': api,
    '@/types/entryClassification': entryClassification,
    '@/api/library': {
      listLibraries: async () => ({ success: true, data: [{ id: 'a', name: 'Library A', color: 1 }, { id: 'b', name: 'Library B', color: 2 }] }),
      listEntryFiles: async (id: string, params: any) => { calls.entries.push([id, params]); return { success: true, total: 1, data: [{ id: '12345678-full-entry-id', title: 'Actual material', library_id: id, content_type: 'research', created_at: '2026-10-01T12:00:00Z' }] } },
      ...overrides.library,
    },
    '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) },
    '@/components/ui/feedback': { confirm: (options: ConfirmOptions) => { calls.confirmations.push(options); return { destroy() {}, update(next: Partial<ConfirmOptions>) { Object.assign(options, next) } } }, toast: { success: (text: string) => calls.notices.push(text) } },
    '@/components/ui': Object.fromEntries(['Button', 'Input', 'Textarea', 'Field', 'Select', 'LibMark', 'Tag', 'EmptyState', 'ErrorState'].map(name => [name, loadUiComponent(name)]).concat([['Dialog', defineComponent({ props: ['modelValue', 'title'], setup: (props, { slots }) => () => props.modelValue ? h('section', { role: 'dialog' }, [props.title, slots.default?.(), slots.footer?.()]) : null })]])),
    '@/components/FollowToggle.vue': { __esModule: true, default: defineComponent({ props: { targetId: String, swiss: Boolean }, setup: props => () => h('button', { 'data-follow': props.targetId, 'data-swiss': props.swiss }, 'Follow') }) },
  })
  const app = renderer.createApp(component)
  app.use(createI18n({ legacy: false, locale: language, messages: { 'zh-CN': zh, 'en-US': en } }))
  const root = element('root'); app.mount(root); t.after(() => app.unmount())
  return { root, calls, route, api, app }
}

test('all-library list selects a topic, reads its materials, and keeps the full ID in the detail link', async t => {
  const { root, calls } = mount(t)
  await settle()
  assert.deepEqual(calls.lists, ['a', 'b'])
  assert.match(visibleText(root), /Description one/)
  assert.ok(descendants(root).some(node => node.props['data-follow'] === 'one'))
  assert.deepEqual(calls.entries[0], ['a', { page: 1, page_size: 20, libraries: ['a'], topic_ids: ['one'] }])
  const link = find(root, 'a', 'Actual material')
  assert.equal(link.props.href, '/platform/entries/12345678-full-entry-id')
  assert.ok(find(root, 'td', '12345678'))
  assert.equal(visibleText(root).includes('12345678-full-entry-id'), false)
})

test('switching library or selected topic rejects stale material and progress responses', async t => {
  const staleEntries = deferred(), staleProgress = deferred()
  const { root, route, calls } = mount(t, {
    getTopicBackfill: async (id: string) => id === 'one' ? staleProgress.promise : { success: true, data: { ...run('completed'), topic_id: 'two', batches: [], failed_entries: 0, failed_batches: 0 } },
    library: { listEntryFiles: async (id: string, params: any) => id === 'a' ? staleEntries.promise : { success: true, total: 1, data: [{ id: 'current', library_id: 'b', title: 'Current material' }] } },
  }, { libraries: 'a' })
  await settle()
  route.query = { libraries: 'b' }; await settle()
  staleEntries.resolve({ success: true, total: 99, data: [{ id: 'old', title: 'Stale material' }] })
  staleProgress.resolve({ success: true, data: run('failed') }); await settle()
  assert.match(visibleText(root), /Description two/)
  assert.match(visibleText(root), /Current material/)
  assert.doesNotMatch(visibleText(root), /Stale material|Model output invalid/)
  assert.deepEqual(calls.lists, ['a', 'b'])
})

test('polling refreshes counts and materials at completion, then stops and cleans up on unmount', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let progressReads = 0, listReads = 0, entryReads = 0
  const { root, app } = mount(t, {
    listTopics: async () => ({ success: true, data: [{ ...topic('one'), entry_count: ++listReads === 1 ? 2 : 4 }] }),
    getTopicBackfill: async () => ({ success: true, data: run(++progressReads === 1 ? 'running' : 'completed') }),
    library: { listEntryFiles: async () => { entryReads++; return { success: true, data: [], total: 0 } } },
  }, { libraries: 'a' })
  await settle()
  const bar = descendants(root).find(node => node.props.role === 'progressbar')!
  assert.equal(bar.props['aria-valuenow'], 60)
  assert.match(visibleText(root), /Model output invalid/)
  const retry = find(root, 'button', zh.topicManagement.retryFailedBatches)
  assert.equal(retry.props.disabled, true)
  t.mock.timers.tick(2000); await settle()
  assert.equal(progressReads, 2); assert.equal(listReads, 2); assert.equal(entryReads, 2)
  assert.match(visibleText(root), /历史归入完成/)
  t.mock.timers.tick(10000); await settle()
  assert.equal(progressReads, 2)
  app.unmount()
  t.mock.timers.tick(10000); await settle()
  assert.equal(progressReads, 2)
})

test('failed batches retry once, retain errors, and use the returned pending progress', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const pending = deferred()
  let retryReads = 0
  const { root } = mount(t, {
    getTopicBackfill: async () => ({ success: true, data: run('failed') }),
    retryTopicBackfill: async () => { retryReads++; if (retryReads === 1) throw { status: 409 }; return pending.promise },
  }, { libraries: 'a' })
  await settle()
  await click(find(root, 'button', zh.topicManagement.retryFailedBatches))
  assert.match(visibleText(root), /历史归入状态已变化/)
  await click(find(root, 'button', zh.topicManagement.retryFailedBatches))
  await click(find(root, 'button', zh.topicManagement.retryFailedBatches))
  assert.equal(retryReads, 2)
  pending.resolve({ success: true, data: { ...run('pending'), processed: 4, failed_entries: 0, failed_batches: 0, batches: [] } }); await settle()
  assert.match(visibleText(root), /等待历史归入/)
  assert.doesNotMatch(visibleText(root), /Model output invalid/)
})

function setInput(root: Element, tag: string, index: number, value: string) {
  const node = descendants(root).filter(node => node.tag === tag)[index]!
  node.props[tag === 'select' ? 'onChange' : 'onInput']({ target: { value } })
}
async function submit(root: Element) {
  for (const node of descendants(root).filter(node => node.tag === 'input')) Object.assign(node, { focus() {} })
  descendants(root).find(node => node.tag === 'form')!.props.onSubmit({ preventDefault() {} })
  await settle()
}

test('create requires a library, preserves a conflict draft, and blocks duplicate submissions', async t => {
  const response = deferred()
  let requests = 0
  const { root, calls } = mount(t, { createTopic: async (input: any) => { calls.creates.push(input); if (++requests === 1) throw { error: { code: 1005 } }; return response.promise } })
  await settle(); await click(find(root, 'button', zh.topicManagement.create))
  await submit(root)
  assert.match(visibleText(root), /请填写主题名称/)
  assert.match(visibleText(root), /请选择主题所属的库/)
  setInput(root, 'input', 0, '  New direction  ')
  setInput(root, 'textarea', 0, '  Scope definition  ')
  setInput(root, 'select', 1, 'b')
  await submit(root)
  assert.match(visibleText(root), /此名称已存在/)
  assert.equal(descendants(root).find(node => node.tag === 'input')?.props.value, '  New direction  ')
  await submit(root); await submit(root)
  assert.equal(requests, 2)
  assert.deepEqual(calls.creates[1], { name: 'New direction', description: 'Scope definition', library_id: 'b' })
  response.resolve({ success: true, data: topic('two', 'b') }); await settle()
  assert.equal(descendants(root).some(node => node.props.role === 'dialog'), false)
  assert.deepEqual(calls.notices, [zh.topicManagement.created])
})

test('edit fixes the original library and deletion uses a retryable shared confirmation', async t => {
  let failDelete = true
  const { root, calls } = mount(t, { deleteTopic: async (id: string) => { calls.deletes.push(id); if (failDelete) throw { status: 404 }; return { success: true } } }, { libraries: 'a' })
  await settle(); await click(find(root, 'button', zh.topicManagement.edit))
  const select = descendants(root).filter(node => node.tag === 'select')[1]!
  assert.equal(select.props.disabled, true); assert.equal(select.props.value, 'a')
  setInput(root, 'textarea', 0, 'Revised scope'); await submit(root)
  assert.deepEqual(calls.updates, [['one', { name: 'Topic one', description: 'Revised scope' }]])
  await click(find(root, 'button', zh.common.delete))
  assert.deepEqual(calls.deletes, [])
  const confirmation = calls.confirmations[0]!
  assert.match(confirmation.body!, /Topic one/)
  assert.match(confirmation.body!, /资料仍然保留/)
  await assert.rejects(() => confirmation.onConfirm!({ dialog: {} as any }) as Promise<void>, /主题已不存在/)
  failDelete = false
  await confirmation.onConfirm!({ dialog: {} as any })
  assert.deepEqual(calls.deletes, ['one', 'one'])
  assert.equal(calls.notices.at(-1), zh.topicManagement.deleted)
})

test('partial library failures preserve readable topics and offer a working list retry', async t => {
  let failure = true
  const { root } = mount(t, { listTopics: async (id: string) => { if (id === 'b' && failure) throw Error('unavailable'); return { success: true, data: [topic(id, id)] } } })
  await settle()
  assert.match(visibleText(root), /Library B/)
  assert.match(visibleText(root), /无法加载主题/)
  assert.match(visibleText(root), /Description a/)
  failure = false
  await click(find(root, 'button', zh.common.retry)); await settle()
  assert.doesNotMatch(visibleText(root), /无法加载主题/)
  assert.ok(find(root, 'strong', 'Topic b'))
})

test('empty topics have no table or detail panel and missing progress has a specific recovery state', async t => {
  const empty = mount(t, { listTopics: async () => ({ success: true, data: [] }) })
  await settle()
  assert.match(visibleText(empty.root), /还没有主题/)
  assert.equal(descendants(empty.root).some(node => node.tag === 'table'), false)
  const missing = mount(t, { getTopicBackfill: async () => { throw { status: 404 } } }, { libraries: 'a' })
  await settle()
  assert.match(visibleText(missing.root), /主题已不存在/)
  assert.equal(descendants(missing.root).some(node => node.props.role === 'progressbar'), false)
})

test('material pages use the selected topic and expose errors without losing the last readable rows', async t => {
  let fail = false
  const { root, calls } = mount(t, {
    library: { listEntryFiles: async (id: string, params: any) => { calls.entries.push([id, params]); if (fail) throw Error('unavailable'); return { success: true, total: 21, data: [{ id: `entry-page-${params.page}`, title: `Material page ${params.page}`, library_id: id }] } } },
  }, { libraries: 'a' })
  await settle()
  fail = true; await click(find(root, 'button', zh.topicManagement.next))
  assert.match(visibleText(root), /无法加载主题下资料/)
  assert.match(visibleText(root), /Material page 1/)
  fail = false; await click(find(root, 'button', zh.common.retry))
  assert.match(visibleText(root), /Material page 2/)
  assert.deepEqual(calls.entries.at(-1)?.[1], { page: 2, page_size: 20, libraries: ['a'], topic_ids: ['one'] })
})

test('creating in another library opens the created topic within that library filter', async t => {
  let created = false
  const { root, route } = mount(t, {
    listTopics: async (id: string) => ({ success: true, data: created && id === 'b' ? [topic('created', 'b')] : [topic(id === 'a' ? 'one' : 'two', id)] }),
    createTopic: async () => { created = true; return { success: true, data: topic('created', 'b') } },
  }, { libraries: 'a' })
  await settle(); await click(find(root, 'button', zh.topicManagement.create))
  setInput(root, 'input', 0, 'New direction'); setInput(root, 'select', 1, 'b')
  await submit(root)
  assert.equal(route.query.libraries, 'b')
  assert.equal(route.query.topic_id, 'created')
  assert.match(visibleText(root), /Description created/)
})

test('changing topics in the same library ignores a late page response and preserves URL context', async t => {
  const pending = deferred()
  const { root, route } = mount(t, {
    listTopics: async () => ({ success: true, data: [topic('one'), topic('two')] }),
    library: { listEntryFiles: async (_id: string, params: any) => params.topic_ids[0] === 'one' ? pending.promise : { success: true, total: 1, data: [{ id: 'second', title: 'Second topic material', library_id: 'a' }] } },
  }, { libraries: 'a', extra: 'preserve' })
  await settle(); await click(find(root, 'button', 'Topic two'))
  pending.resolve({ success: true, total: 100, data: [{ id: 'old', title: 'Old topic material' }] }); await settle()
  assert.match(visibleText(root), /Second topic material/)
  assert.doesNotMatch(visibleText(root), /Old topic material/)
  assert.deepEqual(route.query, { libraries: 'a', extra: 'preserve', topic_id: 'two' })
})

test('unmounting an active assignment cancels polling and discards late reads', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  let calls = 0
  const pending = deferred()
  const { app } = mount(t, { getTopicBackfill: async () => ++calls === 1 ? { success: true, data: run() } : pending.promise }, { libraries: 'a' })
  await settle()
  t.mock.timers.tick(2000); await settle()
  assert.equal(calls, 2)
  app.unmount()
  pending.resolve({ success: true, data: run() }); await settle()
  t.mock.timers.tick(10000); await settle()
  assert.equal(calls, 2)
})

test('cancelled assignment retains failure details without offering an active retry', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const { root, calls } = mount(t, { getTopicBackfill: async () => ({ success: true, data: run('cancelled') }) }, { libraries: 'a' })
  await settle()
  assert.match(visibleText(root), /历史归入已取消/)
  assert.equal(find(root, 'button', zh.topicManagement.retryFailedBatches).props.disabled, true)
  await click(find(root, 'button', zh.topicManagement.retryFailedBatches))
  t.mock.timers.tick(10000); await settle()
  assert.deepEqual(calls.retries, [])
  assert.equal(calls.lists.length, 1)
})

test('an empty library catalog explains how to start and makes no unscoped topic requests', async t => {
  const { root, calls } = mount(t, { library: { listLibraries: async () => ({ success: true, data: [] }) } })
  await settle()
  assert.deepEqual(calls.lists, [])
  assert.match(visibleText(root), /请先在设置中创建库/)
  assert.equal(find(root, 'button', zh.topicManagement.create).props.disabled, true)
  assert.equal(descendants(root).some(node => node.tag === 'table'), false)
})

test('English assignment failures show actual material and batch counts', async t => {
  const { root } = mount(t, { getTopicBackfill: async () => ({ success: true, data: run('failed') }) }, { libraries: 'a' }, 'en-US')
  await settle()
  assert.match(visibleText(root), /Failed: 2 materials in 1 batches/)
})


test('material type tags use translated built-in names and retain custom library names', async t => {
  const { root } = mount(t, { library: { listEntryFiles: async () => ({ success: true, total: 5, data: [
    { id: 'a', title: 'Built-in other', library_id: 'a', content_type: 'other' },
    { id: 'b', title: 'Built-in research', library_id: 'a', content_type: 'research' },
    { id: 'c', title: 'Custom material', library_id: 'a', content_type: '内部报告' },
    { id: 'd', title: 'Missing type', library_id: 'a' },
    { id: 'e', title: 'Unknown custom', library_id: 'a', content_type: 'custom_type' },
  ] }) } }, { libraries: 'a' })
  await settle()
  const tags = descendants(root).filter(node => String(node.props.class).split(' ').includes('rq-tag'))
  assert.deepEqual(tags.map(visibleText), ['其他', '研究', '内部报告', '未分类', 'custom_type'])
  assert.doesNotMatch(visibleText(root), /归入方式暂不可用|手动或自动筛选/)
})

test('English material type tags show a display name instead of the built-in identifier', async t => {
  const { root } = mount(t, {}, { libraries: 'a' }, 'en-US')
  await settle()
  const tags = descendants(root).filter(node => String(node.props.class).split(' ').includes('rq-tag'))
  assert.deepEqual(tags.map(visibleText), ['Research'])
  assert.doesNotMatch(visibleText(root), /Assignment sources are unavailable|automatic filters cannot/)
})
