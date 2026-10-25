import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../../i18n/locales/en-US'
import zh from '../../i18n/locales/zh-CN'
import { descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from './componentTestHarness'

const messages = { 'en-US': en, 'zh-CN': zh }
const arrivedAt = '2026-10-24T16:00:00Z'

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
          occurred_at: scenario.occurredAt, created_at: arrivedAt, sources: [],
        }],
      }
      const component = loadComponent(new URL('./StoriesPage.vue', import.meta.url), {
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
  const component = loadComponent(new URL('./StoriesPage.vue', import.meta.url), {
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
