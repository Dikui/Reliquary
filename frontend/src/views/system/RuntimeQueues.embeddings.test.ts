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
import { click, descendants, element, hasClass, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../knowledge/componentTestHarness'

const require = createRequire(import.meta.url)
const moduleOf = (value: unknown) => ({ __esModule: true, default: value })
const messages = { 'zh-CN': zh, 'en-US': en }
const queue = { name: 'entity', pool: 'enrichment', weight: 1, active: 1, pending: 0, scheduled: 0, retry: 0, archived: 0, completed: 0, paused: false }

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: queue and task API loading shows the localized full rebuild name and descriptions`, async () => {
    const calls: { url: string; options?: any }[] = []
    const source = readFileSync(new URL('../../api/system/index.ts', import.meta.url), 'utf8')
    const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    const api = { exports: {} as any }
    new Function('require', 'module', 'exports', code)((name: string) => name === '@/utils/request' ? {
      get: async (url: string, options?: any) => {
        calls.push({ url, options })
        if (url === '/api/v1/system/runtime/queues') return {
          available: true, queues: [queue], pools: [{ name: 'enrichment', concurrency: 1, queue_count: 1, instances: 1, cluster_capacity: 1, active: 1, utilization: 1 }],
        }
        assert.equal(url, '/api/v1/system/runtime/queues/entity/tasks')
        return { available: true, has_more: false, tasks: [{
          id: 'full-rebuild-task', queue: 'entity', type: 'knowledge:embedding_rebuild', state: 'active',
          retried: 0, max_retry: 3, allowed_actions: [],
        }] }
      },
    } : require(name), api, api.exports)
    const drawer = defineComponent({ props: ['visible'], setup: (props, { slots }) => () => props.visible ? h('section', { role: 'dialog' }, slots.default?.()) : null })
    const component = loadComponent(new URL('./RuntimeQueues.vue', import.meta.url), {
      '@/api/system': api.exports, '@/components/settings/SettingDrawer.vue': moduleOf(drawer),
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
      assert.ok(messages[locale].system.globalSettings.keyDescriptions.asynq.enrichment_concurrency.includes(scopeText))
      const count = descendants(root).find(node => hasClass(node, 'rq-task-count--active'))
      assert.ok(count)
      await click(count)
      const dialog = descendants(root).find(node => node.props.role === 'dialog')
      assert.ok(dialog)
      const taskType = descendants(dialog).find(node => hasClass(node, 'rq-failed-row-type'))!
      assert.equal(visibleText(taskType), locale === 'zh-CN' ? '全部向量重建' : 'Rebuild all embeddings')
      assert.doesNotMatch(visibleText(dialog), /knowledge:embedding_rebuild/)
      assert.deepEqual(calls, [
        { url: '/api/v1/system/runtime/queues', options: undefined },
        { url: '/api/v1/system/runtime/queues/entity/tasks', options: { params: { state: 'active', page_size: 20 } } },
      ])
      assert.deepEqual(errors, []); assert.deepEqual(warnings, [])
    } finally { app.unmount() }
  })
}
