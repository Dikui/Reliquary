import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../../../i18n/locales/en-US'
import zh from '../../../i18n/locales/zh-CN'
import { click, descendants, element, hasClass, loadComponent, registerPrimitives, renderer, settle, visibleText, type Element } from '../componentTestHarness'

const aurora = { id: 'a', canonical_name: 'Aurora' }, partner = { id: 'b', canonical_name: 'Partner' }
const edges = [
  { subject_entity_id: 'a', object_entity_id: 'b', latest_action: 'Partners', fact_count: 2 },
  { subject_entity_id: 'b', object_entity_id: 'a', latest_action: 'Responds', fact_count: 1 },
]
const envelope = (data: unknown) => ({ success: true, data })
const fact = {
  id: 'fact', story_id: 'story', title: 'A retained single-ended fact', subject: 'Aurora', action: 'Releases', object: 'Model',
  occurred_at: '2026-10-21', manual_corrected: true, no_source: true, sources: [],
}
function button(root: Element, label: string) {
  const found = descendants(root).find(node => node.tag === 'button' && visibleText(node) === label)
  assert.ok(found, `missing button ${label}`)
  return found
}
async function mount(locale: 'zh-CN' | 'en-US' = 'en-US', api: Record<string, unknown> = {}) {
  const calls: Array<{ kind: string; id: string; params: any }> = []
  const selected = ref(aurora), opened: string[] = []
  const component = loadComponent(new URL('./EntityActivity.vue', import.meta.url), {
    '@/api/entities': {
      getEntityGraph: async (id: string, params: unknown) => {
        calls.push({ kind: 'graph', id, params })
        return envelope({ center: id === 'a' ? aurora : partner, nodes: [aurora, partner], edges })
      },
      getEntityFacts: async (id: string, params: unknown) => {
        calls.push({ kind: 'facts', id, params })
        return envelope({ items: [{ ...fact, title: id === 'a' ? fact.title : 'Partner fact' }], total: 21 })
      }, ...api,
    },
  })
  const root = element('root')
  const app = renderer.createApp(defineComponent({ setup: () => () => h(component, {
    entity: selected.value, onSelect: (entity: typeof aurora) => { selected.value = entity },
    onOpenSource: (id: string) => opened.push(id),
  }) }))
  const copy = locale === 'en-US' ? en : zh
  app.use(createI18n({ legacy: false, locale, messages: { 'en-US': en, 'zh-CN': zh } }))
  registerPrimitives(app)
  for (const name of ['t-date-range-picker', 't-pagination']) app.component(name, defineComponent({ setup: () => () => h(name) }))
  app.component('t-empty', defineComponent({ props: ['description'], setup: props => () => h('p', props.description) }))
  app.component('router-link', defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { 'data-location': props.to }, slots.default?.()) }))
  app.config.warnHandler = warning => { throw new Error(warning) }
  app.mount(root); await settle()
  return { root, app, calls, copy, selected, opened }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: directed edges open their timeline and nodes recenter without losing the date range`, async () => {
    const env = await mount(locale)
    try {
      assert.ok(visibleText(env.root).includes(env.copy.entityActivity.graph))
      assert.ok(visibleText(env.root).includes(env.copy.stories.noSource))
      assert.ok(visibleText(env.root).includes('2026-10-21'))
      const link = descendants(env.root).find(node => node.tag === 'a')!
      assert.deepEqual(link.props['data-location'], { path: '/platform/stories/story', hash: '#fact-fact' })
      const date = descendants(env.root).find(node => node.tag === 't-date-range-picker')!
      date.props['onUpdate:modelValue'](['2026-10-20', '2026-10-24']); await settle()
      const edge = descendants(env.root).find(node => hasClass(node, 'graph-edge'))!
      await click(edge)
      assert.deepEqual(env.calls.at(-1), { kind: 'facts', id: 'a', params: {
        from: '2026-10-20', to: '2026-10-24', other_entity_id: 'b', direction: 'outgoing', limit: 20, offset: 0,
      } })
      const incoming = descendants(env.root).find(node => hasClass(node, 'graph-edge') && visibleText(node).includes('Responds'))!
      await click(incoming)
      assert.equal(env.calls.at(-1)!.params.direction, 'incoming')
      await click(button(env.root, 'Partner'))
      assert.equal(env.selected.value.id, 'b')
      assert.equal(env.calls.at(-1)!.id, 'b')
      assert.equal(env.calls.at(-1)!.params.from, '2026-10-20')
      assert.equal(env.calls.at(-1)!.params.other_entity_id, undefined)
      assert.ok(visibleText(env.root).includes('Partner fact'))
    } finally { env.app.unmount() }
  })
}

test('counterpart filtering resets the page and all facts clears the directed filter', async () => {
  const env = await mount()
  try {
    const paging = descendants(env.root).find(node => node.tag === 't-pagination')!
    paging.props['onUpdate:modelValue'](2); await settle()
    assert.equal(env.calls.at(-1)!.params.offset, 20)
    const select = descendants(env.root).find(node => node.props['aria-label'] === env.copy.entityActivity.counterpart)!
    select.props['onUpdate:modelValue']('b'); select.props.onChange(); await settle()
    assert.equal(env.calls.at(-1)!.params.offset, 0)
    assert.equal(env.calls.at(-1)!.params.other_entity_id, 'b')
    assert.equal(env.calls.at(-1)!.params.direction, undefined)
    await click(button(env.root, env.copy.entityActivity.allFacts))
    assert.equal(env.calls.at(-1)!.params.other_entity_id, undefined)
  } finally { env.app.unmount() }
})

test('late requests cannot replace the current center or update an unmounted view', async () => {
  let resolveGraph: (value: unknown) => void = () => {}, resolveFacts: (value: unknown) => void = () => {}
  const env = await mount('en-US', {
    getEntityGraph: (id: string) => id === 'a' ? new Promise(resolve => { resolveGraph = resolve }) : Promise.resolve(envelope({ center: partner, nodes: [partner], edges: [] })),
    getEntityFacts: (id: string) => id === 'a' ? new Promise(resolve => { resolveFacts = resolve }) : Promise.resolve(envelope({ items: [{ ...fact, title: 'Current partner fact' }], total: 1 })),
  })
  env.selected.value = partner; await settle()
  resolveGraph(envelope({ center: aurora, nodes: [aurora, partner], edges }))
  resolveFacts(envelope({ items: [fact], total: 1 })); await settle()
  assert.ok(visibleText(env.root).includes('Current partner fact'))
  assert.ok(!visibleText(env.root).includes(fact.title))
  assert.equal(descendants(env.root).filter(node => hasClass(node, 'graph-edge')).length, 0)
  env.selected.value = aurora; await settle(); env.app.unmount()
  resolveGraph(envelope({ center: aurora, nodes: [aurora], edges: [] }))
  resolveFacts(envelope({ items: [fact], total: 1 })); await settle()
  assert.equal(visibleText(env.root), '')
})

test('independent read failures retain readable facts and retry reaches the empty state', async () => {
  let phase = 0
  const env = await mount('en-US', {
    getEntityGraph: async () => { if (phase === 1) throw new Error(); return envelope({ center: aurora, nodes: [aurora], edges: [] }) },
    getEntityFacts: async () => { if (phase === 1) throw new Error(); return envelope({ items: phase === 0 ? [fact] : [], total: phase === 0 ? 1 : 0 }) },
  })
  try {
    assert.ok(visibleText(env.root).includes(env.copy.entityActivity.noEdges))
    assert.ok(visibleText(env.root).includes(fact.title))
    phase = 1
    await click(button(env.root, env.copy.common.refresh))
    assert.ok(visibleText(env.root).includes(env.copy.entityActivity.graphFailed))
    assert.ok(visibleText(env.root).includes(env.copy.entityActivity.factsFailed))
    assert.ok(visibleText(env.root).includes(fact.title), 'failed refresh retains readable facts')
    phase = 2
    const factAlert = descendants(env.root).find(node => node.props.role === 'alert' && visibleText(node).includes(env.copy.entityActivity.factsFailed))!
    await click(button(factAlert, env.copy.common.retry))
    assert.ok(visibleText(env.root).includes(env.copy.entityActivity.graphFailed), 'timeline retry does not replace the graph error')
    assert.ok(!visibleText(env.root).includes(env.copy.entityActivity.factsFailed))
    assert.ok(visibleText(env.root).includes(env.copy.entityActivity.noFacts))
  } finally { env.app.unmount() }
})
