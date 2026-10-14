import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { loadComponent, renderer, element, descendants, visibleText, click, settle, type Element } from '../library/componentTestHarness.ts'
import zh from '../../i18n/locales/zh-CN.ts'
import en from '../../i18n/locales/en-US.ts'
import { metricKeys, metricLabels } from '../../api/insightMetrics.ts'
import { mapTrend, chartGeometry } from './trend.ts'

const metrics = (v: number) => Object.fromEntries(metricKeys.map(k => [k, v]))
const question = (extra = {}) => ({ id: 'q1', method: 'ordinary', status: 'confirmed', source: 'manual', revision: 1, answer_deleted: false, request: { query: 'Calibration?', libraries: ['lib1'], content_types: ['reference'] }, answers: [{ entry_id: 'e1', grade: 2 }], ...extra })
const run = (id: string, v: number, extra = {}) => ({ id, set_id: 'set1', set_snapshot: { id: 'set1', name: 'Calibration set' }, status: 'completed', options: { k: 5, vector_set: 'active', collapse_similar: false }, summary: metrics(v), snapshot: { revision: 'config-fixture' }, created_at: '2026-10-14T08:00:00Z', error: '', questions: [{ question_id: 'q1', question: question(), metrics: metrics(v), hits: ['e1'], results: { results: [{ entry_id: 'e1', title: 'Reference 1' }, { entry_id: 'e2', title: 'Reference 2' }] }, snapshot: { vector_weight: .7 }, error: '' }], ...extra })
const summary = (currency = 'USD', extra = {}) => ({ model_id: 'model1', purpose: 'body_embedding', period: '2026-10-14T00:00:00Z', currency, calls: 4, failures: 1, failure_rate: .25, average_duration_ms: 125.25, input_tokens: 100, output_tokens: null, cost: .001, unknown_input_calls: 1, unknown_output_calls: 4, unknown_cost_calls: 1, not_applicable_cost_calls: 1, ...extra })
function environment(locale = 'en-US') {
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en }, warnHtmlMessage: false }), route = reactive({ query: {} as Record<string, unknown> })
  const calls: { method: string; path: string; body: any; update?: boolean }[] = []
  const state = { sets: [{ id: 'set1', name: 'Calibration set', description: 'Hand checked' }], questions: [question()], records: [{ id: 'record1', query: 'Original search?', source: 'web', method: 'ordinary', libraries: ['lib1'], created_at: '2026-10-14T07:00:00Z', results: [{ entry_id: 'e1', rank: 1, score: .8 }, { entry_id: 'e2', rank: 2, score: .6 }], error: '' }], runs: [run('run1', .2), run('run2', .75)], summaries: [summary(), summary('CNY', { cost: null, unknown_cost_calls: 0, not_applicable_cost_calls: 4 })], details: [{ id: 'call1', requested_at: '2026-10-14T08:00:00Z', model_id: 'model1', model_name: 'Fixture', purpose: 'body_embedding', entry_id: 'e1', duration_ms: 125, success: true, error_category: '', input_tokens: 0, output_tokens: null, cost: null, cost_status: 'not_applicable', currency: 'USD', attempt: 2, retry_number: 3 }], total: 26, fail: (_method: string, _path: string): any => undefined }
  const comparison = { before_summary: metrics(.2), after_summary: metrics(.75), delta: metrics(.55), questions: [{ question_id: 'q1', question_changed: true, change: 'retained', before: run('run1', .2).questions[0], after: { ...run('run2', .75).questions[0], question: question({ revision: 2 }) }, delta: metrics(.55), added_hits: ['new-hit'], lost_hits: ['lost-hit'] }] }
  const capture = (method: string, path: string, body: any, update?: boolean) => { calls.push({ method, path, body: body && JSON.parse(JSON.stringify(body)), update }); const error = state.fail(method, path); if (error) throw error }
  const api = { metricKeys, metricLabels,
    async read(path: string, body = {}) {
      capture('get', path, body)
      if (path === 'libraries') return [{ id: 'lib1', name: 'My materials' }, { id: 'lib2', name: 'Other library' }]
      if (path === 'models') return [{ id: 'model1', name: 'Fixture', type: 'KnowledgeQA' }, { id: 'rerank1', name: 'Rerank fixture', type: 'Rerank' }]
      if (path === 'evaluation/sets') return state.sets
      if (path.endsWith('/questions')) return state.questions.filter(q => q.status === (body as any).status)
      if (path === 'search/records') return state.records
      if (path === 'evaluation/runs') return state.runs
      if (path === 'evaluation/vector-sets') return [{ selector: 'active', model_id: 'current', dimension: 3 }, { selector: 'candidate:next', model_id: 'next', dimension: 3 }, { selector: 'archive:old', model_id: 'old', dimension: 3 }]
      if (path.startsWith('evaluation/runs/')) return state.runs.find(r => r.id === path.split('/').at(-1))
      if (path === 'evaluation/compare') return comparison
      if (path === 'entries/search') return [{ id: 'e1', title: 'Selected material', library_id: 'lib1' }, { id: 'outside', title: 'Outside library', library_id: 'lib2' }]
      if (path === 'model-calls/purposes') return ['body_embedding', 'evaluation_generation']
      if (path === 'model-calls/summary' || path === 'model-calls/trend') return state.summaries
      if (path === 'entries/e1') return { title: 'Selected material' }
      throw new Error(`Unexpected GET ${path}`)
    },
    async write(path: string, body: any, update = false) {
      capture('write', path, body, update)
      if (/^evaluation\/sets(?:\/set\d)?$/.test(path)) { const saved = { id: 'set1', ...body }; state.sets = [saved]; return saved }
      if (path.endsWith('/confirm')) { state.questions = state.questions.map(q => ({ ...q, status: 'confirmed' })); return state.questions[0] }
      if (path.endsWith('/candidates')) { state.questions = [question({ id: 'pending1', status: 'pending', source: 'ai' })]; return state.questions }
      if (path.endsWith('/from-record')) { state.questions.push(question({ id: 'converted', answers: body.answers })); return state.questions.at(-1) }
      if (path.endsWith('/runs')) return state.runs[0]
      if (path.includes('/questions')) return question(body)
      throw new Error(`Unexpected WRITE ${path}`)
    },
    async remove(path: string) { capture('delete', path, {}); if (path === 'evaluation/sets/set1') state.sets = []; else state.questions = []; return {} },
    async callDetails(body: any) { capture('get', 'model-calls', body); return { data: state.details, total: state.total } },
  }
  const modules: Record<string, unknown> = { 'vue-i18n': { useI18n: () => i18n.global }, 'vue-router': { useRoute: () => route, useRouter: () => ({ replace: async (to: any) => { route.query = to.query } }) }, '@/api/insights': api, './insights.css': {}, './trend': { mapTrend, chartGeometry } }
  function component(name: string) { return loadComponent(new URL(name, import.meta.url), modules) }
  for (const name of ['EntryPicker', 'QuestionEditor', 'RunResults', 'TrendChart']) modules[`./${name}.vue`] = component(`./${name}.vue`)
  function mount(name: string, props = {}) {
    const root = element('root'), errors: unknown[] = [], warnings: string[] = [], app = renderer.createApp(component(name), props)
    app.config.errorHandler = e => errors.push(e); app.config.warnHandler = w => warnings.push(w); app.mount(root)
    return { root, errors, warnings, close: () => app.unmount() }
  }
  return { mount, state, calls, route, comparison, t: i18n.global.t }
}
const button = (root: Element, text: string) => { const b = descendants(root).find(n => n.tag === 'button' && visibleText(n).trim() === text); assert.ok(b, `Missing button ${text}`); return b }
const control = (root: Element, label: string, tag = 'input') => { const n = descendants(root).find(n => n.tag === 'label' && visibleText(n).startsWith(label))?.children.find(n => n.tag === tag); assert.ok(n, `Missing control ${label}`); return n }
async function input(node: Element, value: string | number) { node.props.onInput?.({ target: { value: String(value) } }); await settle() }
async function change(node: Element, value: string | boolean) { node.props.onChange?.({ target: { value, checked: value } }); await settle() }
async function submit(root: Element) { const form = descendants(root).find(n => n.tag === 'form')!; assert.ok(form); form.props.onSubmit({ preventDefault() {} }); await settle() }
const check = (root: Element, text?: string) => text ? descendants(root).find(n => n.tag === 'label' && visibleText(n).includes(text))!.children.find(n => n.tag === 'input')! : descendants(root).find(n => n.tag === 'input' && n.props.type === 'checkbox')!

test('set creation, editing and confirmed deletion preserve the history explanation', async () => {
  const env = environment(), m = env.mount('./EvaluationPage.vue')
  try {
    await settle(); await click(button(m.root, 'New set')); await input(control(m.root, 'Name'), 'New evaluation'); await input(control(m.root, 'Description', 'textarea'), 'Scope'); await submit(m.root)
    assert.deepEqual(env.calls.find(c => c.method === 'write')?.body, { name: 'New evaluation', description: 'Scope' })
    await click(button(m.root, 'Edit set')); await input(control(m.root, 'Name'), 'Renamed'); await submit(m.root); assert.equal(env.calls.filter(c => c.method === 'write').at(-1)?.update, true)
    await click(button(m.root, 'Delete set')); assert.ok(visibleText(m.root).includes('Historical runs are retained.')); assert.ok(!env.calls.some(c => c.method === 'delete')); await click(button(m.root, 'Confirm delete')); assert.ok(env.calls.some(c => c.method === 'delete')); assert.ok(visibleText(m.root).includes('No evaluation sets yet.')); assert.deepEqual(m.errors, [])
  } finally { m.close() }
})
for (const locale of ['zh-CN', 'en-US']) test(`${locale}: question editing saves complete request, libraries and relevance grades`, async () => {
  const env = environment(locale), m = env.mount('./QuestionEditor.vue', { setId: 'set1', question: question(), libraries: [{ id: 'lib1', name: 'My materials' }] })
  try {
    await input(control(m.root, env.t('ikb19.query'), 'textarea'), 'Revised?'); await input(descendants(m.root).find(n => n.tag === 'input' && n.props.type === 'number')!, 8); await submit(m.root)
    const saved = env.calls.find(c => c.method === 'write')!; assert.equal(saved.path, 'evaluation/sets/set1/questions/q1'); assert.equal(saved.update, true); assert.deepEqual(saved.body, { method: 'ordinary', request: { query: 'Revised?', libraries: ['lib1'], content_types: ['reference'], collapse_similar: false }, answers: [{ entry_id: 'e1', grade: 8 }] }); assert.deepEqual(m.errors, [])
  } finally { m.close() }
})
test('manual question uses selected-library material search and graph parameters', async () => {
  const env = environment(), m = env.mount('./QuestionEditor.vue', { setId: 'set1', libraries: [{ id: 'lib1', name: 'My materials' }] })
  try {
    await input(control(m.root, 'Question', 'textarea'), 'New?'); await change(check(m.root), true); await input(control(m.root, 'Search materials'), 'Calibration'); await click(button(m.root, 'Search')); assert.ok(visibleText(m.root).includes('Selected material')); assert.ok(!visibleText(m.root).includes('Outside library'))
    await change(check(m.root, 'Selected material'), true); await change(control(m.root, 'Search method', 'select'), 'graph'); await input(control(m.root, 'Graph parameters JSON', 'textarea'), '{"entity_ids":["entity1"],"hops":2}'); await submit(m.root)
    const saved = env.calls.find(c => c.method === 'write')!; assert.equal(saved.update, false); assert.deepEqual(saved.body.answers, [{ entry_id: 'e1', grade: 1 }]); assert.deepEqual(saved.body.request.graph_parameters, { entity_ids: ['entity1'], hops: 2 }); assert.deepEqual(m.errors, [])
  } finally { m.close() }
})
test('deleted reference warnings and save failure retain the draft for repair', async () => {
  const env = environment(); env.state.fail = method => method === 'write' ? { message: 'Missing reference' } : undefined
  const m = env.mount('./QuestionEditor.vue', { setId: 'set1', question: question({ answer_deleted: true }), libraries: [{ id: 'lib1', name: 'My materials' }] })
  try { assert.ok(visibleText(m.root).includes('A reference material was deleted.')); await input(control(m.root, 'Question', 'textarea'), 'Still edited'); await submit(m.root); assert.ok(visibleText(m.root).includes('Missing reference')); assert.equal(control(m.root, 'Question', 'textarea').props.value, 'Still edited'); env.state.fail = () => undefined; await submit(m.root); assert.equal(env.calls.filter(c => c.method === 'write').length, 2); assert.deepEqual(m.errors, []) } finally { m.close() }
})
test('record filters default to excluding evaluation and conversion sends only checked answers', async () => {
  const env = environment(), m = env.mount('./EvaluationPage.vue')
  try {
    await settle(); await click(button(m.root, 'Search records')); assert.equal(env.calls.find(c => c.path === 'search/records')?.body.include_evaluation, false)
    await change(control(m.root, 'Source', 'select'), 'mcp'); await change(control(m.root, 'Search method', 'select'), 'graph'); await change(control(m.root, 'Libraries', 'select'), 'lib1'); await input(control(m.root, 'Question'), 'Original'); await input(control(m.root, 'From'), '2026-10-01T00:00'); await click(button(m.root, 'Apply filters'))
    const params = env.calls.filter(c => c.path === 'search/records').at(-1)!.body; assert.equal(params.source, 'mcp'); assert.equal(params.method, 'graph'); assert.equal(params.library, 'lib1'); assert.equal(params.query, 'Original'); assert.ok(params.from.endsWith('Z'))
    await click(button(m.root, 'Create question from record')); await change(check(m.root, '#2'), true); await click(button(m.root, 'Save question')); assert.deepEqual(env.calls.find(c => c.path.endsWith('/from-record'))?.body, { record_id: 'record1', answers: [{ entry_id: 'e2', grade: 1 }] }); assert.deepEqual(m.errors, [])
  } finally { m.close() }
})
test('AI generation sends scope and count, shows pending questions, then supports confirmation and deletion', async () => {
  const env = environment(), m = env.mount('./EvaluationPage.vue')
  try {
    await settle(); await click(button(m.root, 'AI candidates')); await change(check(m.root), true); await input(control(m.root, 'Search materials'), 'calibration'); await click(button(m.root, 'Search')); await change(check(m.root, 'Selected material'), true); await input(control(m.root, 'Question count'), 2); await change(control(m.root, 'Model', 'select'), 'model1'); await submit(m.root)
    assert.deepEqual(env.calls.find(c => c.path.endsWith('/candidates'))?.body, { libraries: ['lib1'], entry_ids: ['e1'], count: 2, model_id: 'model1' }); assert.equal(control(m.root, 'Confirmation status', 'select').props.value, 'pending')
    await click(button(m.root, 'Confirm candidate')); assert.ok(env.calls.some(c => c.path.endsWith('/pending1/confirm'))); assert.ok(visibleText(m.root).includes('No pending questions.')); await change(control(m.root, 'Confirmation status', 'select'), 'confirmed'); await click(button(m.root, 'Delete')); await click(button(m.root, 'Confirm delete')); assert.ok(env.calls.some(c => c.path.endsWith('/pending1') && c.method === 'delete')); assert.deepEqual(m.errors, [])
  } finally { m.close() }
})
test('run form gets active, candidate, archived selectors and sends exact overrides', async () => {
  const env = environment(), m = env.mount('./EvaluationPage.vue')
  try { await settle(); await click(button(m.root, 'Runs and comparison')); assert.ok(env.calls.some(c => c.path === 'evaluation/vector-sets')); assert.equal(descendants(control(m.root, 'Vector set', 'select')).filter(n => n.tag === 'option').length, 3); await input(control(m.root, 'K (1 to 100)'), 8); await change(control(m.root, 'Vector set', 'select'), 'candidate:next'); await change(control(m.root, 'Rerank model', 'select'), ''); await change(check(m.root), true); await submit(m.root); assert.deepEqual(env.calls.find(c => c.path.endsWith('/runs') && c.method === 'write')?.body, { k: 8, vector_set: 'candidate:next', collapse_similar: true, rerank_model_id: '' }); assert.ok(visibleText(m.root).includes('0.2000')); assert.deepEqual(m.errors, []) } finally { m.close() }
})
test('details and comparison render five means, signed deltas, revision changes and added or lost hits', () => {
  const env = environment(), m = env.mount('./RunResults.vue', { run: run('run1', .2), comparison: env.comparison })
  try { const text = visibleText(m.root); for (const label of metricLabels) assert.ok(text.includes(label)); for (const value of ['0.2000', '0.7500', '+0.5500', 'Question revised', 'new-hit', 'lost-hit', 'config-fixture', 'vector_weight', 'Hit', 'Miss']) assert.ok(text.includes(value), value); assert.deepEqual(m.errors, []) } finally { m.close() }
})
test('comparison requests selected completed runs in order', async () => {
  const env = environment(), m = env.mount('./EvaluationPage.vue')
  try { await settle(); await click(button(m.root, 'Runs and comparison')); await change(control(m.root, 'Before run', 'select'), 'run1'); await change(control(m.root, 'After run', 'select'), 'run2'); await click(button(m.root, 'Compare')); assert.deepEqual(env.calls.find(c => c.path === 'evaluation/compare')?.body, { before: 'run1', after: 'run2' }); assert.ok(visibleText(m.root).includes('+0.5500')); assert.deepEqual(m.errors, []) } finally { m.close() }
})
for (const locale of ['zh-CN', 'en-US']) test(`${locale}: calls filters and summaries display partial tokens, unknowns, subscription costs and separate currencies`, async () => {
  const env = environment(locale), m = env.mount('./ModelCallsPage.vue')
  try {
    await settle(); const text = visibleText(m.root); for (const v of ['USD 0.001000', 'CNY', env.t('ikb19.unknown'), env.t('ikb19.notApplicable'), env.t('ikb19.partial'), '25.00%', '125.3 ms']) assert.ok(text.includes(v), v)
    await input(control(m.root, env.t('ikb19.from')), '2026-10-01'); await input(control(m.root, env.t('ikb19.toExclusive')), '2026-11-01'); await change(control(m.root, env.t('ikb19.model'), 'select'), 'model1'); await change(control(m.root, env.t('ikb19.purpose'), 'select'), 'evaluation_generation'); await submit(m.root)
    const p = env.calls.filter(c => c.path === 'model-calls').at(-1)!.body; assert.equal(p.from, '2026-10-01T00:00:00.000Z'); assert.equal(p.to, '2026-11-01T00:00:00.000Z'); assert.equal(p.model_id, 'model1'); assert.equal(p.purpose, 'evaluation_generation')
    for (const period of ['week', 'month']) { await change(control(m.root, env.t('ikb19.period'), 'select'), period); assert.equal(env.calls.filter(c => c.path.endsWith('/summary')).at(-1)?.body.period, period) }
    await click(button(m.root, env.t('ikb19.next'))); assert.equal(env.calls.filter(c => c.path === 'model-calls').at(-1)?.body.offset, 25); assert.deepEqual(m.errors, [])
  } finally { m.close() }
})
test('material scope applies to every statistics request and can be cleared', async () => {
  const env = environment(); env.route.query.entry_id = 'e1'; const m = env.mount('./ModelCallsPage.vue')
  try { await settle(); assert.ok(visibleText(m.root).includes('Selected material')); assert.ok(env.calls.filter(c => c.path.startsWith('model-calls') && c.path !== 'model-calls/purposes').every(c => c.body.entry_id === 'e1')); await click(button(m.root, 'Show calls for all materials')); assert.ok(!env.calls.filter(c => c.path === 'model-calls').at(-1)?.body.entry_id); assert.deepEqual(m.errors, []) } finally { m.close() }
})
test('trend mapping separates currencies, inserts zero days and retains null cost gaps', () => {
  const data = [summary('USD', { period: '2026-10-01T00:00:00Z', calls: 2, input_tokens: 0, output_tokens: null, cost: 0 }), summary('USD', { period: '2026-10-03T00:00:00Z', cost: null }), summary('CNY', { period: '2026-10-01T00:00:00Z', cost: .02 })], mapped = mapTrend(data, 'cost')
  assert.deepEqual(mapped.map(s => s.currency), ['CNY', 'USD']); assert.deepEqual(mapped[1]!.points.map(p => p.value), [0, 0, null]); assert.equal(mapTrend(data, 'tokens')[1]!.points[0]!.value, 0); assert.equal(mapTrend(data, 'calls')[1]!.points[0]!.value, 2)
  const g = chartGeometry(mapped[1]!.points, .02); assert.equal(g.points[0]!.y, 210); assert.equal(g.points[2]!.y, null); assert.equal(g.segments.length, 1)
})
test('SVG chart has mapped series, accessible data table and partial value annotations', () => {
  const env = environment(), m = env.mount('./TrendChart.vue', { rows: env.state.summaries, metric: 'cost' })
  try { assert.equal(descendants(m.root).filter(n => n.tag === 'svg' && n.props.role === 'img').length, 1); assert.equal(descendants(m.root).filter(n => n.props['data-currency']).length, 2); assert.ok(visibleText(m.root).includes('View chart data')); assert.ok(visibleText(m.root).includes('Partial total')); assert.deepEqual(m.errors, []) } finally { m.close() }
})
for (const empty of ['sets', 'questions', 'pending', 'records', 'runs', 'calls', 'trend']) test(`empty state offers next step: ${empty}`, async () => {
  const env = environment(); if (empty === 'sets') env.state.sets = []; if (empty === 'questions' || empty === 'pending') env.state.questions = []; if (empty === 'records') env.state.records = []; if (empty === 'runs') env.state.runs = []; if (empty === 'calls') { env.state.total = 0; env.state.details = []; env.state.summaries = [] }
  const m = env.mount(empty === 'calls' ? './ModelCallsPage.vue' : empty === 'trend' ? './TrendChart.vue' : './EvaluationPage.vue', empty === 'trend' ? { rows: [], metric: 'calls' } : {})
  try { await settle(); if (empty === 'records') await click(button(m.root, 'Search records')); if (empty === 'runs') await click(button(m.root, 'Runs and comparison')); if (empty === 'pending') await change(control(m.root, 'Confirmation status', 'select'), 'pending'); const key = ({ sets: 'noSets', questions: 'noQuestions', pending: 'noCandidates', records: 'noRecords', runs: 'noRuns', calls: 'noCalls', trend: 'noTrend' } as Record<string, string>)[empty]; assert.ok(visibleText(m.root).includes(env.t(`ikb19.${key}`))); assert.deepEqual(m.errors, []) } finally { m.close() }
})
for (const page of ['./EvaluationPage.vue', './ModelCallsPage.vue']) test(`${page}: failed API has retry and recovers without false empty states`, async () => {
  const env = environment(); env.state.fail = () => ({ message: 'Service unavailable' }); const m = env.mount(page)
  try { await settle(); assert.ok(visibleText(m.root).includes('Service unavailable')); assert.ok(!visibleText(m.root).includes('No matching calls.')); assert.ok(!visibleText(m.root).includes('No evaluation sets yet.')); env.state.fail = () => undefined; await click(button(m.root, page.includes('Evaluation') ? 'Refresh' : 'Retry')); assert.ok(!visibleText(m.root).includes('Service unavailable')); assert.deepEqual(m.errors, []) } finally { m.close() }
})
test('graph 503 retains run parameters and offers graph service recovery steps', async () => {
  const env = environment(); env.state.fail = (method, path) => method === 'write' && path.endsWith('/runs') ? { status: 503 } : undefined; const m = env.mount('./EvaluationPage.vue')
  try { await settle(); await click(button(m.root, 'Runs and comparison')); await input(control(m.root, 'K (1 to 100)'), 7); await submit(m.root); assert.ok(visibleText(m.root).includes('Graph search is unavailable (503).')); assert.equal(control(m.root, 'K (1 to 100)').props.value, 7); assert.equal(button(m.root, 'Start run').props.disabled, false); assert.deepEqual(m.errors, []) } finally { m.close() }
})
test('failed and running details show errors and next actions', () => {
  const env = environment(), bad = env.mount('./RunResults.vue', { run: run('bad', 0, { status: 'failed', error: 'Model timeout' }) }), running = env.mount('./RunResults.vue', { run: run('active', 0, { status: 'running' }) })
  try { assert.ok(visibleText(bad.root).includes('Model timeout')); assert.ok(visibleText(bad.root).includes('start another run')); assert.ok(visibleText(running.root).includes('Refresh details to check progress')); assert.deepEqual([...bad.errors, ...running.errors], []) } finally { bad.close(); running.close() }
})
test('narrow layout contains scrolling tables and responds to theme tokens', () => {
  const css = readFileSync(new URL('./insights.css', import.meta.url), 'utf8'); assert.match(css, /\.table-scroll[^}]*max-width: 100%;[^}]*overflow-x: auto/); assert.match(css, /@media \(max-width: 640px\)/); assert.match(css, /grid-template-columns: minmax\(0, 1fr\)/); assert.match(css, /var\(--td-bg-color-container\)/); assert.match(css, /min-width: 0/)
})
