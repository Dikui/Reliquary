import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import * as countCopy from '../../i18n/countCopy'
import * as pagination from './runtimeTaskPagination'
import { click, descendants, element, hasClass, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../knowledge-base/componentTestHarness'

const require = createRequire(import.meta.url)
const moduleOf = (value: unknown) => ({ __esModule: true, default: value })
const messages = { 'zh-CN': zh, 'en-US': en }
const queue = { name: 'entity', pool: 'enrichment', weight: 1, active: 1, pending: 0, scheduled: 0, retry: 0, archived: 0, completed: 0, paused: false }

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const kind of ['embeddingRebuild', 'topicBackfill'] as const) {
  test(`${locale}: queue and task API loading shows localized ${kind} name and descriptions`, async () => {
    const calls: { url: string; options?: any }[] = []
    const source = readFileSync(new URL('../../api/system.ts', import.meta.url), 'utf8')
    const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    const api = { exports: {} as any }
    new Function('require', 'module', 'exports', code)((name: string) => name === '@/utils/request' ? {
      get: async (url: string, options?: any) => {
        if (url === '/health') return { status: 'ok', queue_connection: { state: 'active', alert: false, alert_after_seconds: 300 } }
        calls.push({ url, options })
        if (url === '/api/v1/system/runtime/queues') return {
          available: true, queues: [queue], pools: [{ name: 'enrichment', concurrency: 1, queue_count: 1, instances: 1, cluster_capacity: 1, active: 1, utilization: 1 }],
        }
        assert.equal(url, '/api/v1/system/runtime/queues/entity/tasks')
        return { available: true, has_more: false, tasks: [{
          id: 'full-rebuild-task', queue: 'entity', type: kind === 'embeddingRebuild' ? 'knowledge:embedding_rebuild' : 'topic:backfill', state: 'active',
          retried: 0, max_retry: 3, allowed_actions: [],
          task_id: kind === 'topicBackfill' ? 'topic-history-job' : undefined,
        }] }
      },
    } : require(name), api, api.exports)
    const drawer = defineComponent({ props: ['visible'], setup: (props, { slots }) => () => props.visible ? h('section', { role: 'dialog' }, slots.default?.()) : null })
    const component = loadComponent(new URL('./RuntimeQueues.vue', import.meta.url), {
      './QueueConnectionNotice.vue': moduleOf(loadComponent(new URL('./QueueConnectionNotice.vue', import.meta.url), { '@/api/system': api.exports })),
      '@/api/system': api.exports, '@/api/chatgpt': { getChatGPTAccounts: async () => ({ accounts: [] }), getChatGPTPolicy: async () => ({ paused: false }) }, '@/components/ChatGPTUsageStatus.vue': moduleOf(defineComponent({ setup: () => () => null })), '@/components/settings/SettingDrawer.vue': moduleOf(drawer),
      '@/i18n/countCopy': countCopy, './runtimeTaskPagination': pagination,
      'tdesign-vue-next': { MessagePlugin: {} },
    })
    const app = renderer.createApp(component)
    app.use(createI18n({ legacy: false, locale, messages })); registerPrimitives(app)
    app.component('t-switch', defineComponent({ setup: () => () => null }))
    app.component('t-skeleton', defineComponent({ setup: () => () => null }))
    app.component('t-progress', defineComponent({ setup: () => () => null }))
    app.component('t-empty', defineComponent({ setup: () => () => null }))
    app.component('t-table', defineComponent({ props: ['data'], setup: (props, { slots }) => () => h('div',
      props.data.map((row: unknown) => h('div', [slots.name?.({ row }), slots.active?.({ row })])),
    ) }))
    const root = element('root'), errors: unknown[] = [], warnings: string[] = []
    app.config.errorHandler = error => errors.push(error)
    app.config.warnHandler = warning => warnings.push(warning)
    try {
      app.mount(root); await settle()
      const runtime = messages[locale].system.globalSettings.runtime
      assert.ok(visibleText(root).includes(runtime.poolDescriptions.enrichment))
      assert.ok(visibleText(root).includes(runtime.queueDescriptions.entity))
      const scopeText = locale === 'zh-CN' ? '全部向量重建' : 'all embeddings'
      assert.ok(runtime.poolDescriptions.enrichment.includes(scopeText))
      assert.ok(runtime.queueDescriptions.entity.includes(scopeText))
      assert.ok(messages[locale].system.globalSettings.keyDescriptions.queue.enrichment_concurrency.includes(scopeText))
      const count = descendants(root).find(node => hasClass(node, 'rq-task-count--active'))
      assert.ok(count)
      await click(count)
      const dialog = descendants(root).find(node => node.props.role === 'dialog')
      assert.ok(dialog)
      const taskType = descendants(dialog).find(node => hasClass(node, 'rq-failed-row-type'))!
      assert.equal(visibleText(taskType), runtime.tasks.taskTypes[kind])
      assert.doesNotMatch(visibleText(dialog), /knowledge:embedding_rebuild|topic:backfill/)
      if (kind === 'topicBackfill') {
        assert.ok(visibleText(dialog).includes(runtime.tasks.taskIDLabel))
        assert.ok(visibleText(dialog).includes('topic-history-job'))
      }
      assert.deepEqual(calls, [
        { url: '/api/v1/system/runtime/queues', options: undefined },
        { url: '/api/v1/system/runtime/queues/entity/tasks', options: { params: { state: 'active', page_size: 20 } } },
      ])
      assert.deepEqual(errors, []); assert.deepEqual(warnings, [])
    } finally { app.unmount() }
  })
  }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const action of ['run_now', 'delete'] as const) {
    test(`${locale}: durable failed task ${action} uses the HTTP API and reloads queue counts and details`, async () => {
      const calls: { method: string; url: string; state?: string }[] = []
      const notices: string[] = []
      const runtime = messages[locale].system.globalSettings.runtime
      let job: any = {
        id: 'postgres-job-14', queue: 'default', type: 'document:process', state: 'archived',
        retried: 3, max_retry: 3, allowed_actions: ['run_now', 'delete'],
        last_error: 'fixture model unavailable', entry_id: 'article-14', worker: 'core',
        enqueued_at: '2026-10-11T04:00:00Z', last_failed_at: '2026-10-11T04:01:00Z',
      }
      const source = readFileSync(new URL('../../api/system.ts', import.meta.url), 'utf8')
      const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
      const api = { exports: {} as any }
      new Function('require', 'module', 'exports', code)((name: string) => name === '@/utils/request' ? {
        get: async (url: string, options?: any) => {
          if (url === '/health') return { status: 'ok', queue_connection: { state: 'active', alert: false, alert_after_seconds: 300 } }
          calls.push({ method: 'GET', url, ...(options?.params?.state ? { state: options.params.state } : {}) })
          if (url === '/api/v1/system/runtime/queues') return {
            available: true, queues: [{ ...queue, name: 'default', pool: 'core', active: 0, pending: job?.state === 'pending' ? 1 : 0, archived: job?.state === 'archived' ? 1 : 0 }],
            pools: [{ name: 'core', concurrency: 1, queue_count: 1, instances: 1, cluster_capacity: 1, active: 0, utilization: 0 }],
          }
          assert.equal(url, '/api/v1/system/runtime/queues/default/tasks')
          return { available: true, has_more: false, tasks: job?.state === options?.params?.state ? [job] : [] }
        },
        post: async (url: string) => {
          calls.push({ method: 'POST', url })
          assert.equal(url, `/api/v1/system/runtime/queues/default/tasks/postgres-job-14/actions/${action}`)
          job = action === 'delete' ? null : { ...job, state: 'pending', allowed_actions: ['cancel'] }
          return { success: true }
        },
      } : require(name), api, api.exports)
      const drawer = defineComponent({ props: ['visible'], setup: (props, { slots }) => () => props.visible ? h('section', { role: 'dialog' }, slots.default?.()) : null })
      const component = loadComponent(new URL('./RuntimeQueues.vue', import.meta.url), {
        './QueueConnectionNotice.vue': moduleOf(loadComponent(new URL('./QueueConnectionNotice.vue', import.meta.url), { '@/api/system': api.exports })),
      '@/api/system': api.exports,
        '@/api/chatgpt': { getChatGPTAccounts: async () => ({ accounts: [] }), getChatGPTPolicy: async () => ({ paused: false }) },
        '@/components/ChatGPTUsageStatus.vue': moduleOf(defineComponent({ setup: () => () => null })),
        '@/components/settings/SettingDrawer.vue': moduleOf(drawer), '@/i18n/countCopy': countCopy,
        './runtimeTaskPagination': pagination,
        'tdesign-vue-next': { MessagePlugin: { success: (message: string) => notices.push(message), error: (message: string) => assert.fail(message) } },
      })
      const app = renderer.createApp(component)
      app.use(createI18n({ legacy: false, locale, messages })); registerPrimitives(app)
      for (const name of ['t-switch', 't-skeleton', 't-progress', 't-empty']) {
        app.component(name, defineComponent({ setup: () => () => null }))
      }
      app.component('t-table', defineComponent({ props: ['data'], setup: (props, { slots }) => () => h('div',
        props.data.map((row: unknown) => h('div', [slots.name?.({ row }), slots.pending?.({ row }), slots.archived?.({ row })])),
      ) }))
      const root = element('root'), errors: unknown[] = [], warnings: string[] = []
      app.config.errorHandler = error => errors.push(error)
      app.config.warnHandler = warning => warnings.push(warning)
      try {
        app.mount(root); await settle()
        const failedCount = descendants(root).find(node => hasClass(node, 'rq-failed-count'))
        assert.ok(failedCount)
        await click(failedCount)
        const dialog = descendants(root).find(node => node.props.role === 'dialog')
        assert.ok(dialog)
        assert.ok(visibleText(dialog).includes('fixture model unavailable'))
        assert.ok(visibleText(dialog).includes(runtime.pools.core))
        assert.ok(visibleText(dialog).includes(runtime.tasks.attempts.replace('{current}', '4').replace('{max}', '4')))
        const actionLabel = action === 'delete' ? runtime.tasks.deleteRecord : runtime.tasks.runNow
        const button = descendants(dialog).find(node => node.tag === 'button' && node.props['aria-label'] === actionLabel)
        assert.ok(button)
        const confirmation = button.parent
        assert.ok(confirmation?.props['data-popconfirm'])
        await click(confirmation)
        assert.ok(visibleText(dialog).includes(action === 'delete' ? runtime.tasks.deleteConfirm : runtime.tasks.runNowConfirm))
        const confirm = descendants(dialog).find(node => node.props['data-confirm'])
        assert.ok(confirm)
        await click(confirm); await settle()
        assert.deepEqual(notices, [runtime.tasks.actionSuccess[action]])
        assert.ok(!visibleText(dialog).includes('fixture model unavailable'))
        assert.equal(descendants(root).find(node => hasClass(node, 'rq-failed-count')), undefined)
        if (action === 'run_now') {
          assert.equal(job.retried, 3)
          assert.ok(descendants(root).some(node => hasClass(node, 'rq-task-count') && !hasClass(node, 'rq-failed-count')))
        }
        assert.ok(calls.some(call => call.method === 'POST'))
        assert.equal(calls.filter(call => call.url === '/api/v1/system/runtime/queues').length, 2)
        assert.equal(calls.filter(call => call.url.endsWith('/tasks') && call.state === 'archived').length, 2)
        assert.deepEqual(errors, []); assert.deepEqual(warnings, [])
      } finally { app.unmount() }
    })
  }
}
