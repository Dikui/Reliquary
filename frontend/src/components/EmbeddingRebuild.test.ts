import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import { defineComponent, h } from 'vue'
import ts from 'typescript'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import type { EmbeddingRebuildData, EmbeddingRebuildRun } from '../api/embedding-rebuild'
import { click, descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../views/knowledge/componentTestHarness'

const messages = { 'zh-CN': zh, 'en-US': en }
const baseRun: EmbeddingRebuildRun = { id: 'run-1', status: 'pending', model_id: 'model-1', model_name: 'Multimodal model', dimension: 512, total_count: 10, completed_count: 0, failed_count: 0 }

function actualAPI(get: () => Promise<unknown>, post: (payload: any) => Promise<unknown>, paths: string[]) {
  const output = ts.transpileModule(readFileSync(new URL('../api/embedding-rebuild.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => {
    assert.equal(name, '@/utils/request')
    return { get: (path: string) => { paths.push(path); return get() }, post: (path: string, payload: any) => { paths.push(path); return post(payload) } }
  }, module, module.exports)
  return module.exports
}

async function mount(locale: keyof typeof messages, api: object, props: object = {}) {
  const component = loadComponent(new URL('./EmbeddingRebuild.vue', import.meta.url), { '@/api/embedding-rebuild': api })
  const app = renderer.createApp(component, { modelId: 'model-1', models: [{ id: 'model-1', display_name: 'Multimodal model' }], ...props })
  app.use(createI18n({ legacy: false, locale, messages }))
  registerPrimitives(app)
  app._context.components['t-alert'] = defineComponent({ props: ['message'], setup: (props, { attrs }) => () => h('div', attrs, props.message) })
  const errors: unknown[] = [], warnings: string[] = []
  app.config.errorHandler = error => errors.push(error)
  app.config.warnHandler = warning => warnings.push(warning)
  const root = element('root')
  app.mount(root)
  await settle()
  return { root, app, errors, warnings }
}
function button(root: ReturnType<typeof element>, label: string) {
  const target = descendants(root).find(node => node.tag === 'button' && visibleText(node) === label)
  assert.ok(target, label)
  return target
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: API-loaded progress supports start, pause, resume, failure retry and completion`, async () => {
    const copy = messages[locale].embeddingRebuild
    let data: EmbeddingRebuildData = { latest_run: null, failed_items: [] }
    const payloads: any[] = [], paths: string[] = []
    const api = actualAPI(async () => ({ success: true, data: structuredClone(data) }), async payload => {
      payloads.push(payload)
      if (payload.action === 'start') data = { latest_run: { ...baseRun }, failed_items: [] }
      if (payload.action === 'pause') data = { latest_run: { ...baseRun, status: 'paused', completed_count: 3, failed_count: 1 }, failed_items: [{ id: 'item-1', kind: 'image', source_id: 'article-1/photo.png', error: 'Temporary model failure', attempts: 1 }] }
      if (payload.action === 'resume') data.latest_run = { ...data.latest_run!, status: 'running' }
      if (payload.action === 'retry_failed') data = { latest_run: { ...baseRun, status: 'running', completed_count: 9 }, failed_items: [] }
      return { success: true, data: structuredClone(data) }
    }, paths)
    const view = await mount(locale, api)
    try {
      assert.ok(visibleText(view.root).includes(copy.noRun))
      const popconfirm = descendants(view.root).find(node => node.props['data-popconfirm'])!
      await click(popconfirm)
      const confirm = descendants(view.root).find(node => node.props['data-confirm'])!
      await click(confirm)
      assert.ok(visibleText(view.root).includes(copy.status.pending))
      await click(button(view.root, copy.pause))
      assert.ok(visibleText(view.root).includes(copy.status.paused))
      assert.ok(visibleText(view.root).includes('Temporary model failure'))
      await click(button(view.root, copy.resume))
      assert.equal(data.latest_run?.completed_count, 3)
      data.latest_run = { ...data.latest_run!, status: 'completed_with_errors', completed_count: 9 }
      await click(button(view.root, messages[locale].common.refresh))
      await click(button(view.root, copy.retryFailed))
      data.latest_run = { ...data.latest_run!, status: 'completed', completed_count: 10 }
      await click(button(view.root, messages[locale].common.refresh))
      assert.ok(visibleText(view.root).includes(copy.status.completed))
      assert.equal(descendants(view.root).find(node => node.tag === 'progress')?.props.value, 10)
      assert.deepEqual(payloads, [{ action: 'start' }, { action: 'pause', run_id: 'run-1' }, { action: 'resume', run_id: 'run-1' }, { action: 'retry_failed', run_id: 'run-1' }])
      assert.ok(paths.every(path => path === '/api/v1/embeddings/rebuild'))
      assert.deepEqual(view.errors, []); assert.deepEqual(view.warnings, [])
    } finally { view.app.unmount() }
  })

  test(`${locale}: failed refresh preserves progress and an API action error is retryable`, async () => {
    let count = 0, attempts = 0
    const view = await mount(locale, {
      getEmbeddingRebuild: async () => { if (++count > 1) throw new Error('Progress unavailable'); return { success: true, data: { latest_run: { ...baseRun, status: 'paused', completed_count: 3 }, failed_items: [] } } },
      controlEmbeddingRebuild: async () => { if (++attempts === 1) throw new Error('Queue unavailable'); return { success: true, data: { latest_run: { ...baseRun, status: 'completed', completed_count: 10 }, failed_items: [] } } },
    })
    try {
      await click(button(view.root, messages[locale].common.refresh))
      assert.ok(visibleText(view.root).includes('Progress unavailable'))
      assert.equal(descendants(view.root).find(node => node.tag === 'progress')?.props.value, 3)
      await click(button(view.root, messages[locale].embeddingRebuild.resume))
      assert.ok(visibleText(view.root).includes('Queue unavailable'))
      await click(button(view.root, messages[locale].embeddingRebuild.resume))
      assert.ok(visibleText(view.root).includes(messages[locale].embeddingRebuild.status.completed))
    } finally { view.app.unmount() }
  })

  test(`${locale}: unsaved model settings and a different model prevent rebuilding old tasks`, async () => {
    const view = await mount(locale, { getEmbeddingRebuild: async () => ({ success: true, data: { latest_run: { ...baseRun, status: 'paused', model_id: 'old-model' }, failed_items: [] } }) }, { disabled: true })
    try {
      assert.ok(visibleText(view.root).includes(messages[locale].embeddingRebuild.saveFirst))
      assert.ok(visibleText(view.root).includes(messages[locale].embeddingRebuild.modelChanged))
      assert.equal(button(view.root, messages[locale].embeddingRebuild.resume).props.disabled, true)
    } finally { view.app.unmount() }
  })
  test(`${locale}: a changed profile on the same model exposes a new rebuild and disables resuming`, async () => {
    const requests: any[] = []
    const view = await mount(locale, {
      getEmbeddingRebuild: async () => ({ success: true, data: { latest_run: { ...baseRun, status: 'paused' }, failed_items: [], current_model_matches: false } }),
      controlEmbeddingRebuild: async (action: string, runId?: string) => { requests.push({ action, runId }); return { success: true, data: { latest_run: { ...baseRun, id: 'run-new', dimension: 768 }, failed_items: [], current_model_matches: true } } },
    })
    try {
      assert.equal(button(view.root, messages[locale].embeddingRebuild.resume).props.disabled, true)
      assert.equal(button(view.root, messages[locale].embeddingRebuild.start).props.disabled, false)
      await click(descendants(view.root).find(node => node.props['data-popconfirm'])!)
      await click(descendants(view.root).find(node => node.props['data-confirm'])!)
      assert.deepEqual(requests, [{ action: 'start', runId: undefined }])
    } finally { view.app.unmount() }
  })
}
