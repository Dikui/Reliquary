import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import enUS from '../../../i18n/locales/en-US'
import zhCN from '../../../i18n/locales/zh-CN'
import type { KnowledgeBackfillBatch, KnowledgeBackfillEstimate } from '../../../api/knowledge-context'

const require = createRequire(import.meta.url)
const estimate: KnowledgeBackfillEstimate = { template_only_message_count: 205, pending_message_count: 205, batch_message_count: 200, remaining_message_count: 5, estimated_model_calls: 400 }
const batch: KnowledgeBackfillBatch = { id: 'batch-1', entity_id: 'entity-1', message_count: 200, success_count: 199, failure_count: 1, status: 'completed_with_failures', created_at: '2026-10-09T12:00:00Z', updated_at: '2026-10-09T12:01:00Z', items: [{ knowledge_id: 'message-failed', evidence_id: 'evidence-failed', status: 'failed', error: 'Message is being reprocessed' }] }

function compileComponent(filename: string, modules: Record<string, unknown>) {
  const { descriptor } = parse(readFileSync(new URL(`./${filename}`, import.meta.url), 'utf8'), { filename })
  const script = compileScript(descriptor, { id: filename })
  const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  return component
}

async function renderBackfill({ locale = 'zh-CN', kind = 'entity', enabled = true, compact = false, batches = [], api = {}, scenario = async () => {} }: {
  locale?: 'zh-CN' | 'en-US'; kind?: 'entity' | 'topic'; enabled?: boolean; compact?: boolean; batches?: KnowledgeBackfillBatch[]; api?: object; scenario?: (state: any, confirmations: any[], requests: any, beforeUnmount: () => void) => Promise<void>
} = {}) {
  const confirmations: any[] = []
  const requests = { starts: [] as object[], estimates: [] as object[], lists: [] as object[], sources: [] as string[], changes: 0, feedback: [] as string[] }
  const callbacks: Array<() => void> = []
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const envelope = (data: unknown) => Promise.resolve({ success: true, data })
  let currentBatches = batches
  const modules: Record<string, unknown> = {
    vue: { ...require('vue'), onBeforeUnmount: (callback: () => void) => callbacks.push(callback) },
    'vue-i18n': { useI18n: () => i18n.global },
    'tdesign-vue-next': { MessagePlugin: { success: (message: string) => requests.feedback.push(message) } },
    '@/components/settings/useConfirmDelete': { useConfirmDelete: () => (options: any) => { confirmations.push(options); return { hide() {} } } },
    '@/api/knowledge-context': {
      getKnowledgeBackfillEstimate: (target: object) => { requests.estimates.push(target); return envelope(estimate) },
      listKnowledgeBackfillBatches: (target: object) => { requests.lists.push(target); return envelope(currentBatches) },
      getKnowledgeBackfillBatch: () => envelope(batch),
      startKnowledgeBackfill: (target: object) => { requests.starts.push(target); currentBatches = [batch]; return envelope(batch) },
      ...api,
    },
  }
  modules['./EventsCoverageNotice.vue'] = { __esModule: true, default: compileComponent('EventsCoverageNotice.vue', modules) }
  const component = compileComponent('HistoryBackfill.vue', modules)
  const setup = component.setup
  let state: any
  component.setup = (props: object, context: object) => {
    state = setup(props, context)
    onServerPrefetch(async () => {
      await state.refresh()
      await scenario(state, confirmations, requests, () => callbacks.forEach(callback => callback()))
      await nextTick()
      state.clearPolling()
    })
    return state
  }
  const app = createSSRApp(component, { kind, targetId: 'entity-1', name: 'Aurora', enabled, compact, showCoverage: true, onChanged: () => requests.changes++, onOpenSource: (id: string) => requests.sources.push(id) })
  const stub = defineComponent({ props: ['visible', 'message', 'description', 'disabled'], setup: (props, { attrs, slots }) => () => h('div', { ...attrs, 'data-disabled': props.disabled || undefined }, [props.message, props.description, slots.default?.()]) })
  const dialog = defineComponent({ props: ['visible'], setup: (props, { slots }) => () => props.visible ? h('div', { role: 'dialog' }, slots.default?.()) : null })
  for (const name of ['t-button', 't-tag', 't-empty']) app.component(name, stub)
  app.component('t-dialog', dialog)
  try { return { html: await renderToString(app), state, confirmations, requests } }
  finally { callbacks.forEach(callback => callback()) }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: coverage, message count, batch limit and call estimate use localized copy`, async () => {
    const { html, requests } = await renderBackfill({ locale })
    assert.equal(requests.starts.length, 0, 'opening a starred profile never starts backfill automatically')
    assert.match(html, /205/)
    assert.match(html, /400/)
    assert.match(html, /200/)
    assert.match(html, /5/)
    assert.match(html, locale === 'zh-CN' ? /只包含收录时命中关注范围的消息/ : /Only messages in focus at ingestion are included/)
    assert.match(html, locale === 'zh-CN' ? /实际次数随事件数变化/ : /Actual calls vary with the number of events/)
    assert.doesNotMatch(html, /knowledgeWorkbench\./)
  })

  test(`${locale}: explicit confirmation starts one batch and shows its result and failures`, async () => {
    const { html, requests, confirmations } = await renderBackfill({ locale, kind: 'topic', scenario: async (state, confirmations, requests) => {
      state.confirmStart()
      assert.equal(requests.starts.length, 0)
      assert.match(confirmations[0].body, /200/)
      assert.match(confirmations[0].body, /400/)
      assert.match(confirmations[0].body, /5/)
      await confirmations[0].onConfirm()
    } })
    assert.equal(confirmations.length, 1)
    assert.deepEqual(requests.starts, [{ topic_id: 'entity-1' }])
    assert.equal(requests.changes, 1)
    assert.match(html, /199/)
    assert.match(html, /Message is being reprocessed/)
    assert.match(html, locale === 'zh-CN' ? /已完成，部分失败/ : /Completed with failures/)
    assert.match(html, locale === 'zh-CN' ? /消息 200 条，成功 199 条，失败 1 条/ : /200 messages, 199 succeeded, 1 failed/)
    assert.doesNotMatch(html, /knowledgeWorkbench\./)
  })

  test(`${locale}: real HTTP rejection keeps its plain object message`, async () => {
    const { html, requests } = await renderBackfill({
      locale,
      api: { startKnowledgeBackfill: () => Promise.reject({ status: 503, message: 'Maintenance queue unavailable' }) },
      scenario: async (state, confirmations) => { state.confirmStart(); await confirmations[0].onConfirm() },
    })
    assert.match(html, /Maintenance queue unavailable/)
    assert.equal(requests.changes, 0)
  })

  test(`${locale}: an unavailable estimate does not hide valid batch results or failures`, async () => {
    const { html, confirmations } = await renderBackfill({
      locale, batches: [batch],
      api: { getKnowledgeBackfillEstimate: () => Promise.reject({ status: 400, message: 'Extraction model unavailable' }) },
      scenario: async state => { state.confirmStart(); await state.openResults() },
    })
    assert.equal(confirmations.length, 0)
    assert.match(html, /Extraction model unavailable/)
    assert.match(html, /199/)
    assert.match(html, /Message is being reprocessed/)
    assert.match(html, locale === 'zh-CN' ? /相关模板抽取消息数暂不可用/ : /The related template-only message count is unavailable/)
    assert.doesNotMatch(html, locale === 'zh-CN' ? /待补抽 205/ : /205 messages pending/)
  })

  test(`${locale}: a known zero count remains distinct from an unavailable count`, async () => {
    const { html } = await renderBackfill({ locale, api: { getKnowledgeBackfillEstimate: () => Promise.resolve({ success: true, data: { ...estimate, template_only_message_count: 0, pending_message_count: 0, batch_message_count: 0, remaining_message_count: 0, estimated_model_calls: 0 } }) } })
    assert.match(html, locale === 'zh-CN' ? /相关消息中仍有 0 条仅抽取了模板字段/ : /0 related messages still have only template fields extracted/)
    assert.doesNotMatch(html, locale === 'zh-CN' ? /相关模板抽取消息数暂不可用/ : /The related template-only message count is unavailable/)
  })
}

test('an estimate failure invalidates the previous budget and still refreshes active batches', async () => {
  let unavailable = false
  const active = { ...batch, status: 'processing' }
  const { state, confirmations, html } = await renderBackfill({
    api: {
      getKnowledgeBackfillEstimate: () => unavailable ? Promise.reject({ status: 503, message: 'Estimate unavailable' }) : Promise.resolve({ success: true, data: estimate }),
      listKnowledgeBackfillBatches: () => Promise.resolve({ success: true, data: unavailable ? [active] : [] }),
    },
    scenario: async state => {
      assert.equal(state.estimateValid.value, true)
      unavailable = true
      await state.refresh()
      state.confirmStart()
      assert.equal(state.estimateValid.value, false)
      assert.equal(state.batches.value[0].status, 'processing')
      assert.ok(state.pollTimer, 'active batch refresh continues even when estimates fail')
    },
  })
  assert.equal(confirmations.length, 0)
  assert.equal(state.estimateValid.value, false)
  assert.match(html, /Estimate unavailable/)
  assert.doesNotMatch(html, /待补抽 205/)
})

test('refreshing an estimate prevents confirmation until both results are ready', async () => {
  let delayed = false
  let finish!: (value: unknown) => void
  const { confirmations } = await renderBackfill({
    api: { getKnowledgeBackfillEstimate: () => delayed ? new Promise(resolve => { finish = resolve }) : Promise.resolve({ success: true, data: estimate }) },
    scenario: async state => {
      delayed = true
      const pending = state.refresh()
      state.confirmStart()
      assert.equal(state.estimateValid.value, false)
      assert.equal(state.loading.value, true)
      finish({ success: true, data: estimate })
      await pending
      assert.equal(state.estimateValid.value, true)
    },
  })
  assert.equal(confirmations.length, 0)
})

test('an unstarred topic shows coverage and an instruction without the backfill action', async () => {
  const { html, requests } = await renderBackfill({ enabled: false, kind: 'topic', scenario: async state => { state.confirmStart() } })
  assert.equal(requests.starts.length, 0)
  assert.match(html, /加星标后可手动补抽历史消息/)
  assert.doesNotMatch(html, />补抽历史消息</)
})

test('an active batch prevents duplicate starts and keeps successful and failed counts visible', async () => {
  const active = { ...batch, status: 'processing' }
  const { html, confirmations } = await renderBackfill({ batches: [active], scenario: async state => { state.confirmStart() } })
  assert.equal(confirmations.length, 0)
  assert.match(html, /补抽正在进行/)
  assert.match(html, /data-disabled="true"/)
})

test('a rejected backfill preserves the estimate and shows an actionable error', async () => {
  const { html, requests } = await renderBackfill({ api: { startKnowledgeBackfill: () => Promise.reject(new Error('Maintenance queue unavailable')) }, scenario: async (state, confirmations) => { state.confirmStart(); await confirmations[0].onConfirm() } })
  assert.match(html, /Maintenance queue unavailable/)
  assert.match(html, /205/)
  assert.equal(requests.changes, 0)
  assert.equal(requests.feedback.length, 0)
})

test('canceling a confirmation permits a later explicit confirmation', async () => {
  const { confirmations, requests } = await renderBackfill({ scenario: async (state, confirmations) => {
    state.confirmStart()
    state.confirmStart()
    assert.equal(confirmations.length, 1, 'a visible confirmation cannot be opened twice')
    confirmations[0].onClose()
    state.confirmStart()
    assert.equal(confirmations.length, 2)
    await confirmations[1].onConfirm()
  } })
  assert.equal(confirmations.length, 2)
  assert.equal(requests.starts.length, 1)
})

test('a pending queue request blocks duplicate confirmation and submission', async () => {
  let finish!: (response: object) => void
  let starts = 0
  await renderBackfill({
    api: { startKnowledgeBackfill: () => { starts++; return new Promise(resolve => { finish = resolve }) } },
    scenario: async (state, confirmations) => {
      state.confirmStart()
      const pending = confirmations[0].onConfirm()
      state.confirmStart()
      await state.start({ entity_id: 'entity-1' })
      assert.equal(starts, 1)
      assert.equal(confirmations.length, 1)
      assert.equal(state.starting.value, true)
      finish({ success: true, data: batch })
      await pending
    },
  })
})

test('older batch details cannot replace the current selection', async () => {
  const responses = new Map<string, (response: object) => void>()
  await renderBackfill({
    api: { getKnowledgeBackfillBatch: (id: string) => new Promise(resolve => responses.set(id, resolve)) },
    scenario: async state => {
      const first = state.loadBatchDetails('batch-old')
      const second = state.loadBatchDetails('batch-new')
      responses.get('batch-new')!({ success: true, data: { ...batch, id: 'batch-new' } })
      await second
      responses.get('batch-old')!({ success: true, data: { ...batch, id: 'batch-old' } })
      await first
      assert.equal(state.selectedBatch.value.id, 'batch-new')
      assert.equal(state.detailsLoading.value, false)
    },
  })
})

test('an estimate response after unmount cannot replace the prior count', async () => {
  let delayed = false
  let finish!: (response: object) => void
  await renderBackfill({
    api: { getKnowledgeBackfillEstimate: () => delayed ? new Promise(resolve => { finish = resolve }) : Promise.resolve({ success: true, data: estimate }) },
    scenario: async (state, _confirmations, _requests, beforeUnmount) => {
      delayed = true
      const pending = state.refresh()
      beforeUnmount()
      finish({ success: true, data: { ...estimate, template_only_message_count: 999 } })
      await pending
      assert.equal(state.estimate.value.template_only_message_count, 205)
    },
  })
})

test('each terminal batch refreshes its estimate and notifies the workbench once while other batches remain active', async () => {
  for (const status of ['completed', 'completed_with_failures', 'failed']) {
    let completed = false
    const other = { ...batch, id: 'batch-2', status: 'processing' }
    const { requests } = await renderBackfill({
      api: {
        listKnowledgeBackfillBatches: () => Promise.resolve({ success: true, data: [{ ...batch, status: completed ? status : 'processing' }, other] }),
      },
      scenario: async (state, _confirmations, requests) => {
        const estimatesBefore = requests.estimates.length
        completed = true
        await state.refresh()
        assert.equal(requests.changes, 1)
        assert.equal(requests.estimates.length, estimatesBefore + 1)
        assert.equal(state.estimateValid.value, true)
        await state.refresh()
      },
    })
    assert.equal(requests.changes, 1)
  }
})
