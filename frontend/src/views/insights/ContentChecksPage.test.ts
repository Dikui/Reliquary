import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import { reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../../i18n/locales/en-US'
import zh from '../../i18n/locales/zh-CN'
import { diagnosticUI } from './diagnosticsTestSupport'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../library/componentTestHarness'
import * as reference from '../../utils/bodyReference'
const location = { paragraph: 4, start: 30, end: 42, body_hash: 'original-hash', heading_path: [], mapping: 'exact' }
const run = (id = 'run-one') => ({ id, model_id: 'model-one', status: 'failed', total: 4, completed: 2, previous_id: '', targets: ['a', 'b', 'unfinished-one', 'unfinished-two'], error: 'context limit reached', created_at: '2026-10-27T03:00:00Z', results: [{ entry_id: 'a', findings: [{ kind: 'conflict', description: 'Actual conflict', evidence: [{ entry_id: 'material-one', title: 'Actual source', library_id: 'lib-one', quote: '<script>literal evidence</script>', location }] }] }] })
function mount(t: TestContext, api: Record<string, any> = {}) {
  const route = reactive({ query: {} as Record<string, any> }), calls: any[] = [], errors: unknown[] = [], notices: string[] = []
  const settings = { enabled: true, model_id: 'model-one', weekday: 1, time: '03:00', next_run_at: '2026-11-02T19:00:00Z' }
  const service = {
    checkSettings: async () => settings, checkRuns: async (page: number) => ({ items: [run(), run('run-two')], total: 22 }),
    checkRun: async (id: string) => { calls.push(['detail', id]); return run(id) },
    saveCheckSettings: async (draft: any) => { calls.push(['save', { ...draft }]); return draft },
    recheck: async (id: string) => { calls.push(['retry', id]); return { ...run('retry-run'), status: 'pending', completed: 0, results: [] } }, ...api,
  }
  const ui = { ...diagnosticUI(), toast: { success: (message: string) => notices.push(message) }, confirm: () => ({ result: Promise.resolve(true), destroy() {} }) }
  const component = loadComponent(new URL('./ContentChecksPage.vue', import.meta.url), {
    '@/components/ui': ui, '@/api/content-check': service, '@/utils/bodyReference': reference,
    '@/api/model': { listModels: async () => [{ id: 'model-one', name: 'Real model', status: 'active' }] },
    'vue-router': { useRoute: () => route, useRouter: () => ({ resolve: (to: any) => ({ href: '/platform/content-checks?' + new URLSearchParams(to.query).toString() }), push: async (to: any) => { route.query = to.query }, replace: async (to: any) => { route.query = to.query } }) },
  })
  const app = renderer.createApp(component), root = element('root')
  app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en, 'zh-CN': zh } }))
  app.config.errorHandler = error => errors.push(error); app.mount(root)
  t.after(() => app.unmount())
  const button = (label: string) => { const node = descendants(root).find(node => node.tag === 'button' && visibleText(node).trim() === label); assert.ok(node, label); return node }
  return { root, route, calls, errors, notices, button }
}

test('check records select the newest batch and preserve grouped evidence with exact detail references', async t => {
  const env = mount(t); await settle()
  assert.ok(visibleText(env.root).includes('Actual conflict'))
  assert.ok(visibleText(env.root).includes('Possible conflicts'))
  assert.ok(visibleText(env.root).includes('<script>literal evidence</script>'))
  assert.equal(descendants(env.root).some(n => n.tag === 'script'), false)
  const source = descendants(env.root).find(n => n.tag === 'a' && String(n.props.href).startsWith('/platform/entries/material-one?'))!
  assert.deepEqual(JSON.parse(new URL(source.props.href, 'https://example.test').searchParams.get('reference')!), location)
  assert.ok(descendants(env.root).some(n => n.tag === 'a' && n.props.href === '/platform/entries/unfinished-one'))
  assert.equal(env.calls[0][1], 'run-one')
  await click(env.button('Recheck unfinished materials')); await settle()
  assert.ok(env.calls.some(c => c[0] === 'retry' && c[1] === 'run-one'))
  assert.equal(env.route.query.run, 'retry-run')
  assert.deepEqual(env.errors, [])
})

test('schedule edits are isolated until save, use active models and validate enabled configuration', async t => {
  const env = mount(t); await settle(); await click(env.button('Weekly check settings'))
  const time = descendants(env.root).find(n => n.tag === 'input' && n.props.type === 'time')!
  time.props.onInput({ target: { value: '04:30' } }); await settle()
  await click(env.button('Cancel')); await click(env.button('Weekly check settings'))
  assert.equal(descendants(env.root).find(n => n.tag === 'input' && n.props.type === 'time')!.props.value, '03:00')
  const model = descendants(env.root).find(n => n.tag === 'select' && n.props['aria-label'] === 'Check model')!
  model.props.onChange({ target: { value: '' } }); await settle(); descendants(env.root).find(n => n.tag === 'form')!.props.onSubmit({ preventDefault() {} }); await settle()
  assert.equal(env.calls.some(c => c[0] === 'save'), false)
  assert.equal(model.props['aria-invalid'], true)
  assert.ok(model.props['aria-describedby'])
  model.props.onChange({ target: { value: 'model-one' } }); await settle(); descendants(env.root).find(n => n.tag === 'form')!.props.onSubmit({ preventDefault() {} }); await settle()
  assert.equal(env.calls.find(c => c[0] === 'save')[1].weekday, 1)
  assert.equal(env.notices.length, 1)
  assert.deepEqual(env.errors, [])
})

test('detail failures leave batch navigation and retry visible without showing old evidence', async t => {
  const env = mount(t, { checkRun: async (id: string) => { if (id === 'run-two') throw { status: 404 }; return run(id) } }); await settle()
  const next = descendants(env.root).find(n => n.tag === 'button' && n.props['data-run-id'] === 'run-two')!
  await click(next)
  assert.ok(!visibleText(env.root).includes('Actual conflict'))
  assert.ok(visibleText(env.root).includes('This check record no longer exists'))
  assert.ok(env.button('Retry'))
  assert.deepEqual(env.errors, [])
})

test('batch lists do not invent zero problems when the list endpoint omits reports', async t => {
  const summary = { ...run('run-two'), results: undefined }
  const env = mount(t, { checkRuns: async () => ({ items: [{ ...run(), results: undefined }, summary], total: 2 }) }); await settle()
  const row = descendants(env.root).find(n => n.tag === 'button' && n.props['data-run-id'] === 'run-two')!
  assert.match(visibleText(row), /2\/4/)
  assert.doesNotMatch(visibleText(row), /problems/)
  const current = descendants(env.root).find(n => n.tag === 'button' && n.props['data-run-id'] === 'run-one')!
  assert.match(visibleText(current), /1.*problems/)
})

test('recheck conflicts preserve evidence and explain that the run has changed', async t => {
  const env = mount(t, { recheck: async () => { throw { status: 409 } } }); await settle()
  await click(env.button('Recheck unfinished materials'))
  assert.match(visibleText(env.root), /already has a recheck/)
  assert.match(visibleText(env.root), /Actual conflict/)
  assert.equal(env.route.query.run, undefined)
})

test('stale run responses cannot replace the newly selected batch', async t => {
  let resolveOld: (value: any) => void = () => {}
  const env = mount(t, { checkRun: async (id: string) => id === 'run-one' ? new Promise(resolve => { resolveOld = resolve }) : { ...run(id), results: [] } })
  await settle()
  await click(descendants(env.root).find(n => n.tag === 'button' && n.props['data-run-id'] === 'run-two')!)
  resolveOld(run()); await settle()
  assert.ok(!visibleText(env.root).includes('Actual conflict'))
  assert.equal(env.route.query.run, 'run-two')
  assert.deepEqual(env.errors, [])
})

test('empty batches show a compact setup action with no empty detail layout', async t => {
  const env = mount(t, { checkRuns: async () => ({ items: [], total: 0 }) }); await settle()
  assert.ok(visibleText(env.root).includes('No checks yet'))
  assert.equal(descendants(env.root).some(n => String(n.props.class).includes('checks-layout')), false)
  assert.equal(env.calls.some(c => c[0] === 'detail'), false)
})

test('a failed page change does not present batches from the previous page', async t => {
  const env = mount(t, { checkRuns: async (page: number) => { if (page === 2) throw new Error('List failed'); return { items: [run()], total: 22 } } }); await settle()
  await click(env.button('Next'))
  assert.match(visibleText(env.root), /List failed/)
  assert.equal(descendants(env.root).some(n => n.props['data-run-id'] === 'run-one'), false)
  assert.equal(env.route.query.page, '2')
})
