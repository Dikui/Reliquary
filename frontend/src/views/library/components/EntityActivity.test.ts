import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../../../i18n/locales/en-US'
import zh from '../../../i18n/locales/zh-CN'
import { click, descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText, type Element } from '../componentTestHarness'

const aurora = { id: 'a', canonical_name: 'Aurora' }, partner = { id: 'b', canonical_name: 'Partner' }
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
  const calls: Array<{ id: string; params: any }> = []
  const selected = ref(aurora), opened: string[] = []
  const component = loadComponent(new URL('./EntityActivity.vue', import.meta.url), {
    '@/api/entities': {
      getEntityFacts: async (id: string, params: unknown) => {
        calls.push({ id, params })
        return envelope({ items: [{ ...fact, title: id === 'a' ? fact.title : 'Partner fact' }], total: 21 })
      }, ...api,
    },
  })
  const root = element('root')
  const app = renderer.createApp(defineComponent({ setup: () => () => h(component, {
    entity: selected.value,
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
  test(`${locale}: facts render with source state and the date range reloads the timeline`, async () => {
    const env = await mount(locale)
    try {
      assert.ok(visibleText(env.root).includes(env.copy.entityActivity.timeline))
      assert.ok(visibleText(env.root).includes(env.copy.stories.noSource))
      assert.ok(visibleText(env.root).includes('2026-10-21'))
      const link = descendants(env.root).find(node => node.tag === 'a')!
      assert.deepEqual(link.props['data-location'], { path: '/platform/stories/story', hash: '#fact-fact' })
      const date = descendants(env.root).find(node => node.tag === 't-date-range-picker')!
      date.props['onUpdate:modelValue'](['2026-10-20', '2026-10-24']); await settle()
      assert.deepEqual(env.calls.at(-1), { id: 'a', params: { from: '2026-10-20', to: '2026-10-24', limit: 20, offset: 0 } })
    } finally { env.app.unmount() }
  })
}

test('pagination requests the next page', async () => {
  const env = await mount()
  try {
    const paging = descendants(env.root).find(node => node.tag === 't-pagination')!
    paging.props['onUpdate:modelValue'](2); await settle()
    assert.equal(env.calls.at(-1)!.params.offset, 20)
  } finally { env.app.unmount() }
})

test('a failed refresh keeps readable facts and retry reaches the empty state', async () => {
  let phase = 0
  const env = await mount('en-US', {
    getEntityFacts: async () => { if (phase === 1) throw new Error(); return envelope({ items: phase === 0 ? [fact] : [], total: phase === 0 ? 1 : 0 }) },
  })
  try {
    phase = 1
    await click(button(env.root, env.copy.common.refresh))
    assert.ok(visibleText(env.root).includes(env.copy.entityActivity.factsFailed))
    assert.ok(visibleText(env.root).includes(fact.title), 'failed refresh retains readable facts')
    phase = 2
    const alert = descendants(env.root).find(node => node.props.role === 'alert' && visibleText(node).includes(env.copy.entityActivity.factsFailed))!
    await click(button(alert, env.copy.common.retry))
    assert.ok(!visibleText(env.root).includes(env.copy.entityActivity.factsFailed))
    assert.ok(visibleText(env.root).includes(env.copy.entityActivity.noFacts))
  } finally { env.app.unmount() }
})

test('late requests for a previous entity do not replace the current timeline', async () => {
  let resolveFacts: (value: unknown) => void = () => {}
  const env = await mount('en-US', {
    getEntityFacts: (id: string) => id === 'a' ? new Promise(resolve => { resolveFacts = resolve }) : Promise.resolve(envelope({ items: [{ ...fact, title: 'Current partner fact' }], total: 1 })),
  })
  env.selected.value = partner; await settle()
  resolveFacts(envelope({ items: [fact], total: 1 })); await settle()
  assert.ok(visibleText(env.root).includes('Current partner fact'))
  assert.ok(!visibleText(env.root).includes(fact.title))
})
