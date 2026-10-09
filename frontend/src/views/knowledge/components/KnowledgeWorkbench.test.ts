import { formatCountCopy } from '../../../i18n/countCopy'
import assert from 'node:assert/strict'
import test, { mock } from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import zhCN from '../../../i18n/locales/zh-CN'
import enUS from '../../../i18n/locales/en-US'
import ts from 'typescript'
import * as display from './knowledgeContextDisplay'
import * as scope from '../../../api/knowledge-context-scope'
import * as markdownCitation from '../../../utils/markdownCitation'
import { compiledSFCStyleRules, LONG_SOURCE_URL, styleDeclarationsAtWidth } from '../../../utils/sfcStyleTestHelpers'
import { reportCitationClick, reportMarkdownTestComponent } from './reportMarkdownTestHarness'
import type { KnowledgeEntity, KnowledgeEvent, KnowledgeEvidence, KnowledgeFact, KnowledgeFocusScope } from '../../../api/knowledge-context'

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
  result = {}, scenario = async () => {}, status = { pending_count: 1, latest_run: null }, workflows = {},
  focus = {}, focusApi = {},
  locale,
}: { result?: object; scenario?: Scenario; status?: object; workflows?: object; focus?: Partial<KnowledgeFocusScope>; focusApi?: object; locale?: 'zh-CN' | 'en-US' } = {}) {
  const requests = { status: 0, rebuild: 0, query: [] as any[], snapshots: [] as string[], focus: [] as object[], stars: [] as Array<{ kind: string; id: string; starred: boolean }> }
  const cleanupCallbacks: Array<() => void> = []
  const envelope = (data: unknown) => Promise.resolve({ success: true, data })
  let focusData: KnowledgeFocusScope = { starred_entities: [], starred_topics: [], tracked_entities: [], tracked_topics: [], available_topics: [], ...focus }
  function setStar(kind: 'entity' | 'topic', id: string, starred: boolean) {
    requests.stars.push({ kind, id, starred })
    if (kind === 'entity') focusData = { ...focusData, starred_entities: starred ? [entity] : [] }
    else focusData = { ...focusData, starred_topics: starred ? focusData.available_topics.filter(item => item.id === id) : [] }
    return Promise.resolve({ success: true })
  }
  const api = {
    getKnowledgeFocusScope: (params: object) => { requests.focus.push(params); return envelope(focusData) },
    starKnowledgeEntity: (id: string) => setStar('entity', id, true),
    unstarKnowledgeEntity: (id: string) => setStar('entity', id, false),
    starKnowledgeTopic: (id: string) => setStar('topic', id, true),
    unstarKnowledgeTopic: (id: string) => setStar('topic', id, false),
    listKnowledgeContextEntities: () => envelope([entity]),
    listKnowledgeContextEvents: () => envelope([]),
    listKnowledgeContextEvidence: () => envelope(evidence),
    getGlobalKnowledgeEntityContext: () => envelope(result),
    getGlobalKnowledgeEvidence: (id: string) => { requests.snapshots.push(id); return envelope({ id, body: 'Acme 发布了 **新模型**。' }) },
    queryKnowledgeContext: (_kbId: string, query: object) => { requests.query.push(query); return envelope(result) },
    getKnowledgeNameEmbeddingRebuildStatus: () => { requests.status++; return envelope(status) },
    rebuildKnowledgeNameEmbeddings: () => {
      requests.rebuild++
      return envelope({ pending_count: 1, latest_run: { status: 'pending', processed_count: 0, total_count: 1 } })
    },
    ...focusApi,
  }
  const emptyComponent = defineComponent({ setup: () => () => null })
  const i18n = locale ? createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } }) : null
  const reportComponent = reportMarkdownTestComponent()
  const modules: Record<string, unknown> = {
    vue: { ...require('vue'), onBeforeUnmount: (callback: () => void) => { cleanupCallbacks.push(callback) } },
    'tdesign-vue-next': { MessagePlugin: { success() {} } },
    'vue-i18n': { useI18n: () => i18n?.global || ({ t: (key: string, params?: object) => params ? `${key} ${JSON.stringify(params)}` : key }) },
    './MergeSuggestions.vue': { __esModule: true, default: emptyComponent },
    './EvidenceExtractionDetails.vue': { __esModule: true, default: emptyComponent },
    './HistoryBackfill.vue': { __esModule: true, default: emptyComponent },
    './EventsCoverageNotice.vue': { __esModule: true, default: defineComponent({ props: ['count', 'loading'], setup: props => () => h('div', { class: 'events-coverage-notice' }, `knowledgeWorkbench.events.coverageNotice ${props.loading ? 'knowledgeWorkbench.events.countLoading' : props.count === null ? 'knowledgeWorkbench.events.countUnavailable' : `knowledgeWorkbench.events.templateOnlyCount ${props.count}`}`) }) },
    './RepostSources.vue': { __esModule: true, default: emptyComponent },
    './ReportMarkdown.vue': { __esModule: true, default: reportComponent },
    './knowledgeContextDisplay': display,
    '@/stores/auth': { useAuthStore: () => ({ isLoggedIn: true }) },
    '@/api/knowledge-domains': { listKnowledgeDomains: () => envelope([]) },
    '@/api/knowledge-context-scope': scope,
    '@/api/knowledge-context': api,
    '@/api/knowledge-workflows': workflows,
    '@/api/chat/index': {},
    '@/utils/security': { safeMarkdownToHTML: (value: string) => value },
    '@/utils/markdownCitation': markdownCitation,
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    new Function('require', 'module', 'exports', compile(code))(
      (name: string) => modules[name] ?? (name === '@/i18n/countCopy' ? { formatCountCopy } : require(name)), module, module.exports,
    )
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  let workbenchState: any
  component.setup = (props: object, context: object) => {
    const state = setup(props, context)
    workbenchState = state
    onServerPrefetch(async () => {
      try {
        await state.refreshAll()
        await state.loadNameEmbeddingStatus()
        await scenario(state, () => { cleanupCallbacks.forEach(callback => callback()) })
        await nextTick()
      } finally { state.clearNameEmbeddingPolling() }
    })
    return state
  }
  const app = createSSRApp(component, { kbId: 'kb-a', domainId: 'domain-a' })
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
  return { html, requests, state: workbenchState, reportState: reportComponent.testState }
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

for (const queryScope of ['knowledge_base', 'domain', 'all']) {
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

for (const queryScope of ['knowledge_base', 'domain', 'all'] as const) {
  test(`${queryScope} event timeline gets a server count and displays coverage even when empty`, async () => {
    const { html, requests } = await renderWorkbench({
      result: { template_only_message_count: 7, entities: [], facts: [], events: [], evidence: [], attention: [] },
      scenario: async state => { state.queryScope.value = queryScope; await nextTick(); await state.selectSection('events') },
    })
    assert.match(html, /knowledgeWorkbench.events.coverageNotice/)
    assert.match(html, /knowledgeWorkbench.events.templateOnlyCount 7/)
    assert.equal(requests.query.at(-1).scope, queryScope)
    assert.equal(requests.query.at(-1).limit, 1, 'the full count is independent of returned records')
  })
}

test('a topic opens its profile in the existing workbench without changing routes', async () => {
  const topic = { id: 'tag-a', name: 'AI releases', kind: 'topic', knowledge_base_id: 'kb-a' }
  const { state } = await renderWorkbench({
    focus: { starred_topics: [topic] },
    scenario: async state => { await state.selectSection('focus'); state.openFocusTopic(topic) },
  })
  assert.equal(state.topicProfileVisible.value, true)
  assert.equal(state.selectedTopic.value.id, topic.id)
  assert.equal(state.isTopicStarred(topic.id), true)
})

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: starring an entity reveals backfill at the same list row and opens its profile`, async () => {
    const { html, state, requests } = await renderWorkbench({
      locale, result: { entities: [entity], facts: [], events: [], evidence: [], attention: [] },
      scenario: async state => {
        await state.selectSection('entities')
        await state.toggleFocusStar('entity', entity)
        await state.openFocusEntity(entity)
      },
    })
    const row = html.match(/<div class="entity-list__row">([\s\S]*?)<\/div><\/div>/)?.[1]
    assert.ok(row)
    assert.match(row, /class="focus-backfill-entry"/)
    assert.match(row, locale === 'zh-CN' ? /补抽历史消息/ : /Backfill historical messages/)
    assert.equal(state.selectedEntity.value.id, entity.id)
    assert.equal(state.activeSection.value, 'entities')
    assert.equal(requests.rebuild, 0)
  })

  test(`${locale}: starring a topic reveals backfill on the same topic card and opens its profile`, async () => {
    const topic = { id: 'tag-a', name: 'AI releases', kind: 'topic', knowledge_base_id: 'kb-a' }
    const { html, state, requests } = await renderWorkbench({
      locale, focus: { available_topics: [topic] },
      scenario: async state => {
        await state.selectSection('focus')
        await state.toggleFocusStar('topic', topic)
        state.openFocusTopic(topic)
      },
    })
    const card = html.match(/<div class="focus-topic">([\s\S]*?)<\/div><\/div>/)?.[1]
    assert.ok(card)
    assert.match(card, /class="focus-backfill-entry"/)
    assert.match(card, locale === 'zh-CN' ? /补抽历史消息/ : /Backfill historical messages/)
    assert.equal(state.topicProfileVisible.value, true)
    assert.equal(state.selectedTopic.value.id, topic.id)
    assert.equal(requests.rebuild, 0)
  })
}

test('an event count HTTP failure shows unavailable instead of a zero count', async () => {
  const { html, state } = await renderWorkbench({
    focusApi: { queryKnowledgeContext: () => Promise.reject({ status: 500, message: 'Coverage query unavailable' }) },
    scenario: async state => { await state.selectSection('events') },
  })
  assert.equal(state.eventsTemplateOnlyCount.value, null)
  assert.match(html, /knowledgeWorkbench.events.countUnavailable/)
  assert.doesNotMatch(html, /knowledgeWorkbench.events.templateOnlyCount 0/)
  assert.equal(state.loadError.value, 'Coverage query unavailable')
})

test('refreshing an event count clears its previous value until the server responds', async () => {
  let finish!: (value: unknown) => void
  const { state } = await renderWorkbench({
    focusApi: { queryKnowledgeContext: () => new Promise(resolve => { finish = resolve }) },
    scenario: async state => {
      state.eventsTemplateOnlyCount.value = 3
      const pending = state.loadEvents()
      assert.equal(state.eventsTemplateOnlyCount.value, null)
      assert.equal(state.eventsLoading.value, true)
      finish({ success: true, data: { template_only_message_count: 0 } })
      await pending
      assert.equal(state.eventsTemplateOnlyCount.value, 0)
    },
  })
  assert.equal(state.eventsTemplateOnlyCount.value, 0)
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

test('signed-in user can view name embedding status and request a rebuild', async () => {
  const { html, requests } = await renderWorkbench({ scenario: state => state.rebuildNameEmbeddings() })
  assert.ok(requests.status > 0)
  assert.equal(requests.rebuild, 1)
  assert.match(html, /name-embedding-rebuild/)
  assert.match(html, /knowledgeWorkbench.nameEmbeddings.status.pending/)
})

test('latest rebuild failures remain visible and evidence metadata has a separator', async () => {
  const { html } = await renderWorkbench({
    status: { pending_count: 1, latest_run: { status: 'failed', error: 'Embedding provider unavailable', processed_count: 0, total_count: 1 } },
    scenario: state => state.selectSection('evidence'),
  })
  assert.match(html, /knowledgeWorkbench.nameEmbeddings.status.failed/)
  assert.match(html, /Embedding provider unavailable/)
  assert.match(html, /Team library<\/span><span[^>]*><span class="evidence-card__separator"[^>]*> · <\/span>Publisher/)
})

test('workbench displays missing publication exclusions returned by the query', async () => {
  const { html } = await renderWorkbench({
    result: { excluded_missing_published_at_count: 7 },
    scenario: state => state.runQuery(),
  })
  assert.match(html, /knowledgeWorkbench.sourceTime.excluded/)
  assert.match(html, /&quot;count&quot;:7/)
})

for (const queryScope of ['knowledge_base', 'domain', 'all']) {
  test(`${queryScope} structured queries display the template-only message count`, async () => {
    const { html, requests } = await renderWorkbench({
      result: { template_only_message_count: 9 },
      scenario: async state => { state.queryScope.value = queryScope; await state.runQuery() },
    })
    assert.equal(requests.query[0].scope, queryScope)
    assert.match(html, /knowledgeWorkbench.query.templateOnlyMessages \{&quot;count&quot;:9\}/)
  })
}

test('object list and details expose the same accessible star state and preserve existing facts', async () => {
  const { html, requests, state } = await renderWorkbench({
    result: { facts: [fact], template_only_message_count: 4 },
    scenario: async state => {
      await state.selectSection('entities')
      await state.selectEntity(entity)
      await state.toggleFocusStar('entity', entity)
    },
  })
  assert.deepEqual(requests.stars, [{ kind: 'entity', id: entity.id, starred: true }])
  assert.equal(state.entityContext.value.facts[0].id, fact.id)
  assert.equal(requests.rebuild, 0)
  assert.equal(requests.query.length, 0)
  assert.equal(requests.snapshots.length, 0)
  assert.equal((html.match(/aria-pressed="true"/g) || []).length, 2)
  assert.match(html, /knowledgeWorkbench.focus.unstarNamed/)
  assert.match(html, /knowledgeWorkbench.events.coverageNotice/)
  assert.match(html, /knowledgeWorkbench.events.templateOnlyCount 4/)
})

test('removing an entity star refreshes focus without querying or rebuilding historical sources', async () => {
  const { html, requests, state } = await renderWorkbench({
    focus: { starred_entities: [entity] },
    result: { facts: [fact], events: [event] },
    scenario: async state => {
      await state.selectSection('entities')
      await state.selectEntity(entity)
      await state.toggleFocusStar('entity', entity)
    },
  })
  assert.deepEqual(requests.stars, [{ kind: 'entity', id: entity.id, starred: false }])
  assert.equal(state.entityContext.value.facts.length, 1)
  assert.equal(state.entityContext.value.events.length, 1)
  assert.equal(requests.rebuild, 0)
  assert.equal(requests.query.length, 0)
  assert.equal((html.match(/aria-pressed="false"/g) || []).length, 2)
})

test('focus lists starred topics, tracked objects, and topic tag star controls', async () => {
  const topic = { id: 'tag-topic', name: 'Agent tools', kind: 'topic', knowledge_base_id: 'kb-a', knowledge_base_name: 'Team library' }
  const { html, requests } = await renderWorkbench({
    focus: { available_topics: [topic], tracked_entities: [entity] },
    scenario: async state => {
      await state.selectSection('focus')
      await state.toggleFocusStar('topic', topic)
    },
  })
  assert.deepEqual(requests.stars, [{ kind: 'topic', id: topic.id, starred: true }])
  assert.match(html, /knowledgeWorkbench.focus.starredTopics/)
  assert.match(html, /knowledgeWorkbench.focus.trackedEntities/)
  assert.match(html, /knowledgeWorkbench.focus.futureMessages/)
  assert.match(html, /Agent tools/)
  assert.match(html, /Team library/)
  assert.match(html, /aria-pressed="true"/)
})

test('failed star mutations retain the prior star state and show an inline retryable error', async () => {
  const { html, state } = await renderWorkbench({
    focus: { starred_entities: [entity] },
    focusApi: { unstarKnowledgeEntity: () => Promise.resolve({ success: false, error: 'Star update unavailable' }) },
    scenario: async state => { await state.selectSection('entities'); await state.toggleFocusStar('entity', entity) },
  })
  assert.equal(state.isEntityStarred(entity.id), true)
  assert.equal(state.starPending.size, 0)
  assert.match(html, /role="alert"[^>]*>Star update unavailable/)
})

test('duplicate star requests remain blocked while a mutation is pending', async () => {
  let release: (response: object) => void = () => {}
  let calls = 0
  await renderWorkbench({
    focusApi: { starKnowledgeEntity: () => { calls++; return new Promise(resolve => { release = resolve }) } },
    scenario: async state => {
      const pending = state.toggleFocusStar('entity', entity)
      assert.equal(state.starPending.has(`entity:${entity.id}`), true)
      await state.toggleFocusStar('entity', entity)
      assert.equal(calls, 1)
      release({ success: true })
      await pending
      assert.equal(state.starPending.size, 0)
    },
  })
})

test('signed-in user can inspect focus and change stars', async () => {
  const { html, requests } = await renderWorkbench({
    focus: { starred_entities: [] },
    scenario: async state => { await state.selectSection('focus'); await state.toggleFocusStar('entity', entity) },
  })
  assert.match(html, /Aurora/)
  assert.match(html, /class="focus-star/)
  assert.equal(requests.stars.length, 1)
})

test('older focus responses cannot replace a more recent refresh', async () => {
  let scenarioStarted = false
  const pending: Array<(result: object) => void> = []
  await renderWorkbench({
    focusApi: { getKnowledgeFocusScope: () => scenarioStarted
      ? new Promise(resolve => pending.push(resolve))
      : Promise.resolve({ success: true, data: {} }) },
    scenario: async state => {
      scenarioStarted = true
      const older = state.loadFocus()
      const newer = state.loadFocus()
      pending[1]({ success: true, data: { starred_entities: [entity] } })
      await newer
      pending[0]({ success: true, data: { starred_entities: [] } })
      await older
      assert.equal(state.isEntityStarred(entity.id), true)
      assert.equal(state.focusLoading.value, false)
    },
  })
})

test('unmount cancels name embedding polling and invalidates outstanding status requests', async () => {
  await renderWorkbench({

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

const report = {
  id: 'report-1', title: 'Weekly/report', markdown: '# Weekly report\n\nRelease [C1]',
  created_at: '2026-10-08T00:00:00Z', excluded_missing_published_at_count: 2,
  citations: [{ id: 'C1', kind: 'evidence', evidence_id: 'report-evidence', quote: 'Acme 发布了 新模型。' }],
}

test('workbench evidence and report sources preserve complete long URLs without changing link labels', async () => {
  const { html: evidenceHtml } = await renderWorkbench({
    result: { evidence: [{ ...evidence[0], title: LONG_SOURCE_URL, original_url: LONG_SOURCE_URL }] },
    scenario: state => state.runQuery(),
  })
  assert.ok(evidenceHtml.includes(`href="${LONG_SOURCE_URL}"`))
  assert.ok(evidenceHtml.includes(`<strong>${LONG_SOURCE_URL}</strong>`))
  assert.match(evidenceHtml, /class="source-url"[^>]*>knowledgeWorkbench.evidence.openSource<\/a>/)
  const longReport = { ...report, markdown: LONG_SOURCE_URL,
    citations: [{ ...report.citations[0], title: LONG_SOURCE_URL, quote: LONG_SOURCE_URL, url: LONG_SOURCE_URL }] }
  const { html: reportHtml } = await renderWorkbench({ scenario: async state => {
    state.activeSection.value = 'reports'
    state.reports.value = [longReport]
    state.selectedReport.value = longReport
  } })
  assert.ok(reportHtml.includes(`>${LONG_SOURCE_URL}</a>`))
  assert.ok(reportHtml.includes(`href="${LONG_SOURCE_URL}" target="_blank" rel="noopener noreferrer">knowledgeWorkbench.evidence.openSource</a>`))
  assert.ok(reportHtml.includes(`“${LONG_SOURCE_URL}”`))
})

test('compiled workbench CSS lets evidence and citation flex items shrink at desktop and narrow widths', async () => {
  const rules = await compiledSFCStyleRules(new URL('./KnowledgeWorkbench.vue', import.meta.url))
  for (const width of [1440, 390]) {
    for (const selector of ['.evidence-card', '.evidence-card__main', '.evidence-card__main > *', '.report-view', '.citation-card']) {
      const style = styleDeclarationsAtWidth(rules, selector, width)
      assert.equal(style['min-width'], '0', `${selector} at ${width}`)
      assert.equal(style['max-width'], '100%', `${selector} at ${width}`)
      assert.equal(style['box-sizing'], 'border-box', `${selector} at ${width}`)
      assert.notEqual(style.overflow, 'hidden', `${selector} must not clip long source text`)
    }
    const source = styleDeclarationsAtWidth(rules, '.source-url', width)
    assert.equal(source['min-width'], '0')
    assert.equal(source['max-width'], '100%')
    assert.equal(source['white-space'], 'normal')
    assert.equal(source['overflow-wrap'], 'anywhere')
    const header = styleDeclarationsAtWidth(rules, '.report-view > header', width)
    assert.equal(header['flex-wrap'], 'wrap')
    assert.equal(styleDeclarationsAtWidth(rules, '.report-view > header > div', width)['min-width'], '0')
    assert.equal(styleDeclarationsAtWidth(rules, '.citation-card__actions', width)['flex-wrap'], 'wrap')
  }
})

test('report citation buttons use the workbench snapshot and original quote highlighting', async () => {
  const { html, requests, state, reportState } = await renderWorkbench({
    scenario: async state => {
      state.activeSection.value = 'reports'
      state.reports.value = [report]
      state.selectedReport.value = report
    },
  })
  assert.match(html, /<h1>Weekly report<\/h1>/)
  assert.match(html, /knowledgeWorkbench.reports.downloadJson/)
  assert.match(html, /knowledgeWorkbench.reports.download/)
  assert.match(html, /&quot;count&quot;:2/)
  reportState.openCitation(reportCitationClick('C1'))
  await Promise.resolve()
  await nextTick()
  assert.deepEqual(requests.snapshots, ['report-evidence'])
  assert.equal(state.snapshotVisible.value, true)
  assert.equal(state.selectedQuote.value, report.citations[0].quote)
  assert.equal(state.selectedSnapshot.value.id, 'report-evidence')
  assert.deepEqual(state.snapshotSegments.value.filter((segment: any) => segment.match).map((segment: any) => segment.text), ['Acme 发布了 **新模型**。'])
})

test('report JSON and Markdown downloads request the chosen format and clean up attachments', async t => {
  const originalDocument = Object.getOwnPropertyDescriptor(globalThis, 'document')
  const links: any[] = []
  Object.defineProperty(globalThis, 'document', { configurable: true, value: {
    createElement: () => {
      const link = { href: '', download: '', clicks: 0, removed: false, click() { this.clicks++ }, remove() { this.removed = true } }
      links.push(link)
      return link
    },
    body: { appendChild() {} },
  } })
  t.after(() => {
    if (originalDocument) Object.defineProperty(globalThis, 'document', originalDocument)
    else Reflect.deleteProperty(globalThis, 'document')
  })
  const revoked: string[] = []
  mock.method(URL, 'createObjectURL', () => `blob:report-${links.length}`)
  mock.method(URL, 'revokeObjectURL', (url: string) => revoked.push(url))
  t.after(() => mock.restoreAll())
  const requested: unknown[][] = []
  await renderWorkbench({
    workflows: { downloadKnowledgeWorkflowReport: (...args: unknown[]) => { requested.push(args); return Promise.resolve(new Blob(['report'])) } },
    scenario: async state => {
      await state.downloadReport(report, 'json')
      await state.downloadReport(report)
      assert.equal(state.reportDownloading.value, '')
    },
  })
  assert.deepEqual(requested, [['kb-a', 'report-1', 'json'], ['kb-a', 'report-1', 'markdown']])
  assert.deepEqual(links.map(link => link.download), ['Weekly_report.json', 'Weekly_report.md'])
  assert.ok(links.every(link => link.clicks === 1 && link.removed))
  assert.deepEqual(revoked, ['blob:report-0', 'blob:report-1'])
})

test('a pending report download blocks a second request and errors restore the action', async () => {
  let reject!: (error: Error) => void
  let requested = 0
  await renderWorkbench({
    workflows: { downloadKnowledgeWorkflowReport: () => {
      requested++
      return new Promise((_resolve, fail) => { reject = fail })
    } },
    scenario: async state => {
      const pending = state.downloadReport(report, 'json')
      await state.downloadReport(report, 'markdown')
      assert.equal(requested, 1)
      assert.equal(state.reportDownloading.value, 'json')
      reject(new Error('Report download unavailable'))
      await pending
      assert.equal(state.reportDownloading.value, '')
      assert.equal(state.loadError.value, 'Report download unavailable')
    },
  })
})


test('workflow editor saves selectable knowledge and retained workflow steps', async () => {
  const saved: Array<{ kbId: string; input: any }> = []
  const retainedTypes = ['search', 'fact_query', 'report']
  const { state } = await renderWorkbench({
    workflows: {
      listKnowledgeWorkflows: () => Promise.resolve({ success: true, data: [] }),
      createKnowledgeWorkflow: (kbId: string, input: object) => {
        saved.push({ kbId, input })
        return Promise.resolve({ success: true, data: { id: 'workflow-a' } })
      },
    },
    scenario: async state => {
      state.startNewWorkflow()
      state.workflowDraft.value.name = 'Knowledge workflow'
      const selectable = state.stepTypeOptions.value.map((option: { value: string }) => option.value)
      assert.deepEqual(selectable, retainedTypes)
      for (const type of retainedTypes) {
        assert.ok(selectable.includes(type), `${type} remains available in the step picker`)
        state.stepTypeToAdd.value = type
        state.addWorkflowStep()
      }
      await state.saveWorkflow()
    },
  })
  assert.equal(saved.length, 1)
  assert.equal(saved[0].kbId, 'kb-a')
  assert.deepEqual(saved[0].input.steps.map((step: { type: string }) => step.type), ['search', 'report', ...retainedTypes])
  assert.equal(state.workflowEditorError.value, '')
  assert.equal(state.workflowDraft.value, null)
})

for (const templateId of ['daily', 'weekly']) {
  test(`${templateId} template saves retained steps and displays a completed run`, async () => {
    let saved: any
    let started = 0
    let current: any
    const completed = { id: 'run-template', status: 'completed', steps: [] }
    const { html, state } = await renderWorkbench({
      workflows: {
        listKnowledgeWorkflows: () => Promise.resolve({ success: true, data: current ? [current] : [] }),
        createKnowledgeWorkflow: (_kbId: string, input: any) => {
          saved = structuredClone(input)
          current = { ...input, id: 'workflow-template' }
          return Promise.resolve({ success: true, data: current })
        },
        listKnowledgeWorkflowRuns: () => Promise.resolve({ success: true, data: started ? [completed] : [] }),
        runKnowledgeWorkflow: () => { started++; return Promise.resolve({ success: true, data: completed }) },
        getKnowledgeWorkflowRun: () => Promise.resolve({ success: true, data: completed }),
      },
      scenario: async state => {
        await state.selectSection('workflows')
        state.startNewWorkflow()
        state.applyTemplate(templateId)
        if (templateId === 'weekly') state.workflowDraft.value.steps[0].config.entity = 'Aurora'
        await state.saveWorkflow()
        assert.equal(state.workflowEditorError.value, '')
        await state.runWorkflow(current)
      },
    })
    assert.deepEqual(saved.steps.map((step: any) => step.type), [templateId === 'daily' ? 'search' : 'fact_query', 'report'])
    assert.deepEqual(saved.steps[1].depends_on, [saved.steps[0].id])
    assert.equal(saved.steps[1].config.window, templateId === 'daily' ? 'previous_day' : 'previous_week')
    assert.equal(saved.enabled, true)
    assert.equal(started, 1)
    assert.equal(state.selectedRun.value.status, 'completed')
    assert.match(html, /knowledgeWorkbench.workflows.runStatus.completed/)
  })
}

test('enabled workflows still allow resuming a paused run', async () => {
  const workflow = { id: 'workflow-paused', name: 'Paused workflow', enabled: true, steps: [], schedule: {} }
  let resumed = 0
  const { state } = await renderWorkbench({
    workflows: {
      resumeKnowledgeWorkflowRun: () => { resumed++; return Promise.resolve({ success: true, data: { id: 'run-paused', status: 'running', steps: [] } }) },
      listKnowledgeWorkflowRuns: () => Promise.resolve({ success: true, data: [{ id: 'run-paused', status: 'running', steps: [] }] }),
      getKnowledgeWorkflowRun: () => Promise.resolve({ success: true, data: { id: 'run-paused', status: 'running', steps: [] } }),
    },
    scenario: async state => {
      state.selectedWorkflow.value = workflow
      state.selectedRun.value = { id: 'run-paused', status: 'paused', steps: [] }
      assert.equal(state.canResumeRun.value, true)
      state.selectedWorkflow.value.enabled = false
      assert.equal(state.canResumeRun.value, false)
      state.selectedWorkflow.value.enabled = true
      await state.resumeRun()
      state.clearRunPolling()
    },
  })
  assert.equal(resumed, 1)
  assert.equal(state.selectedRun.value.status, 'running')
  assert.equal(state.canResumeRun.value, false)
})

test('backfill completion reloads cached evidence, events, coverage and the selected object profile', async () => {
  const calls = { evidence: 0, events: 0, profile: 0 }
  const { state } = await renderWorkbench({
    result: { template_only_message_count: 0 },
    focusApi: {
      listKnowledgeContextEvidence: () => { calls.evidence++; return Promise.resolve({ success: true, data: [{ ...evidence[0], extraction_mode: 'full' }] }) },
      listKnowledgeContextEvents: () => { calls.events++; return Promise.resolve({ success: true, data: [event] }) },
      getGlobalKnowledgeEntityContext: () => { calls.profile++; return Promise.resolve({ success: true, data: { entities: [entity], template_only_message_count: 0 } }) },
    },
    scenario: async state => {
      await state.selectEntity(entity)
      state.evidence.value = [{ ...evidence[0], extraction_mode: 'template_only' }]
      state.eventsTemplateOnlyCount.value = 3
      state.eventContexts.value = { old: {} }
      const before = { ...calls }
      await state.onBackfillChanged()
      assert.deepEqual(calls, { evidence: before.evidence + 1, events: before.events + 1, profile: before.profile + 1 })
      assert.equal(state.evidence.value[0].extraction_mode, 'full')
      assert.equal(state.events.value[0].id, event.id)
      assert.equal(state.eventsTemplateOnlyCount.value, 0)
      assert.equal(state.entityContext.value.template_only_message_count, 0)
      assert.deepEqual(state.eventContexts.value, {})
    },
  })
  assert.ok(state.entityContext.value)
  assert.equal((source.match(/@changed="onBackfillChanged"/g) || []).length, 4, 'all object and topic entry points subscribe')
})

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: disabled workflows cannot run and show the enable-first instruction`, async () => {
    let runs = 0
    const { html } = await renderWorkbench({ locale,
      workflows: { runKnowledgeWorkflow: () => { runs++; return Promise.reject(new Error('must not run')) } },
      scenario: async state => {
        const workflow = { id: 'disabled-workflow', name: 'Disabled workflow', enabled: false, schedule: {}, steps: [] }
        state.workflows.value = [workflow]
        state.activeSection.value = 'workflows'
        await state.runWorkflow(workflow)
      },
    })
    assert.equal(runs, 0)
    assert.match(html, /<div(?=[^>]*data-disabled="true")(?=[^>]*title="[^"]*")[^>]*>(?:\s|<!--.*?-->)*(?:立即运行|Run now)(?:\s|<!--.*?-->)*<\/div>/)
    assert.match(html, locale === 'zh-CN' ? /工作流已停用，请先启用后再运行/ : /This workflow is disabled\. Enable it before running/)
  })
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const count of [1, 2]) {
    test(`${locale}: a structured query uses the correct template-only count for ${count}`, async () => {
      const { html } = await renderWorkbench({
        locale, result: { template_only_message_count: count },
        scenario: async state => { await state.selectSection('query'); await state.runQuery() },
      })
      const expected = locale === 'zh-CN' ? `另有 ${count} 条相关消息仅抽取了模板字段。`
        : count === 1 ? 'Another relevant message has only template fields extracted.' : `Another ${count} relevant messages have only template fields extracted.`
      assert.ok(html.includes(expected), html)
    })
  }
  for (const count of [0, 1, 2]) {
    test(`${locale}: an entity list uses the correct alias noun for ${count}`, async () => {
      const { html } = await renderWorkbench({
        locale,
        scenario: async state => {
          await state.selectSection('entities')
          state.entities.value = [{ ...entity, aliases: Array.from({ length: count }, (_, index) => `Alias ${index}`) }]
        },
      })
      const expected = locale === 'zh-CN' ? `${count} 个别名` : `${count} ${count === 1 ? 'alias' : 'aliases'}`
      assert.ok(html.includes(`${expected}</small>`), html)
    })
  }
}

for (const count of [1, 2]) {
  test(`English event cards use the source quantity ${count}`, async () => {
    const { html } = await renderWorkbench({ locale: 'en-US', focusApi: { listKnowledgeContextEvents: () => Promise.resolve({ success: true, data: [{ ...event, source_count: count }] }) }, scenario: async state => { await state.selectSection('events') } })
    assert.ok(html.includes(`${count} ${count === 1 ? 'source' : 'sources'}<`), html)
  })
}
