import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import { loadComponent, renderer, element, descendants, visibleText, click, settle } from './componentTestHarness'
import { entryPresentationTestModules } from './entryPresentationTestModules'
import * as bodyReference from '../../utils/bodyReference'
import * as searchHelpers from '../../utils/search'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import type { GraphSearchRequest, GraphSearchResponse } from '../../types/graph'

const location = { heading_path: ['Evidence'], paragraph: 7, start: 42, end: 68, body_hash: 'body-v1', mapping: 'exact' }
const result = (): GraphSearchResponse => ({
  status: 'ok', libraries: ['lib-b'], mode: 'neighbors', hops: 2, expansion_limit: 1000,
  candidates: {}, entities: [{ id: 's', canonical_name: 'Subject', entity_type: 'company' }, { id: 'o', canonical_name: 'Object', entity_type: 'product' }],
  edges: [{ id: 'edge', subject_id: 's', object_id: 'o', library_id: 'lib-b', relation_type: '发布', version: 1, source_count: 1,
    sources: [{ entry_id: 'entry', title: 'Source material', library_id: 'lib-b', library_name: 'Research', library_directory: 'research', published_at: null, source_unverified: true, acquisition: 'model_written', evidence: [{ locations: [location], speaker: { name: 'Speaker' } }] }] }],
  paths: [], documents: [], truncated: true, truncations: [{ reason: 'result_limit' }], pending_sync: { global: 3, involved: 2 },
})

async function fixture(locale: 'zh-CN' | 'en-US', search: (request: GraphSearchRequest) => Promise<unknown>) {
  const calls: GraphSearchRequest[] = []
  const pushes: unknown[] = []
  const component = loadComponent(new URL('./GraphSearchPanel.vue', import.meta.url), {
    ...entryPresentationTestModules,
    '@/utils/bodyReference': bodyReference,
    '@/utils/search': searchHelpers,
    '@/api/graph': { searchGraph: (request: GraphSearchRequest) => { calls.push(request); return search(request) } },
    'vue-router': { useRouter: () => ({ push: (location: unknown) => { pushes.push(location) } }) },
  })
  const app = renderer.createApp(component, { libraries: [{ id: 'lib-a', display_name: 'News' }, { id: 'lib-b', display_name: 'Research' }] })
  app.use(createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } }))
  const root = element('root')
  app.mount(root)
  await settle()
  const field = (name: string) => descendants(root).find(node => node.props.name === name)!
  const submit = async () => { descendants(root).find(node => node.tag === 'form')!.props.onSubmit({ preventDefault() {} }); await settle() }
  return { root, app, calls, pushes, field, submit }
}

for (const locale of ['zh-CN', 'en-US'] as const) test(`${locale}: a missing graph entity is an empty state with library context and correction advice`, async () => {
  const messages = locale === 'zh-CN' ? zh : en
  const f = await fixture(locale, async () => { throw { status: 404, error: { code: 1003, message: 'graph entity not found' } } })
  try {
    f.field('entity').props.onInput({ target: { value: 'Missing entity' } })
    f.field('library').props.onChange({ target: { value: 'lib-b' } })
    await f.submit()
    assert.ok(visibleText(f.root).includes(locale === 'zh-CN' ? '未找到该实体' : 'Entity not found'))
    assert.ok(visibleText(f.root).includes('Research'))
    assert.ok(visibleText(f.root).includes(locale === 'zh-CN' ? '检查名称' : 'Check the name'))
    assert.ok(!visibleText(f.root).includes(messages.searchPage.graphFailed))
    assert.ok(!descendants(f.root).some(node => node.tag === 'button' && visibleText(node) === messages.common.retry))
    f.field('entity').props.onInput({ target: { value: 'Another name' } })
    await settle()
    assert.ok(visibleText(f.root).includes(messages.searchPage.graphIdle))
  } finally { f.app.unmount() }
})

test('other graph failures remain errors with retry', async () => {
  for (const failure of [{ status: 404, error: { code: 1000 } }, { status: 503, error: { code: 1003 } }]) {
    const f = await fixture('zh-CN', async () => { throw failure })
    try {
      f.field('entity').props.onInput({ target: { value: 'Subject' } })
      await f.submit()
      assert.ok(visibleText(f.root).includes(zh.searchPage.graphFailed))
      assert.ok(descendants(f.root).some(node => node.tag === 'button' && visibleText(node) === zh.common.retry))
    } finally { f.app.unmount() }
  }
})

for (const locale of ['zh-CN', 'en-US'] as const) test(`${locale}: graph search sends one library and bounded hops, then opens each evidence location`, async () => {
  const f = await fixture(locale, async () => ({ success: true, data: result() }))
  try {
    f.field('entity').props.onInput({ target: { value: '  Subject  ' } })
    f.field('library').props.onChange({ target: { value: 'lib-b' } })
    f.field('hops').props.onChange({ target: { value: '2' } })
    await f.submit()
    assert.deepEqual(f.calls, [{ libraries: ['lib-b'], entity: { name: 'Subject' }, mode: 'neighbors', hops: 2, relation_types: [], limit: 50 }])
    const text = visibleText(f.root)
    for (const value of ['Subject', 'Object', '发布', 'Source material', '¶7', 'Speaker']) assert.ok(text.includes(value), value)
    assert.ok(text.includes(locale === 'zh-CN' ? '尚未同步' : 'awaiting sync'))
    assert.ok(text.includes(locale === 'zh-CN' ? '已截断' : 'truncated'))
    const link = descendants(f.root).find(node => node.tag === 'a' && visibleText(node).includes('Source material'))!
    await click(link)
    assert.deepEqual(f.pushes, [bodyReference.entryDetailLocation('entry', location)])
  } finally { f.app.unmount() }
})

test('graph search resolves ambiguous names with an explicit entity ID and sanitizes failures', async () => {
  let attempts = 0
  const f = await fixture('zh-CN', async () => {
    attempts++
    if (attempts === 1) return { success: true, data: { ...result(), status: 'needs_selection', edges: [], entities: [], candidates: { entity: [{ id: 'candidate-id', canonical_name: 'Candidate', entity_type: 'company' }] } } }
    if (attempts === 2) throw new Error('upstream secret')
    return { success: true, data: result() }
  })
  try {
    await f.submit()
    assert.equal(f.calls.length, 0)
    assert.ok(visibleText(f.root).includes(zh.searchPage.graphRequired))
    f.field('entity').props.onInput({ target: { value: 'Candidate' } })
    await f.submit()
    await click(descendants(f.root).find(node => node.tag === 'button' && visibleText(node).includes('candidate-id'))!)
    assert.deepEqual(f.calls[1]!.entity, { id: 'candidate-id' })
    assert.ok(visibleText(f.root).includes(zh.searchPage.graphFailed))
    assert.ok(!visibleText(f.root).includes('upstream secret'))
    await click(descendants(f.root).find(node => node.tag === 'button' && visibleText(node) === zh.common.retry)!)
    assert.deepEqual(f.calls[2]!.entity, { id: 'candidate-id' })
    assert.ok(visibleText(f.root).includes('Source material'))
  } finally { f.app.unmount() }
})

test('changing graph filters invalidates a pending response', async () => {
  let resolve!: (value: unknown) => void
  const pending = new Promise(done => { resolve = done })
  const f = await fixture('en-US', () => pending)
  try {
    f.field('entity').props.onInput({ target: { value: 'Subject' } })
    await f.submit()
    assert.equal(f.calls.length, 1)
    f.field('library').props.onChange({ target: { value: 'lib-b' } })
    resolve({ success: true, data: result() })
    await settle()
    assert.ok(!visibleText(f.root).includes('Source material'))
    assert.ok(visibleText(f.root).includes(en.searchPage.graphIdle))
  } finally { f.app.unmount() }
})

test('graph sources without evidence or positions remain readable without invented locations', async () => {
  for (const evidence of [null, [{ locations: null }]]) {
    const data = result()
    const source = { ...data.edges[0]!.sources[0]!, evidence }
    const f = await fixture('zh-CN', async () => ({ success: true, data: { ...data, edges: [{ ...data.edges[0], sources: [source] }] } }))
    try {
      f.field('entity').props.onInput({ target: { value: 'Subject' } })
      await f.submit()
      assert.ok(visibleText(f.root).includes(zh.searchPage.noPosition))
      assert.ok(visibleText(f.root).includes(zh.searchPage.noSpeaker))
      assert.ok(!visibleText(f.root).includes('¶'))
    } finally { f.app.unmount() }
  }
})
