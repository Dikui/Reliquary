import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { loadUiComponent } from '../../components/ui/uiTestLoader'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from './componentTestHarness'
import * as detail from './useEntryDetail'
import * as references from '../../utils/bodyReference'
import * as status from '../../utils/entryProcessingStatus'
import * as source from '../../utils/entrySourceLabels'
import * as external from '../../utils/externalUrl'
import * as markdown from '../../utils/markdownPlainText'
import * as spans from '../../utils/processingSpanLabel'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'

async function mountDetail(locale: string, kind = 'article', starAction?: (starred: boolean) => Promise<any>) {
  const oldDocument = globalThis.document
  globalThis.document = { title: '' } as Document
  const route = reactive({ params: { id: 'entry-a' }, query: {} as Record<string, unknown> })
  const pushes: any[] = [], confirmations: any[] = [], requests: string[] = []
  let guard: () => boolean = () => true, fail = true, release: (() => void) | undefined
  const result = (data: any = {}) => Promise.resolve({ success: true, data })
  const api = {
    getEntryDetails: () => result({ id: 'entry-a', title: 'Real title', source_kind: kind, library_id: 'library-a', source: 'javascript:bad', content_type: 'research', parse_status: 'completed', summary_status: 'completed', created_at: '2026-10-28T10:00:00Z', structured_summary: { overview: '**Real** overview', points: ['Point'], key_terms: ['Term'] }, entities: [{ id: 'entity-a', canonical_name: 'Entity A', entity_type: 'company' }], library_content_types: [{ name: 'research', enabled: true }, { name: 'tutorial', enabled: true }] }),
    getEntryEditableBody: () => Promise.resolve({ content: '# Full body\n\nSaved 😀 body', bodyHash: 'current-hash' }),
    getCatalogLibrary: () => result({ id: 'library-a', name: 'Library A', color: 2 }),
    getEntrySpans: () => result({ current_attempt: 1, trace: { name: 'pipeline', kind: 'pipeline', children: [{ name: 'processing', kind: 'stage', status: 'done', duration_ms: 1250 }] } }),
    reparseEntry: async () => { requests.push('reparse'); return result() },
    cancelEntryParse: () => result(),
    delEntryDetails: async () => { requests.push('article'); await new Promise<void>(done => { release = done }); if (fail) throw new Error('Delete failed'); return result() },
    deleteDocument: async () => { requests.push('mine'); return result() },
  }
  const ui = Object.fromEntries(['Button', 'EmptyState', 'ErrorState', 'LibMark', 'Select', 'Skeleton', 'StatusSquare', 'Tag'].map(name => [name, loadUiComponent(name)]))
  const Tabs = defineComponent({ props: ['modelValue', 'items'], emits: ['update:modelValue'], setup: (props, { slots, emit }) => () => h('div', [h('nav', props.items.map((item: any) => h('button', { onClick: () => emit('update:modelValue', item.value) }, item.label))), slots[props.modelValue]?.()]) })
  const Dialog = defineComponent({ props: ['modelValue', 'title'], setup: (props, { slots }) => () => props.modelValue ? h('section', { role: 'dialog' }, [props.title, slots.default?.(), slots.footer?.()]) : null })
  const empty = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
  const Component = loadComponent(new URL('./EntryDetail.vue', import.meta.url), {
    'vue-router': { useRoute: () => route, useRouter: () => ({ replace: async ({ query }: any) => { route.query = query }, push: async (value: any) => { pushes.push(value) } }), onBeforeRouteLeave: (fn: any) => { guard = fn }, onBeforeRouteUpdate() {} },
    '@/api/library': api, '@/api/entities': { getEntity: () => result({ entity: { id: 'entity-a', canonical_name: 'Entity A', entity_type: 'company', description: 'Verified description' } }) },
    '@/api/relations': { listEntityRelations: () => result([{ relations: [{ id: 'r', relation_type: 'uses', direction: 'outgoing', peer: { id: 'b', canonical_name: 'B' }, sources: [{ entry_id: 'entry-a', body_hash: 'current-hash', evidence: [{ locations: [{ paragraph: 2, start: 1, end: 2 }] }] }] }] }]) },
    '@/api/entry-judgments': { correctEntryContentType: async (_id: string, content_type: string) => { requests.push(content_type); return result({ content_type }) }, setEntryStar: async (_id: string, starred: boolean) => starAction ? starAction(starred) : result({ starred }) },
    '@/components/ui': { ...ui, Tabs, Dialog, confirm: (options: any) => { confirmations.push(options); return { destroy() {} } }, toast: { success() {} } },
    '@/components/DocContent.vue': { __esModule: true, default: defineComponent({ props: ['details'], setup: props => () => h('div', { 'data-body': '' }, props.details.md[0]?.content) }) },
    '@/components/FollowToggle.vue': empty, './components/RepostSources.vue': empty,
    './useEntryDetail': detail, '@/utils/bodyReference': references, '@/utils/entryProcessingStatus': status, '@/utils/entrySourceLabels': source, '@/utils/externalUrl': external, '@/utils/markdownPlainText': markdown, '@/utils/processingSpanLabel': spans,
  })
  const root = element('root'), app = renderer.createApp(Component)
  app.use(createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } }))
  app.component('RouterLink', defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { href: typeof props.to === 'string' ? props.to : props.to.path }, slots.default?.()) }))
  app.mount(root); await settle()
  return { root, requests, route, pushes, confirmations, guard: () => guard(), release: () => release?.(), succeed: () => { fail = false }, close: () => { app.unmount(); globalThis.document = oldDocument } }
}

for (const locale of ['zh-CN', 'en-US']) test(`${locale}: detail loads real content and its relationship reference changes the route`, async () => {
  const view = await mountDetail(locale), messages = locale === 'zh-CN' ? zh : en
  try {
    assert.ok(visibleText(view.root).includes('Real title'))
    assert.ok(visibleText(view.root).includes('# Full body'))
    assert.ok(visibleText(view.root).includes('Real overview'))
    assert.ok(!descendants(view.root).some(node => node.props.href === 'javascript:bad'))
    view.route.query.tab = 'relations'; await settle()
    await click(descendants(view.root).find(node => node.tag === 'button' && visibleText(node) === '¶2')!)
    assert.equal(JSON.parse(String(view.route.query.reference)).body_hash, 'current-hash')
    view.route.query.tab = 'processing'; await settle()
    assert.ok(visibleText(view.root).includes(messages.knowledgeStages.stage.processing))
    assert.ok(visibleText(view.root).includes('1.3'))
  } finally { view.close() }
})

test('delete keeps failures readable, blocks departure while pending, and uses the personal document endpoint', async () => {
  const view = await mountDetail('en-US')
  try {
    await click(descendants(view.root).find(node => node.tag === 'button' && visibleText(node) === en.library.deleteDocument)!)
    const pending = view.confirmations.at(-1).onConfirm()
    await settle(); assert.equal(view.guard(), false)
    view.release(); await assert.rejects(pending)
    assert.ok(visibleText(view.root).includes('Delete failed'))
    assert.equal(view.pushes.length, 0)
    view.succeed()
    const retry = view.confirmations.at(-1).onConfirm()
    await settle(); view.release(); await retry
    assert.deepEqual(view.requests, ['article', 'article'])
    assert.equal(view.pushes.at(-1).path, '/platform/entries')
  } finally { view.close() }
  const personal = await mountDetail('en-US', 'mine')
  try {
    await click(descendants(personal.root).find(node => node.tag === 'button' && visibleText(node) === en.library.deleteDocument)!)
    await personal.confirmations.at(-1).onConfirm()
    assert.deepEqual(personal.requests, ['mine'])
  } finally { personal.close() }
})

test('content type, star, reprocessing and export use the stored material', async () => {
  const view = await mountDetail('en-US')
  const oldCreate = URL.createObjectURL, oldRevoke = URL.revokeObjectURL
  let download: Blob | undefined, fileName = ''
  try {
    const button = (label: string) => descendants(view.root).find(node => node.tag === 'button' && visibleText(node).trim() === label)!
    await click(button(`☆ ${en.articleJudgments.star}`))
    assert.ok(button(`★ ${en.articleJudgments.unstar}`))
    await click(button(en.entryDetail.changeType))
    descendants(view.root).find(node => node.tag === 'select')!.props.onChange({ target: { value: 'tutorial' } })
    await settle()
    await click(button(en.articleJudgments.saveContentType))
    assert.ok(view.requests.includes('tutorial'))
    assert.ok(!descendants(view.root).some(node => node.props.role === 'dialog'))
    await click(button(en.library.reprocess))
    await view.confirmations.at(-1).onConfirm()
    assert.ok(view.requests.includes('reparse'))
    URL.createObjectURL = blob => { download = blob as Blob; return 'blob:download' }
    URL.revokeObjectURL = () => {}
    globalThis.document.createElement = (() => ({ set download(value: string) { fileName = value }, click() {} })) as any
    await click(button(en.entryDetail.exportMarkdown))
    assert.equal(await download?.text(), '# Full body\n\nSaved 😀 body')
    assert.equal(fileName, 'Real title.md')
  } finally { URL.createObjectURL = oldCreate; URL.revokeObjectURL = oldRevoke; view.close() }
})

test('starring preserves the enabled button node while blocking duplicate submissions', async () => {
  let finish!: (value: any) => void, calls = 0
  const view = await mountDetail('en-US', 'article', () => { calls++; return new Promise(resolve => { finish = resolve }) })
  try {
    const button = descendants(view.root).find(node => node.tag === 'button' && node.props['aria-pressed'] === false)!
    await click(button)
    assert.equal(button.props.disabled, false, 'native disabling would drop focus in the browser')
    assert.equal(button.props['aria-disabled'], true)
    await click(button)
    assert.equal(calls, 1)
    finish({ success: true, data: { starred: true } }); await settle()
    assert.ok(descendants(view.root).find(node => node.tag === 'button' && node.props['aria-pressed'] === true) === button)
    assert.equal(button.props['aria-disabled'], undefined)
  } finally { view.close() }
})
