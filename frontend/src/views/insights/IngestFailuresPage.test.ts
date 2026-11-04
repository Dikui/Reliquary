import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { diagnosticUI } from './diagnosticsTestSupport'
import * as reference from '../../utils/bodyReference'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import { loadComponent, renderer, element, descendants, visibleText, click, settle } from '../library/componentTestHarness'

const failure = (id = 'f1') => ({ id, request_id: 'request-1', created_at: '2026-10-18T01:00:00Z', expires_at: '2099-10-25T01:00:00Z', transport: 'http', item_index: 0, stage: 'ingest', error_code: 'database_unavailable', reason: 'Database offline', retryable: true, outcome: 'unknown', package_status: 'partial', missing: ['model response'], entry_id: 'e1', materials: [{ id: 'input', name: 'input.json', source: 'request', type: 'application/json', size: 4, sha256: 'fixture' }, { id: 'picture', name: 'picture.png', type: 'image/png', size: 2, sha256: 'fixture' }], config: { model: 'fixture' } })
function environment(options: { query?: Record<string, string>; api?: Record<string, any> } = {}) {
  const route = reactive({ query: { ...options.query } as Record<string, any> }), calls: any[] = [], errors: unknown[] = []
  const state = { expired: false, pending: null as null | ((value: any) => void) }
  const i18n = createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en, 'zh-CN': zh }, warnHtmlMessage: false })
  const api = {
    async listIngestFailures(query: any) { calls.push(['list', query]); return { success: true, data: { items: [failure()], total: 21, page: query.page, page_size: 20 } } },
    async getIngestFailure(id: string) { calls.push(['detail', id]); return { success: true, data: failure(id) } },
    async readFailureText(id: string, material: string, offset: number) { calls.push(['text', id, material, offset]); if (state.expired) throw { status: 410 }; if (state.pending) return new Promise(resolve => { state.pending = resolve }); return { success: true, data: { content: offset ? '乙' : '甲', offset, length: 1, total_length: 2, next_offset: offset ? null : 1 } } },
    async readFailureImage(id: string, material: string) { calls.push(['image', id, material]); return new Blob(['image'], { type: 'image/png' }) },
  }
  Object.assign(api, options.api)
  const routeTo = async (to: any) => { route.query = Object.fromEntries(Object.entries(to.query).filter(([, value]) => value !== undefined)); await settle() }
  const component = loadComponent(new URL('./IngestFailuresPage.vue', import.meta.url), { 'vue-router': { useRoute: () => route, useRouter: () => ({ replace: routeTo, push: routeTo }) }, 'vue-i18n': { useI18n: () => i18n.global }, '@/api/ingest-diagnostics': api, '@/components/ui': diagnosticUI(), '@/utils/bodyReference': reference, '@/api/library': { listLibraries: async () => ({ success: true, data: [] }) }, './FailedProcessingEntries.vue': { __esModule: true, default: defineComponent({ setup: () => () => h('section') }) } })
  const root = element('root'), app = renderer.createApp(component)
  for (const name of ['router-link']) app.component(name, defineComponent({ props: ['loading', 'disabled', 'to', 'value', 'description', 'current', 'pageSize', 'total'], setup: (props, { attrs, slots }) => () => h(name, { ...attrs, ...props }, [props.description, slots.default?.()]) }))
  app.use(i18n); app.config.errorHandler = e => errors.push(e); app.config.warnHandler = () => {}; app.mount(root)
  const button = (label: string) => { const match = descendants(root).find(n => n.tag === 'button' && visibleText(n).trim() === label); assert.ok(match, label); return match }
  return { root, app, calls, route, state, errors, button }
}

test('failure inspection keeps URL paging and reads segmented text and authenticated image without replay', async () => {
  const env = environment(), originalCreate = URL.createObjectURL, originalRevoke = URL.revokeObjectURL, released: string[] = []
  URL.createObjectURL = () => 'blob:failure'; URL.revokeObjectURL = value => released.push(value)
  try {
    await settle(); assert.match(visibleText(env.root), /Database offline/)
    assert.equal(env.calls.find(c => c[0] === 'list')[1].resolved, false)
    await click(env.button('Next'));  await settle()
    assert.equal(env.route.query.page, '2'); assert.equal(env.calls.filter(c => c[0] === 'list').at(-1)[1].page, 2)
    await click(env.button('View details')); assert.equal(env.route.query.failure, 'f1')
    assert.match(visibleText(env.root), /Outcome unknown/); assert.match(visibleText(env.root), /Some materials are missing/)
    await click(env.button('Read material')); assert.match(visibleText(env.root), /甲/)
    await click(env.button('Read next segment')); assert.match(visibleText(env.root), /甲乙/); assert.equal(env.calls.filter(c => c[0] === 'text').at(-1)[3], 1)
    await click(env.button('Original input')); await click(env.button('Read material'))
    assert.ok(env.calls.some(c => c[0] === 'image' && c[2] === 'picture')); assert.equal(descendants(env.root).find(n => n.tag === 'img')?.props.src, 'blob:failure')
    await click(env.button('Close')); assert.equal(env.route.query.page, '2'); assert.deepEqual(released, ['blob:failure']); assert.deepEqual(env.errors, [])
  } finally { env.app.unmount(); URL.createObjectURL = originalCreate; URL.revokeObjectURL = originalRevoke }
})

test('expired materials show an explicit error and an older response cannot reopen closed material content', async () => {
  const env = environment()
  try {
    await settle(); await click(env.button('View details')); env.state.expired = true
    await click(env.button('Read material')); assert.match(visibleText(env.root), /material expired or was removed/)
    env.state.expired = false; env.state.pending = () => {}; void click(env.button('Retry')); await settle()
    const finish = env.state.pending!; await click(env.button('Close material'))
    finish({ success: true, data: { content: 'STALE', offset: 0, length: 5, total_length: 5 } }); await settle()
    assert.ok(!visibleText(env.root).includes('STALE')); assert.deepEqual(env.errors, [])
  } finally { env.app.unmount() }
})


test('failure drawer preserves the list and related materials use the independent detail route', async () => {
  const env = environment()
  try {
    await settle(); await click(env.button('View details'))
    assert.ok(descendants(env.root).some(n => n.tag === 'table'))
    const link = descendants(env.root).find(n => n.tag === 'a' && n.props.href === '/platform/entries/e1')
    assert.ok(link)
    assert.equal(env.calls.some(c => c[0] === 'text' || c[0] === 'image'), false)
    assert.deepEqual(env.errors, [])
  } finally { env.app.unmount() }
})


test('failure filters retain every existing API field, validate exclusive dates and reset URL paging', async () => {
  const env = environment({ query: { page: '2' } })
  try {
    await settle()
    const values = ['2026-10-01', '2026-10-29', 'key-id', 'library-id', 'image_error', 'request-filter', 'failure-filter']
    descendants(env.root).filter(n => n.tag === 'input').forEach((node, i) => node.props.onInput({ target: { value: values[i] } }))
    await settle(); descendants(env.root).find(n => n.tag === 'form')!.props.onSubmit({ preventDefault() {} }); await settle()
    assert.equal(env.route.query.page, undefined)
    assert.deepEqual(env.calls.filter(c => c[0] === 'list').at(-1)[1], { page: 1, page_size: 20, resolved: false, from: new Date('2026-10-01T00:00:00').toISOString(), to: new Date('2026-10-29T00:00:00').toISOString(), api_key_id: 'key-id', library_id: 'library-id', error_code: 'image_error', request_id: 'request-filter', id: 'failure-filter' })
    const before = env.calls.length
    descendants(env.root).find(n => n.tag === 'input')!.props.onInput({ target: { value: '2026-10-30' } }); await settle()
    descendants(env.root).find(n => n.tag === 'form')!.props.onSubmit({ preventDefault() {} }); await settle()
    assert.match(visibleText(env.root), /end date must be after/)
    assert.equal(env.calls.length, before)
    await click(env.button('Reset filters'))
    assert.deepEqual(env.route.query, {})
    const resolution = descendants(env.root).find(n => n.tag === 'select')!
    resolution.props.onChange({ target: { value: 'all' } }); await settle()
    descendants(env.root).find(n => n.tag === 'form')!.props.onSubmit({ preventDefault() {} }); await settle()
    assert.equal(env.route.query.resolution, 'all')
    assert.equal(env.calls.filter(c => c[0] === 'list').at(-1)[1].resolved, undefined)
    resolution.props.onChange({ target: { value: 'resolved' } }); await settle()
    descendants(env.root).find(n => n.tag === 'form')!.props.onSubmit({ preventDefault() {} }); await settle()
    assert.equal(env.calls.filter(c => c[0] === 'list').at(-1)[1].resolved, true)
    assert.deepEqual(env.errors, [])
  } finally { env.app.unmount() }
})

test('missing failures keep a retry in the drawer and no stale material response survives closing', async () => {
  const env = environment({ query: { failure: 'missing' }, api: { getIngestFailure: async () => { throw { code: 1003 } } } })
  try {
    await settle(); assert.match(visibleText(env.root), /failure was not found/)
    assert.ok(env.button('Retry'))
    await click(env.button('Close')); assert.equal(env.route.query.failure, undefined)
    assert.deepEqual(env.errors, [])
  } finally { env.app.unmount() }
})
