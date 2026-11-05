<script setup lang="ts">
import { formatDateTime } from '@/utils/dateTime'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import cytoscape, { type Core, type StylesheetJson } from 'cytoscape'
import { searchGraph } from '@/api/graph'
import { Button, Checkbox, EmptyState, ErrorState, Select, Skeleton } from '@/components/ui'
import { graphElements, graphStyles, overviewStyles } from './entityGraphPresentation'
import { searchErrorCode } from '@/utils/search'
import type { GraphSearchResponse } from '@/types/graph'
import type { SearchEntity, SourceLocation } from '@/types/search'
import RelationTypeManager from './RelationTypeManager.vue'

// Without an entity the graph shows every relation in libraryId.
const props = withDefaults(defineProps<{ entity?: SearchEntity; libraryId?: string; active?: boolean; managed?: boolean; relationTypeOptions?: string[] }>(), { active: true, relationTypeOptions: () => [] })
const emit = defineEmits<{ 'open-source': [id: string, location?: SourceLocation]; 'open-entity': [id: string]; 'relation-types-changed': [] }>()
const { t, locale } = useI18n()
const scopeLibrary = computed(() => props.entity ? props.entity.library_id : props.libraryId || '')
const limitOptions = props.entity ? [20, 50, 100] : [100, 200, 500]
const hops = ref(2), limit = ref(limitOptions[1]!), relationTypes = ref<string[]>([])
const relationOptions = computed(() => [...new Set(props.relationTypeOptions)].sort())
const managingTypes = ref(false)
function toggleRelationType(value: string, checked: boolean) {
  relationTypes.value = checked ? [...relationTypes.value, value] : relationTypes.value.filter(item => item !== value)
}
const entityNotFound = ref(false)
const data = ref<GraphSearchResponse | null>(null), loading = ref(false), error = ref('')
const canvas = ref<HTMLElement | null>(null)
const selection = ref<{ type: 'node' | 'edge'; id: string } | null>(null)
const selectedEdge = computed(() => selection.value?.type === 'edge' ? data.value?.edges.find(edge => edge.id === selection.value?.id) : undefined)
const graphTruncated = computed(() => data.value?.truncated && (!data.value.truncations.length || data.value.truncations.some(item => item.reason !== 'document_limit')))
const documentsTruncated = computed(() => data.value?.truncations.some(item => item.reason === 'document_limit'))
let graph: Core | undefined, resizeObserver: ResizeObserver | undefined, themeObserver: MutationObserver | undefined
let sequence = 0, controller: AbortController | undefined
let fitZoom = 1, quiet = false

function entityName(id: string) { return data.value?.entities.find(entity => entity.id === id)?.canonical_name || id }
function entityType(type: string) {
  const labels: Record<string, string> = { company: t('workbench.entities.types.company'), person: t('workbench.entities.types.person'), product: t('workbench.entities.types.product'), project: t('workbench.entities.types.project'), organization: t('workbench.entities.types.organization'), other: t('workbench.entities.types.other') }
  return labels[type] || labels.other
}

function edgeLabel(edge: { subject_id: string; object_id: string; relation_type: string }) { return `${entityName(edge.subject_id)} — ${edge.relation_type} → ${entityName(edge.object_id)}` }
function select(type: 'node' | 'edge', id: string) {
  if (type === 'node') { if (id !== props.entity?.id) emit('open-entity', id); return }
  selection.value = { type, id }
}
function relationTypesChanged() { emit('relation-types-changed'); void runQuery() }
function stopQuery() { sequence++; controller?.abort(); controller = undefined; loading.value = false }
function clearGraph() { graph?.destroy(); graph = undefined; data.value = null; selection.value = null }

async function runQuery() {
  stopQuery()
  if (!props.active) return
  clearGraph(); error.value = ''; entityNotFound.value = false
  const libraries = scopeLibrary.value ? [scopeLibrary.value] : []
  if (!libraries.length) { error.value = t('entityGraph.noLibraries'); return }
  const current = sequence
  controller = new AbortController()
  loading.value = true
  try {
    const request = props.entity ? { entity: { id: props.entity.id }, mode: 'neighbors' as const, hops: hops.value } : { mode: 'all' as const }
    const response = await searchGraph({ libraries, ...request, relation_types: relationTypes.value, limit: limit.value }, controller.signal)
    if (current !== sequence) return
    if (!response.success) throw new Error(response.message || t('entityGraph.failed'))
    if (response.data.status !== 'ok') throw new Error(t('entityGraph.chooseEntity'))
    data.value = response.data
    loading.value = false
    selection.value = null
    await nextTick()
    if (current === sequence) drawGraph()
  } catch (cause: unknown) {
    if (current !== sequence) return
    const failure = cause as { status?: number; code?: string; message?: string }
    if (failure.status === 404 && searchErrorCode(cause) === 1003) entityNotFound.value = true
    else if (failure.code !== 'ERR_CANCELED') error.value = failure.status === 503 ? t('entityGraph.unavailable') : failure.message || t('entityGraph.failed')
  } finally { if (current === sequence) loading.value = false }
}
function graphStyle(): StylesheetJson {
  const style = getComputedStyle(canvas.value!)
  const color = (name: string) => style.getPropertyValue(name).trim()
  const colors = { text: color('--text'), bg: color('--bg'), muted: color('--muted'), border: color('--border'), select: color('--select'), onSelect: color('--on-select'), font: color('--font'), accent: color('--accent') }
  return props.entity ? graphStyles(colors) : overviewStyles(colors)
}
function drawGraph() {
  if (!canvas.value || !data.value || !props.active) return
  graph?.destroy()
  quiet = false
  graph = cytoscape({
    container: canvas.value,
    elements: graphElements(data.value, props.entity?.id || ''),
    style: graphStyle(),
    layout: props.entity
      ? { name: 'cose', animate: false, padding: 45, nodeRepulsion: () => 24000, idealEdgeLength: () => 150, nodeOverlap: 20 }
      : { name: 'cose', animate: false, padding: 30, nodeDimensionsIncludeLabels: true, nodeRepulsion: () => 35000, idealEdgeLength: () => 70, nodeOverlap: 40, componentSpacing: 60, gravity: 0.5 },
    minZoom: 0.2, maxZoom: 3, boxSelectionEnabled: false, autounselectify: true,
  })
  if (!props.entity) {
    // Hovering a node lights up its neighbourhood and fades the rest of the graph.
    graph.on('mouseover', 'node', event => { graph!.elements().addClass('faded'); event.target.closedNeighborhood().removeClass('faded').addClass('highlight') })
    graph.on('mouseout', 'node', () => graph!.elements().removeClass('faded highlight'))
    graph.on('zoom', updateQuiet)
  }
  graph.on('tap', 'node', event => select('node', event.target.data('rawID')))
  graph.on('tap', 'edge', event => select('edge', event.target.data('rawID')))
  resizeObserver?.disconnect()
  resizeObserver?.observe(canvas.value)
  fitGraph()
  updateSelection()
}
function updateSelection() {
  if (!graph || !data.value) return
  graph.elements().forEach(element => {
    const id = element.data('rawID')
    element.toggleClass('selected', !element.isNode() && selection.value?.id === id)
  })
}

function zoomBy(factor: number) {
  if (!graph) return
  graph.zoom({ level: Math.min(graph.maxZoom(), Math.max(graph.minZoom(), graph.zoom() * factor)), renderedPosition: { x: graph.width() / 2, y: graph.height() / 2 } })
}
function fitGraph() { if (graph) { graph.fit(undefined, 45); if (graph.zoom() > 1) graph.zoom(1); graph.center(); fitZoom = graph.zoom(); updateQuiet() } }
// Overview names drop out once zoomed out past the fitted view or too small to read, as in Obsidian.
function updateQuiet() {
  if (!graph || props.entity) return
  const next = graph.zoom() < Math.max(0.35, fitZoom * 0.8)
  if (next !== quiet) { quiet = next; graph.nodes().toggleClass('quiet', quiet) }
}
function graphKeydown(event: KeyboardEvent) {
  if (!graph) return
  const moves: Record<string, { x: number; y: number }> = { ArrowLeft: { x: 40, y: 0 }, ArrowRight: { x: -40, y: 0 }, ArrowUp: { x: 0, y: 40 }, ArrowDown: { x: 0, y: -40 } }
  if (moves[event.key]) { event.preventDefault(); graph.panBy(moves[event.key]!) }
  else if (event.key === '+' || event.key === '=') { event.preventDefault(); zoomBy(1.2) }
  else if (event.key === '-') { event.preventDefault(); zoomBy(1 / 1.2) }
  else if (event.key === 'Home') { event.preventDefault(); fitGraph() }
}
watch(selection, updateSelection)
// Drop filters for relation types that were renamed, disabled or merged away.
watch(relationOptions, options => { const next = relationTypes.value.filter(type => options.includes(type)); if (next.length !== relationTypes.value.length) relationTypes.value = next })
watch([() => props.entity?.id, scopeLibrary, () => props.active, hops, limit, relationTypes], () => { void runQuery() }, { immediate: true })
watch(locale, () => { if (graph) graph.style(graphStyle()) })
onMounted(() => {
  resizeObserver = new ResizeObserver(() => { if (props.active && graph) { graph.resize(); fitGraph() } })
  if (canvas.value) resizeObserver.observe(canvas.value)
  themeObserver = new MutationObserver(() => { if (graph) graph.style(graphStyle()) })
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['theme-mode', 'style'] })
})
onBeforeUnmount(() => { stopQuery(); resizeObserver?.disconnect(); themeObserver?.disconnect(); graph?.destroy() })
</script>

<template>
  <section class="entity-graph" :class="{ 'is-overview': !entity }" :aria-label="t('entityGraph.graph')" :aria-busy="loading">
    <div class="graph-bar">
      <div v-if="entity" class="graph-hops" role="group" :aria-label="t('entityGraph.hops')">
        <Button v-for="count in [1, 2, 3]" :key="count" :aria-pressed="hops === count" @click="hops = count">{{ t('entityGraph.hopCount', { count }, count) }}</Button>
      </div>
      <details v-if="relationOptions.length" class="graph-type-filter">
        <summary>{{ t('entityGraph.relationTypes') }}<span v-if="relationTypes.length"> ({{ relationTypes.length }})</span></summary>
        <div class="graph-type-options">
          <label v-for="option in relationOptions" :key="option"><Checkbox :model-value="relationTypes.includes(option)" :aria-label="option" @update:model-value="toggleRelationType(option, $event)" />{{ option }}</label>
          <Button v-if="relationTypes.length" variant="ghost" @click="relationTypes = []">{{ t('common.clear') }}</Button>
        </div>
      </details>
      <label class="graph-limit">{{ t('entityGraph.limit') }}<Select :model-value="limit" :aria-label="t('entityGraph.limit')" @update:model-value="limit = Number($event)"><option v-for="count in limitOptions" :key="count" :value="count">{{ count }}</option></Select></label>
      <Button v-if="!managed" :disabled="!scopeLibrary" @click="managingTypes = true">{{ t('ikb18.manageRelationTypes') }}</Button>
      <span v-if="data" class="graph-hint graph-count" role="status">{{ t('entityGraph.count', { nodes: data.entities.length, edges: data.edges.length }) }}</span>
    </div>
    <RelationTypeManager v-if="managingTypes && active" :initial-library-id="scopeLibrary" @close="managingTypes = false" @changed="relationTypesChanged" />
    <ErrorState v-if="error" :title="error"><Button @click="runQuery">{{ t('common.retry') }}</Button></ErrorState>
    <Skeleton v-else-if="loading" :label="t('common.loading')" />
    <EmptyState v-else-if="entityNotFound" :title="t('entityPage.notFound')" :description="t('entityPage.graphNotFoundHint')" />
    <template v-else-if="data">
      <div v-if="data.pending_sync.global || graphTruncated || documentsTruncated" class="graph-notices" role="status">
        <p v-if="data.pending_sync.global">{{ t('entityGraph.pending', { global: data.pending_sync.global, involved: data.pending_sync.involved }) }}</p>
        <p v-if="data.pending_sync.global">{{ t('entityPage.syncHint') }}</p>
        <p v-if="graphTruncated">{{ entity ? t('entityGraph.truncated') : t('entityGraph.overviewTruncated', { limit }) }}</p>
        <p v-if="documentsTruncated">{{ t('entityGraph.documentsTruncated') }}</p>
      </div>
      <EmptyState v-if="!data.edges.length" :title="t('entityGraph.empty')" />
      <template v-else>
        <div class="graph-surface">
          <div ref="canvas" class="graph-canvas" role="group" tabindex="0" :aria-label="t('entityGraph.canvasLabel')" @keydown="graphKeydown" />
          <div class="graph-tools">
            <span class="graph-hint">{{ t('entityPage.graphHint') }}</span>
            <div class="graph-zoom"><Button :aria-label="t('entityGraph.zoomOut')" @click="zoomBy(1 / 1.2)">−</Button><Button :aria-label="t('entityGraph.zoomIn')" @click="zoomBy(1.2)">+</Button><Button @click="fitGraph">{{ t('entityGraph.fit') }}</Button></div>
          </div>
        </div>
        <details class="graph-accessible">
          <summary>{{ t('entityGraph.keyboardBrowse') }}</summary>
          <div class="graph-node-buttons"><button v-for="node in data.entities" :key="node.id" type="button" :aria-current="node.id === entity?.id ? 'true' : undefined" @click="select('node', node.id)">{{ node.canonical_name }} · {{ entityType(node.entity_type) }}</button></div>
          <div class="graph-edge-buttons"><button v-for="edge in data.edges" :key="edge.id" type="button" :aria-pressed="selectedEdge?.id === edge.id" @click="select('edge', edge.id)">{{ edgeLabel(edge) }}</button></div>
        </details>
        <section v-if="selectedEdge" class="graph-detail" aria-live="polite">
          <h3>{{ entityName(selectedEdge.subject_id) }} <span class="graph-verb">{{ selectedEdge.relation_type }}</span> {{ entityName(selectedEdge.object_id) }}</h3>
          <p>{{ t('ikb18.sources', { count: selectedEdge.source_count }) }}</p>
          <article v-for="source in selectedEdge.sources" :key="source.entry_id" class="graph-source">
            <button class="entity-link" type="button" @click="emit('open-source', source.entry_id, source.evidence?.[0]?.locations?.[0])">{{ source.title || source.entry_id }}</button>
            <p>{{ source.library_name }} · {{ source.published_at ? formatDateTime(source.published_at, locale) : t('workbench.unknownDate') }}</p>
            <p v-if="source.source_unverified" class="graph-source-warning">{{ t('entityGraph.sourceUnverified') }}</p>
            <div v-for="(evidence, index) in source.evidence || []" :key="index">
              <p v-if="evidence.speaker">{{ t('ikb18.speaker') }}: <button v-if="evidence.speaker.entity_id" class="entity-link" type="button" @click="emit('open-entity', evidence.speaker.entity_id)">{{ evidence.speaker.name }}</button><span v-else>{{ evidence.speaker.name }}</span></p>
              <button v-for="(location, position) in evidence.locations || []" :key="position" class="entity-link" type="button" @click="emit('open-source', source.entry_id, location)">{{ location.heading_path?.join(' / ') }} · {{ t('ikb18.paragraph', { number: location.paragraph }) }}</button>
            </div>
          </article>
        </section>
      </template>
    </template>
  </section>
</template>

<style scoped>
.entity-graph { color: var(--text); min-width: 0; }
.graph-bar, .graph-tools { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3); padding-block: var(--space-3); }
.graph-bar { justify-content: flex-start; }
.graph-count { margin-left: auto; }
.graph-limit { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-sm); }
.graph-limit :deep(select) { width: 80px; }
.graph-type-filter { position: relative; font-size: var(--text-sm); }
.graph-type-filter summary { cursor: pointer; min-height: var(--control); display: flex; align-items: center; gap: var(--space-1); border: 1px solid var(--border); padding-inline: var(--space-3); }
.graph-type-options { position: absolute; z-index: 1; inset-inline-start: 0; top: 100%; display: grid; gap: var(--space-2); min-width: 160px; max-width: min(320px, 80vw); max-height: 240px; overflow: auto; padding: var(--space-3); border: 1px solid var(--rule); background: var(--surface); box-shadow: var(--shadow-float); }
.graph-type-options label { display: flex; align-items: center; gap: var(--space-2); overflow-wrap: anywhere; cursor: pointer; }
.graph-hops, .graph-zoom { display: flex; gap: var(--space-1); }
.graph-hops :deep([aria-pressed="true"]) { background: var(--text); color: var(--bg); border-color: var(--text); }
.graph-hint, .graph-source p { color: var(--muted); font-size: var(--text-xs); }
.graph-notices { padding: var(--space-3) var(--space-4); border-top: 2px solid var(--rule); background: var(--surface-soft); }
.graph-notices p { margin: 0; }
.graph-surface { min-width: 0; border: 1px solid var(--rule); background: radial-gradient(var(--border) 1px, transparent 1px) 0 0 / 20px 20px; }
.graph-canvas { width: 100%; height: 440px; font-family: var(--font); }
.graph-tools { padding: var(--space-3); border-top: 1px solid var(--border); }
.graph-detail { padding: var(--space-3) var(--space-4); border-top: 2px solid var(--rule); background: var(--surface-soft); overflow-wrap: anywhere; }
.graph-detail h3 { font-size: var(--type-subsection-size); font-weight: var(--type-subsection-weight); }
.graph-verb { background: var(--select); color: var(--on-select); padding: 0 0.3em; }
.graph-source { padding-block: var(--space-2); border-top: 1px solid var(--border); }
.entity-link { padding: 0; border: 0; background: none; color: inherit; font: inherit; text-align: left; text-decoration: underline; cursor: pointer; overflow-wrap: anywhere; }
.entity-link:hover { color: var(--accent); }
.graph-node-buttons, .graph-edge-buttons { display: flex; flex-wrap: wrap; gap: var(--space-2); padding-block: var(--space-2); }
.graph-accessible summary { cursor: pointer; margin-block: var(--space-3); }
.graph-accessible button { background: var(--bg); color: var(--text); border: 1px solid var(--border); font: inherit; padding: var(--space-2); cursor: pointer; overflow-wrap: anywhere; }
.graph-edge-buttons button[aria-pressed="true"] { background: var(--select); color: var(--on-select); }
.graph-source-warning { color: var(--danger); }
button:focus-visible, summary:focus-visible, .graph-canvas:focus-visible { outline: 2px solid var(--text); outline-offset: 3px; }
.is-overview .graph-canvas { height: min(70vh, 720px); }
@media (max-width: 640px) { .graph-canvas { height: 320px; } }
</style>
