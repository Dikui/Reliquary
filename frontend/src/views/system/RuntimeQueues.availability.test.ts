import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import { click, descendants, element, hasClass, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../library/componentTestHarness'
const require = createRequire(import.meta.url)
const moduleOf = (value: unknown) => ({ __esModule: true, default: value })
const messages = { 'zh-CN': zh, 'en-US': en }
const actions: Record<string, string[]> = {
  pending: ['cancel'], dispatched: ['cancel'], active: ['cancel'], scheduled: ['cancel', 'run_now'],
  retry: ['cancel', 'run_now'], archived: ['run_now', 'delete'], completed: [], cancelled: ['delete'],
}
async function mount(locale: 'zh-CN' | 'en-US', state: string, unavailable = false, blocked = false) {
  const calls: string[] = []
  const uiState = state === 'dispatched' ? 'pending' : state
  const job = { id: 'pg-operation', type: 'manual:process', queue: 'default', state: uiState, entry_id: 'entry', retried: 1, max_retry: 3, allowed_actions: blocked ? [] : actions[state], cancellation_unavailable_reason: blocked ? 'completed' : undefined }
  const api = {
    getQueueConnectionStatus: async () => ({ state: 'reconnecting', alert: true, last_error: 'Redis connection refused' }),
    getRuntimeQueues: async () => {
      calls.push('queues')
      if (unavailable) throw new Error('fixture PostgreSQL unavailable')
      return { available: true, queues: [{ name: 'default', pool: 'core', weight: 1, active: 1, pending: 1, scheduled: 1, retry: 1, archived: 1, completed: 1, cancelled: 1 }], pools: [{ name: 'core', concurrency: 8, instances: 1, cluster_capacity: 8, active: 1, utilization: 0.125 }] }
    },
    getRuntimeTasks: async () => { calls.push('tasks'); return { available: true, tasks: [job], has_more: false } },
  }
  const drawer = defineComponent({ props: ['visible'], setup: (props, { slots }) => () => props.visible ? h('section', { role: 'dialog' }, slots.default?.()) : null })
  const component = loadComponent(new URL('./RuntimeQueues.vue', import.meta.url), {
    './QueueConnectionNotice.vue': moduleOf(loadComponent(new URL('./QueueConnectionNotice.vue', import.meta.url), { '@/api/system': api })),
    '@/api/system': api, '@/api/chatgpt': { getChatGPTAccounts: async () => ({ accounts: [] }), getChatGPTPolicy: async () => ({ paused: false }) },
    '@/components/ChatGPTUsageStatus.vue': moduleOf(defineComponent({ setup: () => () => null })), '@/components/settings/SettingDrawer.vue': moduleOf(drawer),
    '@/i18n/countCopy': require('../../i18n/countCopy'), './runtimeTaskPagination': require('./runtimeTaskPagination'), 'tdesign-vue-next': { MessagePlugin: {} },
  })
  const app = renderer.createApp(component)
  app.use(createI18n({ legacy: false, locale, messages })); registerPrimitives(app)
  for (const name of ['t-switch', 't-skeleton', 't-progress', 't-empty']) app.component(name, defineComponent({ setup: () => () => null }))
  app.component('t-table', defineComponent({ props: ['data'], setup: (props, { slots }) => () => h('div', props.data.map((row: unknown) => h('div', [slots.name?.({ row }), slots.active?.({ row })]))) }))
  const root = element('root'), errors: unknown[] = []
  app.config.errorHandler = error => errors.push(error)
  app.mount(root); await settle()
  return { app, root, calls, errors, uiState }
}
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: Redis outage keeps PostgreSQL overview pools and details visible`, async () => {
    const view = await mount(locale, 'retry')
    try {
      const runtime = messages[locale].system.globalSettings.runtime
      assert.ok(visibleText(view.root).includes('Redis connection refused'))
      assert.ok(descendants(view.root).some(node => hasClass(node, 'rq-overview')))
      assert.ok(visibleText(view.root).includes(runtime.pools.core))
      await click(descendants(view.root).find(node => hasClass(node, 'rq-task-count--active'))!)
      assert.ok(visibleText(view.root).includes(runtime.tasks.taskTypes.manualProcess))
      assert.deepEqual(view.calls, ['queues', 'tasks']); assert.deepEqual(view.errors, [])
    } finally { view.app.unmount() }
  })
  test(`${locale}: genuine queue API failure shows database error separately from Redis`, async () => {
    const view = await mount(locale, 'retry', true)
    try { assert.ok(visibleText(view.root).includes('fixture PostgreSQL unavailable')); assert.ok(visibleText(view.root).includes('Redis connection refused')); assert.equal(view.calls[0], 'queues'); assert.deepEqual(view.errors, []) } finally { view.app.unmount() }
  })
  for (const state of Object.keys(actions)) {
    test(`${locale}: ${state} task buttons match server permissions and record confirmation`, async () => {
      const view = await mount(locale, state)
      try {
        const runtime = messages[locale].system.globalSettings.runtime
        await click(descendants(view.root).find(node => hasClass(node, 'rq-task-count--active'))!)
        const dialog = descendants(view.root).find(node => node.props.role === 'dialog')!
        for (const [action, label] of Object.entries({ cancel: runtime.tasks.cancel, run_now: runtime.tasks.runNow, delete: runtime.tasks.deleteRecord })) {
          const button = descendants(dialog).find(node => node.tag === 'button' && node.props['aria-label'] === label)
          assert.equal(Boolean(button), actions[state].includes(action), action)
          if (button && action === 'delete') {
            await click(button.parent!)
            assert.ok(visibleText(dialog).includes(state === 'cancelled' ? runtime.tasks.deleteCancelledConfirm : runtime.tasks.deleteConfirm))
          }
        }
        assert.deepEqual(view.errors, [])
      } finally { view.app.unmount() }
    })
  }
  test(`${locale}: completed business record hides cancellation and explains why`, async () => {
    const view = await mount(locale, 'retry', false, true)
    try {
      await click(descendants(view.root).find(node => hasClass(node, 'rq-task-count--active'))!)
      const runtime = messages[locale].system.globalSettings.runtime
      assert.equal(descendants(view.root).find(node => node.props['aria-label'] === runtime.tasks.cancel), undefined)
      assert.ok(visibleText(view.root).includes(runtime.tasks.cancelUnavailable.replace('{state}', runtime.tasks.states.completed)))
      assert.deepEqual(view.errors, [])
    } finally { view.app.unmount() }
  })
}
