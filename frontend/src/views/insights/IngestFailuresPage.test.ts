import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import { loadComponent, renderer, element, descendants, visibleText, click, settle } from '../library/componentTestHarness'

const failure = (id = 'f1') => ({ id, request_id: 'request-1', created_at: '2026-10-18T01:00:00Z', expires_at: '2099-10-25T01:00:00Z', transport: 'http', item_index: 0, stage: 'ingest', error_code: 'database_unavailable', reason: 'Database offline', retryable: true, outcome: 'unknown', package_status: 'partial', missing: ['model response'], entry_id: 'e1', materials: [{ id: 'input', name: 'input.json', type: 'application/json', size: 4, sha256: 'fixture' }, { id: 'picture', name: 'picture.png', type: 'image/png', size: 2, sha256: 'fixture' }], config: { model: 'fixture' } })
function environment() {
  const route = reactive({ query: {} as Record<string, any> }), calls: any[] = [], errors: unknown[] = []
  const state = { expired: false, pending: null as null | ((value: any) => void) }
  const i18n = createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en, 'zh-CN': zh }, warnHtmlMessage: false })
  const api = {
    async listIngestFailures(query: any) { calls.push(['list', query]); return { success: true, data: { items: [failure()], total: 21, page: query.page, page_size: 20 } } },
    async getIngestFailure(id: string) { calls.push(['detail', id]); return { success: true, data: failure(id) } },
    async readFailureText(id: string, material: string, offset: number) { calls.push(['text', id, material, offset]); if (state.expired) throw { status: 410 }; if (state.pending) return new Promise(resolve => { state.pending = resolve }); return { success: true, data: { content: offset ? '乙' : '甲', offset, length: 1, total_length: 2, next_offset: offset ? null : 1 } } },
    async readFailureImage(id: string, material: string) { calls.push(['image', id, material]); return new Blob(['image'], { type: 'image/png' }) },
  }
  const routeTo = async (to: any) => { route.query = Object.fromEntries(Object.entries(to.query).filter(([, value]) => value !== undefined)); await settle() }
  const component = loadComponent(new URL('./IngestFailuresPage.vue', import.meta.url), { 'vue-router': { useRoute: () => route, useRouter: () => ({ replace: routeTo, push: routeTo }) }, 'vue-i18n': { useI18n: () => i18n.global }, '@/api/ingest-diagnostics': api })
  const root = element('root'), app = renderer.createApp(component)
  for (const name of ['t-button', 't-loading', 't-input', 't-date-picker', 't-empty', 't-pagination', 'router-link']) app.component(name, defineComponent({ props: ['loading', 'disabled', 'to', 'value', 'description', 'current', 'pageSize', 'total'], setup: (props, { attrs, slots }) => () => h(name === 't-button' ? 'button' : name, { ...attrs, ...props }, [props.description, slots.default?.()]) }))
  app.config.errorHandler = e => errors.push(e); app.config.warnHandler = () => {}; app.mount(root)
  const button = (label: string) => { const match = descendants(root).find(n => n.tag === 'button' && visibleText(n).trim() === label); assert.ok(match, label); return match }
  return { root, app, calls, route, state, errors, button }
}

test('failure inspection keeps URL paging and reads segmented text and authenticated image without replay', async () => {
  const env = environment(), originalCreate = URL.createObjectURL, originalRevoke = URL.revokeObjectURL, released: string[] = []
  URL.createObjectURL = () => 'blob:failure'; URL.revokeObjectURL = value => released.push(value)
  try {
    await settle(); assert.match(visibleText(env.root), /Database offline/)
    const pagination = descendants(env.root).find(n => n.tag === 't-pagination')!; pagination.props.onCurrentChange(2); await settle()
    assert.equal(env.route.query.page, '2'); assert.equal(env.calls.filter(c => c[0] === 'list').at(-1)[1].page, 2)
    await click(env.button('View details')); assert.equal(env.route.query.failure, 'f1')
    assert.match(visibleText(env.root), /Outcome unknown/); assert.match(visibleText(env.root), /Some materials are missing/)
    await click(env.button('Read material')); assert.match(visibleText(env.root), /甲/)
    await click(env.button('Read next segment')); assert.match(visibleText(env.root), /甲乙/); assert.equal(env.calls.filter(c => c[0] === 'text').at(-1)[3], 1)
    const reads = descendants(env.root).filter(n => n.tag === 'button' && visibleText(n).trim() === 'Read material'); await click(reads[1]!)
    assert.ok(env.calls.some(c => c[0] === 'image' && c[2] === 'picture')); assert.equal(descendants(env.root).find(n => n.tag === 'img')?.props.src, 'blob:failure')
    await click(env.button('Back to failures')); assert.equal(env.route.query.page, '2'); assert.deepEqual(released, ['blob:failure']); assert.deepEqual(env.errors, [])
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
