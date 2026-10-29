<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import cytoscape, { type Core, type StylesheetJson } from 'cytoscape'
import { searchGraph } from '@/api/graph'
import { ENTITY_TYPES } from '@/api/entity-types'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'
import type { GraphSearchResponse } from '@/types/graph'
import type { SearchEntity, SourceLocation } from '@/types/search'
import LibraryScopeSelect from '@/components/LibraryScopeSelect.vue'
import { useLibraryScope } from '@/composables/useLibraryScope'
import { readLibraryScope, scopedLibraryIDs } from '@/utils/libraryScope'
import RelationTypeManager from './RelationTypeManager.vue'

const props = withDefaults(defineProps<{ entity: SearchEntity; active?: boolean }>(), { active: true })
const emit = defineEmits<{ 'open-source': [id: string, location?: SourceLocation]; 'open-entity': [id: string]; 'open-profile': [id: string] }>()
const { t, locale } = useI18n()
const scope = useLibraryScope(), catalog = ref<LibraryCatalogItem[]>([])
const relationTypes = ref<string[]>([]), hops = ref(1), limit = ref(50)
const data = ref<GraphSearchResponse | null>(null), loading = ref(false), error = ref('')
const canvas = ref<HTMLElement | null>(null)
const managingTypes = ref(false)
const selection = ref<{ type: 'node' | 'edge'; id: string } | null>(null)
const relationOptions = computed(() => [...new Set(catalog.value
  .filter(library => !scope.value.length || scope.value.includes(library.id))
  .flatMap(library => (library.relation_types || []).filter(type => type.enabled).map(type => type.name)))].sort().map(value => ({ value, label: value })))
const hopOptions = computed(() => [1, 2, 3].map(value => ({ value, label: t('entityGraph.hopCount', { count: value }, value) })))
const limitOptions = [20, 50, 100].map(value => ({ value, label: String(value) }))
const selectedNode = computed(() => selection.value?.type === 'node' ? data.value?.entities.find(entity => entity.id === selection.value?.id) : undefined)
const selectedEdge = computed(() => selection.value?.type === 'edge' ? data.value?.edges.find(edge => edge.id === selection.value?.id) : undefined)
const relatedEdges = computed(() => data.value?.edges.filter(edge => edge.subject_id === selectedNode.value?.id || edge.object_id === selectedNode.value?.id) || [])
const graphTruncated = computed(() => data.value?.truncated && (!data.value.truncations.length || data.value.truncations.some(item => item.reason !== 'document_limit')))
const documentsTruncated = computed(() => data.value?.truncations.some(item => item.reason === 'document_limit'))
let graph: Core | undefined, resizeObserver: ResizeObserver | undefined, themeObserver: MutationObserver | undefined
let sequence = 0, controller: AbortController | undefined, catalogReady = false

function entityName(id: string) { return data.value?.entities.find(entity => entity.id === id)?.canonical_name || id }
function entityType(type: string) {
  const labels: Record<string, string> = { company: t('knowledgeWorkbench.entities.types.company'), person: t('knowledgeWorkbench.entities.types.person'), product: t('knowledgeWorkbench.entities.types.product'), project: t('knowledgeWorkbench.entities.types.project'), organization: t('knowledgeWorkbench.entities.types.organization'), other: t('knowledgeWorkbench.entities.types.other') }
  return labels[type] || labels.other
}
function libraryName(id: string) { const library = catalog.value.find(library => library.id === id); return library?.display_name || library?.name || id }
function edgeLabel(edge: { subject_id: string; object_id: string; relation_type: string }) { return `${entityName(edge.subject_id)} — ${edge.relation_type} → ${entityName(edge.object_id)}` }
function select(type: 'node' | 'edge', id: string) { selection.value = { type, id } }
function stopQuery() { sequence++; controller?.abort(); controller = undefined; loading.value = false }
function clearGraph() { graph?.destroy(); graph = undefined; data.value = null; selection.value = null }

async function runQuery() {
  stopQuery()
  if (!props.active || !catalogReady) return
  clearGraph(); error.value = ''
  const libraries = scopedLibraryIDs(scope.value, catalog.value)
  if (!libraries.length) { error.value = t('entityGraph.noLibraries'); return }
  const current = sequence
  controller = new AbortController()
  loading.value = true
  try {
    const response = await searchGraph({ libraries, entity: { id: props.entity.id }, mode: 'neighbors', hops: hops.value, relation_types: relationTypes.value, limit: limit.value }, controller.signal)
    if (current !== sequence) return
    if (!response.success) throw new Error(response.message || t('entityGraph.failed'))
    if (response.data.status !== 'ok') throw new Error(t('entityGraph.chooseEntity'))
    data.value = response.data
    selection.value = { type: 'node', id: props.entity.id }
    await nextTick()
    if (current === sequence) drawGraph()
  } catch (cause: unknown) {
    if (current !== sequence) return
    const failure = cause as { status?: number; code?: string; message?: string }
    if (failure.code !== 'ERR_CANCELED') error.value = failure.status === 503 ? t('entityGraph.unavailable') : failure.message || t('entityGraph.failed')
  } finally { if (current === sequence) loading.value = false }
}
function loadedLibraries(libraries: LibraryCatalogItem[]) {
  catalog.value = libraries; catalogReady = true
  void runQuery()
}
async function refreshRelationTypes() {
  const current = sequence
  try {
    const response = await listLibraries()
    if (current !== sequence) return
    if (!response.success) throw new Error(response.message || t('entityGraph.failed'))
    catalog.value = response.data || []
    const available = new Set(relationOptions.value.map(option => option.value))
    const next = relationTypes.value.filter(type => available.has(type))
    if (next.length !== relationTypes.value.length) relationTypes.value = next
    else await runQuery()
  } catch (cause) { if (current === sequence) error.value = (cause as { message?: string })?.message || t('entityGraph.failed') }
}
function graphStyle(): StylesheetJson {
  const style = getComputedStyle(canvas.value!)
  const color = (name: string) => style.getPropertyValue(name).trim()
  const brand = color('--td-brand-color'), foreground = color('--td-text-color-primary'), background = color('--td-bg-color-container')
  const stroke = color('--td-component-border'), secondary = color('--td-text-color-secondary')
  const colors = [brand, color('--td-warning-color'), color('--td-success-color'), secondary, brand, secondary]
  return [
    { selector: 'node', style: { width: 44, height: 44, shape: 'ellipse', label: 'data(label)', 'background-color': background, 'background-image': element => `data:image/svg+xml;utf8,${encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="44" height="44"><text x="22" y="28" text-anchor="middle" font-size="18" fill="${foreground}">&#${String(element.data('label')).codePointAt(0)};</text></svg>`)}`, 'background-width': 44, 'background-height': 44, 'border-width': 1, 'border-color': stroke, color: foreground, 'font-family': style.fontFamily, 'font-size': 12, 'text-wrap': 'wrap', 'text-max-width': '120px', 'text-valign': 'bottom', 'text-margin-y': 9, 'overlay-opacity': 0 } },
    ...ENTITY_TYPES.map((type, index) => ({ selector: `node[entityType = "${type}"]`, style: { 'background-color': colors[index], 'background-opacity': 0.15 } })),
    { selector: 'node.center', style: { width: 56, height: 56, 'border-width': 2, 'border-color': brand, 'font-weight': 500 } },
    { selector: 'edge', style: { width: 1.5, 'curve-style': 'bezier', 'control-point-step-size': 34, 'target-arrow-shape': 'triangle', 'arrow-scale': 0.9, 'line-color': stroke, 'target-arrow-color': stroke, label: 'data(label)', color: secondary, 'font-family': style.fontFamily, 'font-size': 12, 'text-background-color': background, 'text-background-opacity': 1, 'text-background-padding': '3px', 'text-wrap': 'wrap', 'text-max-width': '110px', 'overlay-opacity': 0 } },
    { selector: '.faded', style: { opacity: 0.55 } },
    { selector: 'edge.related, edge.selected', style: { width: 2, 'line-color': brand, 'target-arrow-color': brand, color: brand } },
    { selector: 'node.selected', style: { 'border-width': 3, 'border-color': brand } },
  ]
}
function drawGraph() {
  if (!canvas.value || !data.value || !props.active) return
  graph?.destroy()
  const neighbors = data.value.entities.filter(entity => entity.id !== props.entity.id)
  const positions = Object.fromEntries(neighbors.map((entity, index) => { const angle = -Math.PI / 2 + index * Math.PI * 2 / neighbors.length; return [entity.id, { x: Math.cos(angle) * 200, y: Math.sin(angle) * 170 }] }))
  graph = cytoscape({
    container: canvas.value,
    elements: [
      ...data.value.entities.map(entity => ({ data: { id: `entity:${entity.id}`, rawID: entity.id, label: entity.canonical_name, entityType: entity.entity_type }, position: positions[entity.id] || { x: 0, y: 0 }, classes: entity.id === props.entity.id ? 'center' : '' })),
      ...data.value.edges.map(edge => ({ data: { id: `relation:${edge.id}`, rawID: edge.id, source: `entity:${edge.subject_id}`, target: `entity:${edge.object_id}`, label: edge.relation_type } })),
    ],
    style: graphStyle(),
    layout: { name: 'cose', animate: false, padding: 45, nodeRepulsion: () => 24000, idealEdgeLength: () => 150, nodeOverlap: 20 },
    minZoom: 0.2, maxZoom: 3, boxSelectionEnabled: false, autounselectify: true,
  })
  graph.on('tap', 'node', event => select('node', event.target.data('rawID')))
  graph.on('tap', 'edge', event => select('edge', event.target.data('rawID')))
  fitGraph()
  updateSelection()
}
function updateSelection() {
  if (!graph || !data.value) return
  const chosen = selection.value
  const related = new Set(chosen?.type === 'edge' ? [chosen.id] : relatedEdges.value.map(edge => edge.id))
  const nodes = new Set(chosen?.type === 'node' ? [chosen.id] : [])
  data.value.edges.filter(edge => related.has(edge.id)).forEach(edge => { nodes.add(edge.subject_id); nodes.add(edge.object_id) })
  graph.elements().forEach(element => {
    const id = element.data('rawID'), isNode = element.isNode()
    element.toggleClass('selected', !!chosen && chosen.id === id && (chosen.type === 'node') === isNode)
    element.toggleClass('related', !isNode && related.has(id))
    element.toggleClass('faded', !!chosen && !(isNode ? nodes.has(id) : related.has(id)))
  })
}
function zoomBy(factor: number) {
  if (!graph) return
  graph.zoom({ level: Math.min(graph.maxZoom(), Math.max(graph.minZoom(), graph.zoom() * factor)), renderedPosition: { x: graph.width() / 2, y: graph.height() / 2 } })
}
function fitGraph() { if (graph) { graph.fit(undefined, 45); if (graph.zoom() > 1) graph.zoom(1); graph.center() } }
function graphKeydown(event: KeyboardEvent) {
  if (!graph) return
  const moves: Record<string, { x: number; y: number }> = { ArrowLeft: { x: 40, y: 0 }, ArrowRight: { x: -40, y: 0 }, ArrowUp: { x: 0, y: 40 }, ArrowDown: { x: 0, y: -40 } }
  if (moves[event.key]) { event.preventDefault(); graph.panBy(moves[event.key]!) }
  else if (event.key === '+' || event.key === '=') { event.preventDefault(); zoomBy(1.2) }
  else if (event.key === '-') { event.preventDefault(); zoomBy(1 / 1.2) }
  else if (event.key === 'Home') { event.preventDefault(); fitGraph() }
}
watch(selection, updateSelection)
watch([() => props.entity.id, () => props.active], ([, active], [, wasActive]) => {
  stopQuery(); clearGraph()
  if (!active) { catalogReady = false; return }
  const savedScope = readLibraryScope()
  if (!wasActive && JSON.stringify(savedScope) !== JSON.stringify(scope.value)) scope.value = savedScope
  else void runQuery()
})
watch([scope, relationTypes, hops, limit], () => { void runQuery() }, { deep: true })
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
  <section class="entity-graph" :aria-label="t('entityGraph.graph')">
    <form v-if="active" class="graph-filters" novalidate @submit.prevent="runQuery">
      <label class="graph-library"><span>{{ t('entityGraph.libraries') }}</span><LibraryScopeSelect v-model="scope" @loaded="loadedLibraries" /></label>
      <label><span>{{ t('entityGraph.relationTypes') }}</span><t-select v-model="relationTypes" multiple clearable filterable :options="relationOptions" :placeholder="t('entityGraph.allRelations')" :aria-label="t('entityGraph.relationTypes')" /></label>
      <label><span>{{ t('entityGraph.hops') }}</span><t-select v-model="hops" :options="hopOptions" :aria-label="t('entityGraph.hops')" /></label>
      <label><span>{{ t('entityGraph.limit') }}</span><t-select v-model="limit" :options="limitOptions" :aria-label="t('entityGraph.limit')" /></label>
      <t-button type="submit" :loading="loading" :disabled="loading || !catalogReady">{{ t('entityGraph.query') }}</t-button>
      <t-button variant="outline" :disabled="!catalog.length" @click="managingTypes = true">{{ t('ikb18.manageRelationTypes') }}</t-button>
    </form>
    <RelationTypeManager v-if="managingTypes && active" :initial-library-id="scope[0] || catalog[0]?.id" @close="managingTypes = false" @changed="refreshRelationTypes" />
    <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button variant="text" :loading="loading" @click="runQuery">{{ t('common.retry') }}</t-button></template></t-alert>
    <div class="graph-notices" aria-live="polite">
      <p v-if="graphTruncated">{{ t('entityGraph.truncated') }}</p>
      <p v-if="documentsTruncated">{{ t('entityGraph.documentsTruncated') }}</p>
      <p v-if="data?.pending_sync.global">{{ t('entityGraph.pending', { global: data.pending_sync.global, involved: data.pending_sync.involved }) }}</p>
    </div>
    <div class="graph-heading"><span>{{ t('entityGraph.center', { name: entity.canonical_name }) }}</span><span v-if="data" role="status">{{ t('entityGraph.count', { nodes: data.entities.length, edges: data.edges.length }) }}</span></div>
    <div class="graph-workspace" :aria-busy="loading">
      <div class="graph-surface">
        <t-loading :loading="loading" show-overlay>
          <div ref="canvas" class="graph-canvas" role="group" tabindex="0" :aria-label="t('entityGraph.canvasLabel')" @keydown="graphKeydown" />
        </t-loading>
        <p v-if="data && !data.edges.length" class="graph-empty" role="status">{{ t('entityGraph.empty') }}</p>
        <div class="graph-tools">
          <span class="graph-hint">{{ t('entityGraph.interactionHint') }}</span>
          <div class="graph-zoom"><t-button variant="outline" size="small" :disabled="!data || loading" :aria-label="t('entityGraph.zoomOut')" @click="zoomBy(1 / 1.2)"><t-icon name="minus" /></t-button><t-button variant="outline" size="small" :disabled="!data || loading" :aria-label="t('entityGraph.zoomIn')" @click="zoomBy(1.2)"><t-icon name="add" /></t-button><t-button variant="outline" size="small" :disabled="!data || loading" @click="fitGraph">{{ t('entityGraph.fit') }}</t-button></div>
        </div>
        <details v-if="data" class="graph-accessible"><summary>{{ t('entityGraph.keyboardBrowse') }}</summary><div class="graph-node-buttons"><button v-for="node in data.entities" :key="node.id" type="button" :aria-pressed="selection?.type === 'node' && selection.id === node.id" @click="select('node', node.id)">{{ node.canonical_name }} · {{ entityType(node.entity_type) }}</button></div><div class="graph-edge-buttons"><button v-for="edge in data.edges" :key="edge.id" type="button" :aria-pressed="selection?.type === 'edge' && selection.id === edge.id" @click="select('edge', edge.id)">{{ edgeLabel(edge) }} · {{ libraryName(edge.library_id) }}</button></div></details>
      </div>
      <aside class="graph-detail" aria-live="polite">
        <template v-if="selectedNode">
          <span class="graph-eyebrow">{{ t('entityGraph.nodeDetails') }}</span><h4>{{ selectedNode.canonical_name }}</h4><p>{{ entityType(selectedNode.entity_type) }}</p>
          <div class="graph-node-actions"><t-button v-if="selectedNode.id !== entity.id" size="small" :disabled="loading" @click="emit('open-entity', selectedNode.id)">{{ t('entityGraph.recenter') }}</t-button><t-button size="small" variant="text" @click="emit('open-profile', selectedNode.id)">{{ t('entityGraph.openProfile') }}</t-button></div>
          <h5>{{ t('entityGraph.related', { count: relatedEdges.length }) }}</h5><div class="graph-edge-buttons"><button v-for="edge in relatedEdges" :key="edge.id" type="button" @click="select('edge', edge.id)">{{ edgeLabel(edge) }}<small>{{ libraryName(edge.library_id) }}</small></button></div>
        </template>
        <template v-else-if="selectedEdge">
          <span class="graph-eyebrow">{{ t('entityGraph.edgeDetails') }}</span><h4>{{ edgeLabel(selectedEdge) }}</h4><p>{{ libraryName(selectedEdge.library_id) }} · {{ t('ikb18.sources', { count: selectedEdge.source_count }) }}</p>
          <h5>{{ t('ikb18.relationSources') }}</h5><article v-for="source in selectedEdge.sources" :key="source.entry_id" class="graph-source">
            <t-button variant="text" @click="emit('open-source', source.entry_id, source.evidence?.[0]?.locations?.[0])">{{ source.title || source.entry_id }}</t-button>
            <p>{{ source.library_name }} · {{ source.published_at ? new Date(source.published_at).toLocaleDateString(locale) : t('knowledgeWorkbench.unknownDate') }}</p>
            <p v-if="source.source_unverified" class="graph-source-warning">{{ t('entityGraph.sourceUnverified') }}</p>
            <div v-for="(evidence, index) in source.evidence" :key="index">
              <p v-if="evidence.speaker">{{ t('ikb18.speaker') }}：<t-button v-if="evidence.speaker.entity_id" variant="text" @click="emit('open-entity', evidence.speaker.entity_id)">{{ evidence.speaker.name }}</t-button><span v-else>{{ evidence.speaker.name }}</span></p>
              <t-button v-for="(location, index) in evidence.locations" :key="index" variant="text" @click="emit('open-source', source.entry_id, location)">{{ location.heading_path?.join(' / ') }} · {{ t('ikb18.paragraph', { number: location.paragraph }) }}</t-button>
            </div>
          </article>
          <h5>{{ t('entityGraph.endpoints') }}</h5><div class="graph-node-buttons"><button v-for="id in [selectedEdge.subject_id, selectedEdge.object_id]" :key="id" type="button" @click="select('node', id)">{{ entityName(id) }}</button></div>
        </template>
        <p v-else class="graph-hint">{{ t('entityGraph.choose') }}</p>
      </aside>
    </div>
    <p class="graph-footnote">{{ t('entityGraph.localOnly') }}</p>
  </section>
</template>

<style scoped>
.entity-graph { container: entity-graph / inline-size; margin-block: 20px; color: var(--td-text-color-primary); }
.graph-filters { display: flex; align-items: flex-end; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
.graph-filters label { display: flex; flex: 1 1 100px; min-width: 0; flex-direction: column; gap: 6px; color: var(--td-text-color-secondary); font-size: 12px; }
.graph-filters .graph-library { flex-basis: 180px; }
.graph-filters :deep(.t-select__wrap) { width: 100%; min-width: 0; }
.graph-heading, .graph-tools { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; padding: 12px 16px; font-size: 12px; color: var(--td-text-color-secondary); }
.graph-workspace { display: grid; grid-template-columns: minmax(0, 1fr) 240px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); }
.graph-surface { position: relative; min-width: 0; background: var(--td-bg-color-container); border-radius: inherit; }
.graph-canvas { width: 100%; height: 440px; font-family: var(--app-font-family); }
.graph-canvas:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: -3px; }
.graph-empty { position: absolute; top: 345px; inset-inline: 16px; text-align: center; color: var(--td-text-color-secondary); pointer-events: none; font-size: 13px; }
.graph-hint, .graph-footnote { color: var(--td-text-color-secondary); font-size: 12px; line-height: 1.7; }
.graph-zoom, .graph-node-actions { display: flex; flex-wrap: wrap; gap: 6px; }
.graph-detail { box-sizing: border-box; border-left: 1px solid var(--td-component-stroke); min-width: 0; max-height: 560px; padding: 20px 16px; overflow: auto; scrollbar-gutter: stable; overflow-wrap: anywhere; }
.graph-detail h4 { font-size: 15px; margin: 12px 0; line-height: 1.7; }
.graph-detail h5 { font-size: 13px; margin: 24px 0 12px; }
.graph-detail p { font-size: 12px; color: var(--td-text-color-secondary); line-height: 1.7; }
.graph-eyebrow { font-size: 12px; color: var(--td-text-color-secondary); }
.graph-source { padding-block: 12px; border-top: 1px solid var(--td-component-stroke); }
.graph-source p { margin: 6px 0; }
.graph-source .graph-source-warning, .graph-notices { color: var(--td-warning-color); font-size: 12px; line-height: 1.7; }
.graph-notices p { margin: 8px 0; }
.graph-source :deep(.t-button) { height: auto; max-width: 100%; white-space: normal; text-align: left; }
.graph-source :deep(.t-button__text) { white-space: normal; overflow-wrap: anywhere; }
.graph-accessible { margin: 4px 16px 16px; font-size: 12px; }
.graph-accessible[open] { max-height: 280px; overflow: auto; scrollbar-gutter: stable; }
.graph-accessible summary { color: var(--td-brand-color); cursor: pointer; padding-block: 8px; }
.graph-node-buttons { display: flex; flex-wrap: wrap; gap: 8px; }
.graph-edge-buttons { display: flex; flex-direction: column; gap: 8px; margin-block: 8px; }
.graph-node-buttons button, .graph-edge-buttons button { padding: 8px; border: 1px solid var(--td-component-border); border-radius: var(--td-radius-small); background: var(--td-bg-color-container); color: var(--td-text-color-primary); text-align: left; cursor: pointer; font: inherit; font-size: 12px; line-height: 1.7; overflow-wrap: anywhere; min-width: 0; max-width: 100%; }
.graph-node-buttons button:hover, .graph-edge-buttons button:hover { background: var(--td-bg-color-container-hover); }
.graph-node-buttons button[aria-pressed="true"], .graph-edge-buttons button[aria-pressed="true"] { border-color: var(--td-brand-color); background: var(--td-brand-color-light); }
.graph-node-buttons button:focus-visible, .graph-edge-buttons button:focus-visible, summary:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.graph-edge-buttons small { display: block; color: var(--td-text-color-secondary); font-size: 11px; }
@container entity-graph (max-width: 660px) { .graph-workspace { grid-template-columns: minmax(0, 1fr); } .graph-detail { border-left: 0; border-top: 1px solid var(--td-component-stroke); max-height: 420px; } }
@media (max-width: 720px) { .graph-canvas { height: 380px; } .graph-empty { top: 300px; } }
</style>
