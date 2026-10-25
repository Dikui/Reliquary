import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../../i18n/locales/en-US'
import zh from '../../i18n/locales/zh-CN'
import { descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from './componentTestHarness'

const messages = { 'en-US': en, 'zh-CN': zh }
const empty = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
const date = '2026-10-24T12:00:00Z'
const fact = (id: string) => ({ id, story_id: 'story', title: id, created_at: date, no_source: true, sources: [] })
const card = () => ({ story: { id: 'story', title: 'New progress', first_fact_at: date, last_fact_at: date }, trigger_fact: fact('Older trigger'), recent_facts: [fact('Latest fact'), fact('Second fact'), fact('Third fact')], matched_follows: [{ target_type: 'entity', target_id: 'entity', name: 'Institute' }] })

function environment(locale: keyof typeof messages) {
  const route = reactive({ query: {} as Record<string, any> })
  const calls: any[] = [], locations: any[] = [], errors: unknown[] = []
  const store = reactive({ targets: [{ target_type: 'entity', target_id: 'entity', name: 'Institute' }], targetsLoading: false, async loadTargets() {} })
  const transport = { fail: false, data: { results: [card()], total: 2, next_cursor: 'next', libraries: [{ library_id: 'lib', library_name: 'News', display_name: 'News' }] }, intercept: undefined as undefined | (() => Promise<any>) }
  const component = loadComponent(new URL('./FollowFeed.vue', import.meta.url), {
    '@/api/follows': { getFollowFeed: async (request: any) => { calls.push(structuredClone(request)); if (transport.intercept) return transport.intercept(); if (transport.fail) throw new Error('unavailable'); return { success: true, data: structuredClone(transport.data) } } },
    '@/stores/follows': { useFollowStore: () => store },
    '@/components/FollowToggle.vue': empty,
    '@/components/SearchEntry.vue': empty,
    'vue-router': { useRoute: () => route, useRouter: () => ({ replace: async (to: any) => { locations.push(to); route.query = to.query || {} }, push: async (to: any) => locations.push(to) }) },
  })
  const root = element('root'), app = renderer.createApp(component)
  app.use(createI18n({ legacy: false, locale, messages })); registerPrimitives(app)
  for (const name of ['t-alert', 't-empty', 't-loading']) app.component(name, defineComponent({ props: ['message', 'description', 'text'], setup: (props, { slots }) => () => h('div', [props.message, props.description, props.text, slots.default?.(), slots.operation?.()]) }))
  app.component('t-select', defineComponent({ props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue', 'change'], setup: (props, { emit }) => () => h('select', { options: props.options, value: props.modelValue, disabled: props.disabled, onChange: (value: any) => { emit('update:modelValue', value); emit('change', value) } }) }))
  app.component('router-link', defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { to: props.to }, slots.default?.()) }))
  app.config.errorHandler = error => errors.push(error)
  app.mount(root)
  const button = (label: string) => descendants(root).find(node => node.tag === 'button' && visibleText(node).trim() === label)!
  return { root, app, route, calls, store, locations, errors, transport, button }
}

for (const locale of ['en-US', 'zh-CN'] as const) {
  test(`${locale}: follow feed shows event, triggering fact, recent three and source state without viewing controls`, async () => {
    const env = environment(locale)
    try {
      await settle()
      const text = visibleText(env.root)
      for (const label of ['New progress', 'Older trigger', 'Latest fact', 'Second fact', 'Third fact', 'Institute']) assert.ok(text.includes(label), label)
      assert.ok(text.includes(messages[locale].stories.noSource))
      assert.ok(!text.includes(locale === 'en-US' ? 'Mark all as seen' : '全部标为已看'))
      assert.deepEqual(env.calls, [{ limit: 20 }])
      const link = descendants(env.root).find(node => node.tag === 'a' && visibleText(node) === 'Older trigger')!
      assert.deepEqual(link.props.to, { path: '/platform/stories/story', hash: '#fact-Older%20trigger' })
      assert.deepEqual(env.errors, [])
    } finally { env.app.unmount() }
  })
  test(`${locale}: filters, page retry and refresh preserve the correct cursor`, async () => {
    const env = environment(locale)
    try {
      await settle()
      env.transport.fail = true
      env.button(messages[locale].follows.nextPage).props.onClick(); await settle()
      assert.ok(visibleText(env.root).includes(messages[locale].follows.feedFailed))
      env.transport.fail = false
      env.button(messages[locale].common.retry).props.onClick(); await settle()
      assert.equal(env.calls.at(-1).cursor, 'next')
      env.button(messages[locale].follows.previousPage).props.onClick(); await settle()
      assert.equal(env.calls.at(-1).cursor, undefined)
      const selects = descendants(env.root).filter(node => node.tag === 'select')
      selects[0].props.onChange('lib'); await settle()
      assert.deepEqual(env.calls.at(-1).libraries, ['lib'])
      assert.equal(env.calls.at(-1).cursor, undefined)
      selects[1].props.onChange(['entity']); await settle()
      assert.deepEqual(env.calls.at(-1).target_ids, ['entity'])
      env.button(messages[locale].follows.refresh).props.onClick(); await settle()
      assert.equal(env.calls.at(-1).cursor, undefined)
      assert.deepEqual(env.errors, [])
    } finally { env.app.unmount() }
  })
}

test('a late feed response cannot overwrite a newer URL filter or an unmounted page', async () => {
  const env = environment('en-US')
  let unmounted = false
  try {
    await settle()
    let finish!: (value: unknown) => void
    env.transport.intercept = () => new Promise(resolve => { finish = resolve })
    env.button(en.follows.refresh).props.onClick(); await settle()
    env.transport.intercept = undefined
    env.transport.data.results[0].story.title = 'Filtered progress'
    env.route.query = { target_ids: 'entity' }; await settle()
    finish({ success: true, data: { ...env.transport.data, results: [card()] } }); await settle()
    assert.ok(visibleText(env.root).includes('Filtered progress'))
    assert.ok(!visibleText(env.root).includes('New progress'))
    env.transport.intercept = () => new Promise(resolve => { finish = resolve })
    env.button(en.follows.refresh).props.onClick(); await settle()
    env.app.unmount(); unmounted = true
    finish({ success: true, data: { ...env.transport.data, results: [card()] } }); await settle()
    assert.deepEqual(env.errors, [])
  } finally { if (!unmounted) env.app.unmount() }
})

test('empty feed offers story and entity entry points and preserves the scoped library label', async () => {
  const env = environment('en-US')
  try {
    await settle()
    env.store.targets = []
    env.transport.data.results = []
    env.transport.data.total = 0
    env.route.query = { library: 'lib' }; await settle()
    const links = descendants(env.root).filter(node => node.tag === 'a')
    assert.ok(links.some(link => link.props.to === '/platform/stories'))
    assert.ok(links.some(link => link.props.to === '/platform/entries?tab=entities'))
    assert.ok(visibleText(env.root).includes(en.follows.emptyTargets))
    const options = descendants(env.root).find(node => node.tag === 'select')!.props.options
    assert.deepEqual(options, [{ value: '', label: 'All libraries' }, { value: 'lib', label: 'News' }])
    assert.deepEqual(env.errors, [])
  } finally { env.app.unmount() }
})
