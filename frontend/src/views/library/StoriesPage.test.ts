import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../../i18n/locales/en-US'
import zh from '../../i18n/locales/zh-CN'
import { descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from './componentTestHarness'

const messages = { 'en-US': en, 'zh-CN': zh }
const arrivedAt = '2026-10-24T16:00:00Z'

function loadStoriesPage(modules: Record<string, unknown>) {
  return loadComponent(new URL('./StoriesPage.vue', import.meta.url), {
    '@/api/entities': { listEntities: async () => ({ success: true, data: [] }) },
    '@/api/domains': { listDomains: async () => ({ success: true, data: [] }) },
    './components/EntityDomainSelect.vue': { __esModule: true, default: loadComponent(new URL('./components/EntityDomainSelect.vue', import.meta.url), {}) },
    ...modules,
  })
}

for (const locale of ['en-US', 'zh-CN'] as const) {
  test(`${locale}: related stories are links with only one level of expansion`, async () => {
    const previousDocument = globalThis.document
    globalThis.document = { title: '' } as Document
    const route = reactive({ params: { storyId: 'story' }, query: { page: '2' }, hash: '' })
    const reads: string[] = []
    const detail = {
      story: { id: 'story', title: 'Model release', first_fact_at: arrivedAt, last_fact_at: arrivedAt },
      facts: [], mentions: [], related_stories: [{
        id: 'nearby/story', title: 'Independent evaluation', first_fact_at: arrivedAt, last_fact_at: arrivedAt,
        related_stories: [{ id: 'distant', title: 'Second level must stay hidden' }],
      }],
    }
    const component = loadStoriesPage({
      '@/components/FollowToggle.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
      '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) },
      './components/GroupingCorrection.vue': defineComponent({ setup: () => () => null }),
      'vue-router': { useRoute: () => route, useRouter: () => ({ replace() {} }) },
      '@/api/stories': { getStory: async (id: string) => { reads.push(id); return { success: true, data: detail } } },
    })
    const root = element('root'), app = renderer.createApp(component)
    app.use(createI18n({ legacy: false, locale, messages }))
    registerPrimitives(app)
    app.component('router-link', defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { to: props.to }, slots.default?.()) }))
    for (const name of ['t-empty', 't-pagination']) app.component(name, defineComponent({ setup: () => () => null }))
    try {
      app.mount(root); await settle()
      const links = descendants(root).filter(node => node.tag === 'a' && visibleText(node) === 'Independent evaluation')
      assert.equal(links.length, 1)
      assert.deepEqual(links[0].props.to, { path: '/platform/stories/nearby%2Fstory', query: { page: '2' } })
      assert.ok(visibleText(root).includes(locale === 'en-US' ? 'Related stories' : '相关事件'))
      assert.ok(!visibleText(root).includes('Second level must stay hidden'))
      assert.deepEqual(reads, ['story'])
    } finally { app.unmount(); await settle(); globalThis.document = previousDocument }
  })
}

for (const locale of ['en-US', 'zh-CN'] as const) {
  for (const scenario of [
    { name: 'date', occurredAt: '2026-10-20', expected: '2026-10-20' },
    {
      name: 'timestamp', occurredAt: '2026-10-20T08:30:00+08:00',
      expected: locale === 'en-US' ? '10/19/2026, 8:30:00 PM EDT' : '2026/10/19 GMT-4 20:30:00',
    },
  ]) {
    test(`${locale}: story ${scenario.name} keeps its occurrence precision in New York`, async () => {
      const previousTZ = process.env.TZ, previousDocument = globalThis.document
      process.env.TZ = 'America/New_York'
      globalThis.document = { title: '' } as Document
      const route = reactive({ params: { storyId: 'story' }, query: {} })
      const detail = {
        story: { id: 'story', title: 'Model release', first_fact_at: arrivedAt, last_fact_at: arrivedAt },
        facts: [{
          id: 'fact', title: 'Release', subject: 'Aurora', action: 'released', object: 'Model A', conditions: '',
          occurred_at: scenario.occurredAt, created_at: arrivedAt, sources: [], manual_corrected: true, no_source: true,
        }],
      }
      const component = loadStoriesPage({
        '@/components/FollowToggle.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
      '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) },
      './components/GroupingCorrection.vue': defineComponent({ setup: () => () => null }),
        'vue-router': { useRoute: () => route, useRouter: () => ({ replace() {} }) },
        '@/api/stories': { getStory: async () => ({ success: true, data: detail }) },
      })
      const root = element('root'), app = renderer.createApp(component)
      app.use(createI18n({ legacy: false, locale, messages }))
      registerPrimitives(app)
      app.component('router-link', defineComponent({ props: ['to'], setup: (_props, { slots }) => () => h('a', slots.default?.()) }))
      app.component('t-empty', defineComponent({ setup: () => () => null }))
      app.component('t-pagination', defineComponent({ setup: () => () => null }))
      app.config.warnHandler = warning => { throw new Error(warning) }
      try {
        app.mount(root)
        await settle()
        const times = descendants(root).filter(node => node.tag === 'time')
        assert.ok(visibleText(root).includes(locale === 'en-US' ? 'No source' : '无出处'))
        assert.equal(times.length, 3)
        assert.equal(times[2].props.datetime, scenario.occurredAt)
        assert.equal(visibleText(times[2]), scenario.expected)
        const arrivalText = locale === 'en-US' ? '10/24/2026, 12:00:00 PM EDT' : '2026/10/24 GMT-4 12:00:00'
        assert.equal(visibleText(times[0]), arrivalText)
        assert.equal(visibleText(times[1]), arrivalText)
      } finally {
        app.unmount()
        await settle()
        globalThis.document = previousDocument
        if (previousTZ === undefined) delete process.env.TZ
        else process.env.TZ = previousTZ
      }
    })
  }
}

for (const merged of [false, true]) {
  test(`fact anchors scroll the story container after its timeline renders, merged=${merged}`, async () => {
    const previousDocument = globalThis.document
    globalThis.document = { title: '' } as Document
    const route = reactive({ params: { storyId: merged ? 'old-story' : 'story' }, query: {}, hash: merged ? '#fact-old-fact' : '#fact-target' })
    const scrolls: unknown[] = []
    let resolveDetail!: (value: unknown) => void
    const detail = { story: { id: 'story', title: 'Story', first_fact_at: arrivedAt, last_fact_at: arrivedAt }, facts: [{
      id: 'target', title: 'Second fact', subject: 'Aurora', action: 'released', object: '', conditions: '', sources: [],
    }], mentions: [] }
    let first = true
    const component = loadStoriesPage({
      '@/components/FollowToggle.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
      '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) },
      './components/GroupingCorrection.vue': defineComponent({ setup: () => () => null }),
      'vue-router': { useRoute: () => route, useRouter: () => ({ replace: (value: { hash: string }) => { route.params.storyId = 'story'; route.hash = value.hash } }) },
      '@/api/stories': {
        getStory: () => { if (first) { first = false; return new Promise(resolve => { resolveDetail = resolve }) } return Promise.resolve({ success: true, data: detail }) },
        getFact: async () => ({ success: true, data: { id: 'target', story_id: 'story' } }),
      },
    })
    const root = element('root'), app = renderer.createApp(component)
    app.use(createI18n({ legacy: false, locale: 'en-US', messages }))
    registerPrimitives(app)
    for (const name of ['router-link', 't-empty', 't-pagination']) app.component(name, defineComponent({ setup: (_props, { slots }) => () => h('div', slots.default?.()) }))
    try {
      app.mount(root); await settle()
      const page = descendants(root).find(node => node.tag === 'main')!
      Object.assign(page, { scrollTop: 80, clientTop: 2, getBoundingClientRect: () => ({ top: 40 }), scrollTo: (options: unknown) => scrolls.push(options) })
      page.querySelectorAll = () => descendants(page).filter(node => node.props.id).map(node => Object.assign(node, {
        id: node.props.id, getBoundingClientRect: () => ({ top: 950 }),
      }))
      assert.deepEqual(scrolls, [], 'no scrolling before the asynchronous facts are rendered')
      resolveDetail({ success: true, data: detail }); await settle()
      assert.equal(route.hash, '#fact-target')
      assert.ok(visibleText(root).includes('Second fact'))
      assert.deepEqual(scrolls, [{ top: 988, behavior: 'auto' }], 'the offset belongs to the internal page, including its existing scroll and border')
    } finally { app.unmount(); await settle(); globalThis.document = previousDocument }
  })
}

test('linked fact endpoints open their entity, unlinked ones stay plain text', async () => {
  const previousDocument = globalThis.document
  globalThis.document = { title: '' } as Document
  const route = reactive({ params: { storyId: 'story' }, query: {} })
  const detail = {
    story: { id: 'story', title: 'Model release', first_fact_at: arrivedAt, last_fact_at: arrivedAt },
    facts: [{
      id: 'fact', title: 'Release', subject: 'Aurora', action: 'released', object: 'Model A', conditions: '',
      subject_entity_id: 'entity-aurora', object_entity_id: null, occurred_at: null, created_at: arrivedAt, sources: [],
    }],
  }
  const component = loadStoriesPage({
    '@/components/FollowToggle.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
      '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) },
      './components/GroupingCorrection.vue': defineComponent({ setup: () => () => null }),
    'vue-router': { useRoute: () => route, useRouter: () => ({ replace() {} }) },
    '@/api/stories': { getStory: async () => ({ success: true, data: detail }) },
  })
  const root = element('root'), app = renderer.createApp(component)
  app.use(createI18n({ legacy: false, locale: 'en-US', messages }))
  registerPrimitives(app)
  app.component('router-link', defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { to: props.to }, slots.default?.()) }))
  app.component('t-empty', defineComponent({ setup: () => () => null }))
  app.component('t-pagination', defineComponent({ setup: () => () => null }))
  try {
    app.mount(root)
    await settle()
    const entityLinks = descendants(root).filter(node => node.tag === 'a' && (node.props.to as { query?: { tab?: string } })?.query?.tab === 'entities')
    assert.equal(entityLinks.length, 1)
    assert.equal(visibleText(entityLinks[0]), 'Aurora')
    assert.deepEqual((entityLinks[0].props.to as { query: unknown }).query, { tab: 'entities', entity_id: 'entity-aurora' })
    assert.ok(visibleText(root).includes('Model A'))
  } finally {
    app.unmount()
    await settle()
    globalThis.document = previousDocument
  }
})

for (const missingAnchor of [false, true]) {
  test(`merged links resolve the story and fact together, unknown anchor=${missingAnchor}`, async () => {
    const previousDocument = globalThis.document
    globalThis.document = { title: '' } as Document
    const route = reactive({ params: { storyId: 'old-story' }, query: { page: '3' }, hash: '#fact-old-fact' })
    const destinations: unknown[] = []
    const component = loadStoriesPage({
      '@/components/FollowToggle.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
      '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) },
      './components/GroupingCorrection.vue': defineComponent({ setup: () => () => null }),
      'vue-router': { useRoute: () => route, useRouter: () => ({ replace: (value: unknown) => destinations.push(value) }) },
      '@/api/stories': {
        getStory: async () => ({ success: true, data: { story: { id: 'kept-story', title: 'Retained story', first_fact_at: arrivedAt, last_fact_at: arrivedAt, manual_corrected: true }, facts: [], mentions: [] } }),
        getFact: async () => {
          if (missingAnchor) throw { status: 404 }
          return { success: true, data: { id: 'kept-fact', story_id: 'kept-story' } }
        },
      },
    })
    const root = element('root'), app = renderer.createApp(component)
    app.use(createI18n({ legacy: false, locale: 'en-US', messages }))
    registerPrimitives(app)
    for (const name of ['router-link', 't-empty', 't-pagination']) app.component(name, defineComponent({ setup: (_props, { slots }) => () => h('div', slots.default?.()) }))
    try {
      app.mount(root); await settle()
      assert.deepEqual(destinations, [{ path: '/platform/stories/kept-story', query: { page: '3' }, hash: missingAnchor ? '#fact-old-fact' : '#fact-kept-fact' }])
      assert.ok(visibleText(root).includes('Retained story'))
      assert.ok(visibleText(root).includes('Manually corrected'))
      assert.equal(descendants(root).some(node => node.props.role === 'alert'), false)
    } finally { app.unmount(); await settle(); globalThis.document = previousDocument }
  })
}

for (const selectedID of ['', 'story']) {
  test(`story ${selectedID ? 'detail' : 'list'} exposes the shared follow and unfollow control`, async () => {
    const previousDocument = globalThis.document
    globalThis.document = { title: '' } as Document
    const route = reactive({ params: { storyId: selectedID }, query: {}, hash: '' })
    const store = reactive({ targetsLoaded: true, targetsLoading: false, targetsError: false, followed: false,
      isFollowing: () => store.followed, isPending: () => false, loadTargets: async () => {}, refreshAfterTargetChange: async () => {},
      setFollowing: async (type: string, id: string, name: string, value: boolean) => { assert.deepEqual([type, id, name], ['story', 'story', 'Progress']); store.followed = value },
    })
    const modules = {
      '@/stores/follows': { useFollowStore: () => store },
      'tdesign-vue-next': { MessagePlugin: { success() {} } },
      'vue-router': { useRoute: () => route, useRouter: () => ({ replace() {} }) },
    }
    const toggle = loadComponent(new URL('../../components/FollowToggle.vue', import.meta.url), modules)
    const story = { id: 'story', title: 'Progress', first_fact_at: arrivedAt, last_fact_at: arrivedAt }
    const component = loadStoriesPage({
      ...modules,
      '@/components/FollowToggle.vue': { __esModule: true, default: toggle },
      './components/GroupingCorrection.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
      '@/api/stories': { listStories: async () => ({ success: true, data: { items: [story], total: 1 } }), getStory: async () => ({ success: true, data: { story, facts: [], mentions: [], related_stories: [] } }) },
    })
    const root = element('root'), app = renderer.createApp(component)
    app.use(createI18n({ legacy: false, locale: 'en-US', messages })); registerPrimitives(app)
    app.component('router-link', defineComponent({ setup: (_props, { slots }) => () => h('a', slots.default?.()) }))
    for (const name of ['t-empty', 't-pagination']) app.component(name, defineComponent({ setup: () => () => null }))
    try {
      app.mount(root); await settle()
      let button = descendants(root).find(node => node.props['aria-label'] === 'Follow Progress')!
      assert.ok(button)
      await button.props.onClick(); await settle()
      button = descendants(root).find(node => node.props['aria-label'] === 'Unfollow Progress')!
      assert.equal(button.props['aria-pressed'], true)
      await button.props.onClick(); await settle()
      assert.equal(store.followed, false)
    } finally { app.unmount(); globalThis.document = previousDocument }
  })
}

for (const locale of ['en-US', 'zh-CN'] as const) {
  test(`${locale}: mixed story filters restore from the URL, reset pagination and survive detail navigation`, async () => {
    const previousDocument = globalThis.document
    globalThis.document = { title: '' } as Document
    const route = reactive({ params: { storyId: '' }, query: { page: '3', entity_ids: [' e1,e1', 'e2'], domain_ids: 'd1', kept: 'yes' } as Record<string, any>, hash: '' })
    const reads: unknown[] = []
    const story = { id: 'story', title: 'Filtered progress', first_fact_at: arrivedAt, last_fact_at: arrivedAt }
    const component = loadStoriesPage({
      '@/components/FollowToggle.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
      '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) },
      './components/GroupingCorrection.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
      'vue-router': { useRoute: () => route, useRouter: () => ({ replace: ({ query }: { query: Record<string, unknown> }) => {
        route.query = Object.fromEntries(Object.entries(query).filter(([, value]) => value !== undefined))
      } }) },
      '@/api/entities': { listEntities: async () => ({ success: true, data: [{ id: 'e1', canonical_name: 'Aurora' }] }) },
      '@/api/domains': { listDomains: async () => ({ success: true, data: [{ id: 'd1', name: 'Agent' }, { id: 'd2', name: 'LLM' }] }) },
      '@/api/stories': {
        listStories: async (limit: number, offset: number, filters: unknown) => { reads.push({ limit, offset, filters }); return { success: true, data: { items: [story], total: 60 } } },
        getStory: async () => ({ success: true, data: { story, facts: [], mentions: [], related_stories: [] } }),
      },
    })
    const root = element('root'), app = renderer.createApp(component)
    app.use(createI18n({ legacy: false, locale, messages })); registerPrimitives(app)
    delete app._context.components['t-select']
    app.component('t-select', defineComponent({
      props: ['modelValue', 'options', 'loading'], emits: ['update:modelValue'],
      setup: (props, { attrs, emit }) => () => h('select', {
        ...attrs, value: props.modelValue, options: props.options,
        onChange: (values: string[]) => emit('update:modelValue', values),
      }),
    }))
    app.component('router-link', defineComponent({ props: ['to'], setup: (props, { slots }) => () => h('a', { to: props.to }, slots.default?.()) }))
    for (const name of ['t-empty', 't-pagination']) app.component(name, defineComponent({ setup: () => () => null }))
    app.config.warnHandler = warning => { throw new Error(warning) }
    const select = () => descendants(root).find(node => node.tag === 'select')!
    try {
      app.mount(root); await settle()
      assert.deepEqual(reads, [{ limit: 20, offset: 40, filters: { entity_ids: ['e1', 'e2'], domain_ids: ['d1'] } }])
      assert.deepEqual(select().props.value, ['entity:e1', 'entity:e2', 'domain:d1'])
      assert.deepEqual(select().props.options.map((group: any) => group.children), [
        [{ value: 'entity:e1', label: 'Aurora' }, { value: 'entity:e2', label: 'e2' }],
        [{ value: 'domain:d1', label: 'Agent' }, { value: 'domain:d2', label: 'LLM' }],
      ])
      select().props.onChange(['entity:e1', 'domain:d2']); await settle()
      assert.deepEqual(route.query, { entity_ids: 'e1', domain_ids: 'd2', kept: 'yes' })
      assert.deepEqual(reads.at(-1), { limit: 20, offset: 0, filters: { entity_ids: ['e1'], domain_ids: ['d2'] } })
      const storyLink = descendants(root).find(node => node.tag === 'a' && visibleText(node) === 'Filtered progress')!
      assert.deepEqual(storyLink.props.to, { path: '/platform/stories/story', query: route.query })
      route.params.storyId = 'story'; await settle()
      assert.equal(select(), undefined)
      const back = descendants(root).find(node => node.tag === 'a' && (node.props.to as { path?: string })?.path === '/platform/stories')!
      assert.deepEqual(back.props.to.query, route.query)
      route.params.storyId = ''; await settle()
      assert.deepEqual(select().props.value, ['entity:e1', 'domain:d2'])
      select().props.onChange([]); await settle()
      assert.deepEqual(route.query, { kept: 'yes' })
      assert.deepEqual(reads.at(-1), { limit: 20, offset: 0, filters: { entity_ids: [], domain_ids: [] } })
    } finally { app.unmount(); globalThis.document = previousDocument }
  })
}

test('story filter failures keep results readable, allow retry and ignore superseded list responses', async () => {
  const previousDocument = globalThis.document
  globalThis.document = { title: '' } as Document
  const route = reactive({ params: { storyId: '' }, query: { entity_ids: 'e1' }, hash: '' })
  let filtersFail = true, defer = false, resolveOld!: (value: unknown) => void
  const story = (title: string) => ({ id: title, title, first_fact_at: arrivedAt, last_fact_at: arrivedAt })
  const component = loadStoriesPage({
    '@/components/FollowToggle.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
    '@/stores/follows': { useFollowStore: () => ({ refreshAfterTargetChange: async () => {} }) },
    './components/GroupingCorrection.vue': { __esModule: true, default: defineComponent({ setup: () => () => null }) },
    'vue-router': { useRoute: () => route, useRouter: () => ({ replace() {} }) },
    '@/api/entities': { listEntities: async () => { if (filtersFail) throw new Error('Offline'); return { success: true, data: [{ id: 'e1', canonical_name: 'Aurora' }] } } },
    '@/api/stories': { listStories: async () => {
      if (defer) { defer = false; return new Promise(resolve => { resolveOld = resolve }) }
      return { success: true, data: { items: [story(route.query.entity_ids)], total: 1 } }
    } },
  })
  const root = element('root'), app = renderer.createApp(component)
  app.use(createI18n({ legacy: false, locale: 'en-US', messages })); registerPrimitives(app)
  app.component('router-link', defineComponent({ setup: (_props, { slots }) => () => h('a', slots.default?.()) }))
  for (const name of ['t-empty', 't-pagination']) app.component(name, defineComponent({ setup: () => () => null }))
  try {
    app.mount(root); await settle()
    assert.ok(visibleText(root).includes('Could not load filter options'))
    assert.ok(visibleText(root).includes('e1'))
    filtersFail = false
    await descendants(root).find(node => node.tag === 'button' && visibleText(node) === 'Retry')!.props.onClick(); await settle()
    assert.ok(!visibleText(root).includes('Could not load filter options'))
    defer = true; route.query.entity_ids = 'old'; await settle()
    route.query.entity_ids = 'latest'; await settle()
    assert.ok(visibleText(root).includes('latest'))
    resolveOld({ success: true, data: { items: [story('Stale response')], total: 1 } }); await settle()
    assert.ok(!visibleText(root).includes('Stale response'))
    assert.ok(visibleText(root).includes('latest'))
  } finally { app.unmount(); globalThis.document = previousDocument }
})
