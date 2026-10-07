import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch } from 'vue'
import ts from 'typescript'
import * as display from './knowledgeContextDisplay'
import * as scope from '../../../api/knowledge-context-scope'
import type { KnowledgeEntity, KnowledgeEvent, KnowledgeEvidence, KnowledgeFact } from '../../../api/knowledge-context'

const require = createRequire(import.meta.url)
const filename = 'KnowledgeWorkbench.vue'
const source = readFileSync(new URL('./KnowledgeWorkbench.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename })
const script = compileScript(descriptor, { id: 'workbench-render-test' })
const template = compileTemplate({
  id: 'workbench-render-test', filename, source: descriptor.template!.content,
  ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings },
})
assert.deepEqual(template.errors, [])
const compile = (code: string) => ts.transpileModule(code, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

const entity = { id: 'object-a', canonical_name: 'Aurora', entity_type: 'model', aliases: [] } as unknown as KnowledgeEntity
const fact = {
  id: 'fact-a', subject_entity_id: entity.id, kind: 'attribute', predicate: 'license',
  value: 'Apache-2.0', status: 'current', evidence_id: 'evidence-a', knowledge_base_id: 'kb-a', quote: 'Apache license',
} as KnowledgeFact
const evidence = ['a', 'b', 'unrelated'].map(id => ({
  id: `evidence-${id}`, title: `Source ${id}`, knowledge_base_id: 'kb-a', knowledge_base_name: 'Team library',
  publisher: 'Publisher', knowledge_id: `message-${id}`, body_hash: 'abc', version: 1, status: 'current', extraction_status: 'complete',
})) as KnowledgeEvidence[]
const event = {
  id: 'event-a', title: 'Model release', source_count: 2, heat: 10, knowledge_base_id: 'kb-a',
  evidence_ids: ['evidence-a', 'evidence-b'],
} as KnowledgeEvent

type Scenario = (state: any, beforeUnmount: () => void) => Promise<void>
async function renderWorkbench({
  role = 'contributor', result = {}, scenario = async () => {}, status = { pending_count: 1, latest_run: null },
}: { role?: string; result?: object; scenario?: Scenario; status?: object } = {}) {
  const requests = { status: 0, rebuild: 0, query: [] as any[] }
  const cleanupCallbacks: Array<() => void> = []
  const envelope = (data: unknown) => Promise.resolve({ success: true, data })
  const api = {
    listKnowledgeContextEntities: () => envelope([entity]),
    listKnowledgeContextEvents: () => envelope([]),
    listKnowledgeContextEvidence: () => envelope(evidence),
    getGlobalKnowledgeEntityContext: () => envelope(result),
    queryKnowledgeContext: (_kbId: string, query: object) => { requests.query.push(query); return envelope(result) },
    getKnowledgeNameEmbeddingRebuildStatus: () => { requests.status++; return envelope(status) },
    rebuildKnowledgeNameEmbeddings: () => {
      requests.rebuild++
      return envelope({ pending_count: 1, latest_run: { status: 'pending', processed_count: 0, total_count: 1 } })
    },
  }
  const emptyComponent = defineComponent({ setup: () => () => null })
  const modules: Record<string, unknown> = {
    vue: { ...require('vue'), onBeforeUnmount: (callback: () => void) => { cleanupCallbacks.push(callback) } },
    'tdesign-vue-next': { MessagePlugin: { success() {} } },
    'vue-i18n': { useI18n: () => ({ t: (key: string, params?: object) => params ? `${key} ${JSON.stringify(params)}` : key }) },
    './MergeSuggestions.vue': { __esModule: true, default: emptyComponent },
    './EvidenceExtractionDetails.vue': { __esModule: true, default: emptyComponent },
    './knowledgeContextDisplay': display,
    '@/stores/auth': { useAuthStore: () => ({ hasRole: () => ['admin', 'owner'].includes(role) }) },
    '@/api/knowledge-domains': { listKnowledgeDomains: () => envelope([]) },
    '@/api/knowledge-context-scope': scope,
    '@/api/knowledge-context': api,
    '@/api/knowledge-workflows': {},
    '@/api/chat/index': {},
    '@/utils/security': { safeMarkdownToHTML: (value: string) => value },
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    new Function('require', 'module', 'exports', compile(code))(
      (name: string) => modules[name] ?? require(name), module, module.exports,
    )
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  component.setup = (props: object, context: object) => {
    const state = setup(props, context)
    onServerPrefetch(async () => {
      try {
        await state.refreshAll()
        if (['admin', 'owner'].includes(role)) await state.loadNameEmbeddingStatus()
        await scenario(state, () => { cleanupCallbacks.forEach(callback => callback()) })
        await nextTick()
      } finally { state.clearNameEmbeddingPolling() }
    })
    return state
  }
  const app = createSSRApp(component, { kbId: 'kb-a', domainId: 'domain-a', canManage: role !== 'viewer' })
  const stub = defineComponent({
    props: ['message', 'description', 'disabled', 'visible'],
    setup(props, { attrs, slots }) {
      return () => h('div', { ...attrs, 'data-disabled': props.disabled || undefined }, [props.message, props.description, slots.default?.()])
    },
  })
  for (const name of ['t-button', 't-icon', 't-select', 't-input', 't-input-number', 't-checkbox', 't-alert', 't-empty', 't-loading', 't-textarea', 't-tag']) app.component(name, stub)
  app.component('t-drawer', emptyComponent)
  const errors: unknown[] = []
  app.config.errorHandler = error => { errors.push(error) }
  const html = await renderToString(app)
  assert.deepEqual(errors, [], 'the actual Vue template and nested cards render without an exception')
  return { html, requests }
}

for (const events of [[], null]) {
  test(`an object with events ${JSON.stringify(events)} still renders its facts`, async () => {
    const { html } = await renderWorkbench({
      result: { entities: [entity], facts: [fact], events, evidence: null, attention: null },
      scenario: async state => { await state.selectSection('entities'); await state.selectEntity(entity) },
    })
    assert.match(html, /class="fact-card"/)
    assert.match(html, /license/)
    assert.match(html, /Apache-2.0/)
    assert.doesNotMatch(html, /class="event-card"/)
  })
}

test('null query lists render the empty result state', async () => {
  const { html } = await renderWorkbench({
    result: { entities: null, facts: null, events: null, evidence: null, attention: null },
    scenario: state => state.runQuery(),
  })
  assert.match(html, /knowledgeWorkbench.query.empty/)
})

for (const queryScope of ['knowledge_base', 'domain', 'team']) {
  test(`${queryScope} event cards only render evidence named by evidence_ids`, async () => {
    const { html, requests } = await renderWorkbench({
      result: { entities: [entity], facts: [], events: [event], evidence, attention: null },
      scenario: async state => { state.queryScope.value = queryScope; await state.runQuery() },
    })
    assert.equal(requests.query[0].scope, queryScope)
    const card = html.match(/<article class="event-card"[^>]*>(.*?)<\/article>/s)?.[1]
    assert.ok(card, 'the event card is present in the rendered query results')
    assert.match(card, /Source a/)
    assert.match(card, /Source b/)
    assert.doesNotMatch(card, /Source unrelated/)
    assert.match(html, /Source unrelated/, 'unrelated evidence remains available in the separate evidence results')
  })
}

test('object details use the same event evidence filter', async () => {
  const { html } = await renderWorkbench({
    result: { entities: [entity], facts: [], events: [event], evidence, attention: null },
    scenario: async state => { await state.selectSection('entities'); await state.selectEntity(entity) },
  })
  assert.match(html, /Source a/)
  assert.doesNotMatch(html, /Source unrelated/)
})

test('an event with null evidence_ids does not inherit unrelated query evidence', async () => {
  const { html } = await renderWorkbench({
    result: { events: [{ ...event, evidence_ids: null }], evidence, attention: null },
    scenario: state => state.runQuery(),
  })
  const card = html.match(/<article class="event-card"[^>]*>(.*?)<\/article>/s)?.[1]
  assert.ok(card)
  assert.doesNotMatch(card, /Source a|Source b|Source unrelated/)
})

for (const role of ['owner', 'admin']) {
  test(`${role} can view name embedding status and request a rebuild`, async () => {
    const { html, requests } = await renderWorkbench({ role, scenario: state => state.rebuildNameEmbeddings() })
    assert.ok(requests.status > 0)
    assert.equal(requests.rebuild, 1)
    assert.match(html, /name-embedding-rebuild/)
    assert.match(html, /knowledgeWorkbench.nameEmbeddings.status.pending/)
  })
}

for (const role of ['contributor', 'viewer']) {
  test(`${role} does not request or display the administrator rebuild control`, async () => {
    const { html, requests } = await renderWorkbench({ role, scenario: state => state.rebuildNameEmbeddings() })
    assert.equal(requests.status, 0)
    assert.equal(requests.rebuild, 0)
    assert.doesNotMatch(html, /name-embedding-rebuild/)
  })
}

test('latest rebuild failures remain visible and evidence metadata has a separator', async () => {
  const { html } = await renderWorkbench({
    role: 'admin', status: { pending_count: 1, latest_run: { status: 'failed', error: 'Embedding provider unavailable', processed_count: 0, total_count: 1 } },
    scenario: state => state.selectSection('evidence'),
  })
  assert.match(html, /knowledgeWorkbench.nameEmbeddings.status.failed/)
  assert.match(html, /Embedding provider unavailable/)
  assert.match(html, /Team library<\/span><span[^>]*><span class="evidence-card__separator"[^>]*> · <\/span>Publisher/)
})

test('unmount cancels name embedding polling and invalidates outstanding status requests', async () => {
  await renderWorkbench({
    role: 'admin',
    scenario: async (state, beforeUnmount) => {
      await state.rebuildNameEmbeddings()
      assert.ok(state.nameEmbeddingPollTimer)
      const request = state.nameEmbeddingRequest
      beforeUnmount()
      assert.equal(state.nameEmbeddingPollTimer, null)
      assert.equal(state.nameEmbeddingRequest, request + 1)
    },
  })
})
