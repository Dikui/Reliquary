import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../../../i18n/locales/en-US'
import zh from '../../../i18n/locales/zh-CN'
import { click, descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText, type Element } from '../componentTestHarness'

const candidate = { id: 'candidate', name: 'New Institute', entity_type: 'organization', status: 'pending', occurrence_count: 2, entity_id: null }
const second = { ...candidate, id: 'second', name: 'Another Institute' }
const envelope = (data: unknown) => ({ success: true, data })
const detail = (item = candidate) => envelope({ candidate: item, total: 1, occurrences: [{ fact: { id: 'fact', story_id: 'story', title: `Fact for ${item.name}`, subject: item.name, action: 'announced', object: 'Model A' }, endpoint: 'subject' }] })
function button(root: Element, label: string) {
  const found = descendants(root).find(node => node.tag === 'button' && visibleText(node) === label)
  assert.ok(found, `missing button ${label}`)
  return found
}
async function mount(locale: 'zh-CN' | 'en-US', api: Record<string, unknown> = {}) {
  const calls: unknown[][] = [], toasts: string[] = [], changes: unknown[] = []
  const component = loadComponent(new URL('./EntityCandidates.vue', import.meta.url), {
    '@/api/entities': {
      listEntityCandidates: async (...args: unknown[]) => { calls.push(['list', ...args]); return envelope({ items: [candidate, second], total: 2 }) },
      getEntityCandidate: async (id: string) => detail(id === second.id ? second : candidate),
      listEntities: async () => envelope([{ id: 'target', canonical_name: 'Known Institute' }]),
      reviewEntityCandidate: async (...args: unknown[]) => { calls.push(['review', ...args]); return envelope({ ...candidate, status: 'approved', entity_id: 'target' }) },
      ...api,
    },
    'tdesign-vue-next': { MessagePlugin: { success: (value: string) => toasts.push(value) } },
  })
  const root = element('root'), app = renderer.createApp(component, { onChanged: () => changes.push(true) })
  const messages = { 'en-US': en, 'zh-CN': zh }
  app.use(createI18n({ legacy: false, locale, messages }))
  registerPrimitives(app)
  app.component('t-empty', defineComponent({ props: ['description'], setup: props => () => h('p', props.description) }))
  app.config.warnHandler = warning => { throw new Error(warning) }
  app.mount(root)
  await settle()
  return { root, app, calls, toasts, changes, copy: messages[locale] }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: candidate facts can be reviewed and approved once`, async () => {
    let resolveReview: (value: unknown) => void = () => {}, reviews = 0
    const env = await mount(locale, { reviewEntityCandidate: () => { reviews++; return new Promise(resolve => { resolveReview = resolve }) } })
    try {
      assert.ok(visibleText(env.root).includes(env.copy.entityCandidates.title))
      await click(button(env.root, candidate.name))
      const link = descendants(env.root).find(node => node.tag === 'a')!
      assert.equal(visibleText(link), 'Fact for New Institute')
      assert.equal(link.props.href, '/platform/stories/story#fact-fact')
      const approve = button(env.root, env.copy.entityCandidates.approve)
      await click(approve); await click(approve)
      assert.equal(reviews, 1)
      resolveReview(envelope({ ...candidate, status: 'approved' })); await settle()
      assert.equal(env.toasts[0], env.copy.entityCandidates.approveSaved)
      assert.equal(env.changes.length, 1)
    } finally { env.app.unmount() }
  })
  test(`${locale}: document candidates show their source and literal evidence`, async () => {
    const env = await mount(locale, { getEntityCandidate: async () => envelope({ candidate, total: 1, occurrences: [{ entry: { id: 'document', title: 'DeepSeek 文档' }, quote: 'DeepSeek 发布文档解析模型。' }] }) })
    try {
      await click(button(env.root, candidate.name))
      const link = descendants(env.root).find(node => node.tag === 'a')!
      assert.equal(link.props.href, '/platform/entries?entry_id=document')
      assert.equal(visibleText(link), 'DeepSeek 文档')
      assert.ok(visibleText(env.root).includes('DeepSeek 发布文档解析模型。'))
    } finally { env.app.unmount() }
  })
  test(`${locale}: failed alias review preserves target and supports retry`, async () => {
    let available = false
    const requests: unknown[][] = []
    const env = await mount(locale, { reviewEntityCandidate: async (...args: unknown[]) => { requests.push(args); if (!available) throw new Error('Review unavailable'); return envelope(candidate) } })
    try {
      await click(button(env.root, candidate.name))
      const target = descendants(env.root).find(node => node.props['aria-label'] === env.copy.entityCandidates.target)!
      target.props['onUpdate:modelValue']('target'); await settle()
      await click(button(env.root, env.copy.entityCandidates.alias))
      assert.ok(visibleText(env.root).includes('Review unavailable'))
      assert.equal(target.props.modelValue, 'target')
      available = true
      await click(button(env.root, env.copy.entityCandidates.alias))
      assert.deepEqual(requests, [['candidate', 'alias', 'target'], ['candidate', 'alias', 'target']])
      assert.equal(env.toasts[0], env.copy.entityCandidates.aliasSaved)
    } finally { env.app.unmount() }
  })
  test(`${locale}: ignored list remains accessible for later approval`, async () => {
    const env = await mount(locale)
    try {
      await click(button(env.root, candidate.name))
      await click(button(env.root, env.copy.entityCandidates.ignore))
      assert.ok(env.calls.some(call => call[0] === 'review' && call[2] === 'ignore'))
      const status = descendants(env.root).find(node => node.props['aria-label'] === env.copy.entityCandidates.status)!
      status.props['onUpdate:modelValue']('ignored'); await settle()
      assert.deepEqual(env.calls.at(-1), ['list', 'ignored', 50, 0])
      await click(button(env.root, candidate.name))
      assert.ok(!descendants(env.root).some(node => node.tag === 'button' && visibleText(node) === env.copy.entityCandidates.ignore))
      assert.ok(button(env.root, env.copy.entityCandidates.approve))
    } finally { env.app.unmount() }
  })
}

test('late candidate facts cannot replace the current selection', async () => {
  let resolveOld: (value: unknown) => void = () => {}
  const env = await mount('en-US', { getEntityCandidate: (id: string) => id === candidate.id ? new Promise(resolve => { resolveOld = resolve }) : Promise.resolve(detail(second)) })
  try {
    await click(button(env.root, candidate.name))
    await click(button(env.root, second.name))
    resolveOld(detail(candidate)); await settle()
    const links = descendants(env.root).filter(node => node.tag === 'a')
    assert.deepEqual(links.map(visibleText), ['Fact for Another Institute'])
  } finally { env.app.unmount() }
})

test('list failures expose retry and an empty list has a clear state', async () => {
  let available = false
  const env = await mount('en-US', { listEntityCandidates: async () => { if (!available) throw new Error('List unavailable'); return envelope({ items: [], total: 0 }) } })
  try {
    assert.ok(visibleText(env.root).includes('List unavailable'))
    available = true
    await click(button(env.root, env.copy.common.retry))
    assert.ok(visibleText(env.root).includes(env.copy.entityCandidates.empty))
  } finally { env.app.unmount() }
})
