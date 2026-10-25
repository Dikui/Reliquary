<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getEntityFacts, getEntityGraph, type Entity, type EntityGraph, type EntityGraphEdge } from '@/api/entities'
import type { StoryFact } from '@/api/stories'

const props = defineProps<{ entity: Entity }>()
const emit = defineEmits<{ select: [entity: Entity]; 'open-source': [id: string] }>()
const { t, locale } = useI18n()
const range = ref<string[]>([]), otherID = ref(''), direction = ref<'' | 'incoming' | 'outgoing'>('')
const graph = ref<EntityGraph | null>(null), facts = ref<StoryFact[]>([]), total = ref(0), page = ref(1)
const graphLoading = ref(false), factsLoading = ref(false), graphError = ref(''), factsError = ref('')
const pageSize = 20
let graphVersion = 0, factsVersion = 0
const neighbors = computed(() => graph.value?.nodes.filter(node => node.id !== props.entity.id) || [])
const counterpartOptions = computed(() => (graph.value?.nodes || [])
  .filter(node => node.id !== props.entity.id || selfEdges.value.length > 0)
  .map(node => ({ value: node.id, label: node.canonical_name })))
const branches = computed(() => neighbors.value.map(node => ({ node, edges: graph.value!.edges.filter(edge => edge.subject_entity_id === node.id || edge.object_entity_id === node.id) })))
const selfEdges = computed(() => graph.value?.edges.filter(edge => edge.subject_entity_id === props.entity.id && edge.object_entity_id === props.entity.id) || [])
const timeFilter = () => ({ from: range.value[0] || undefined, to: range.value[1] || undefined })
const occurrenceTime = (value: string) => /^\d{4}-\d{2}-\d{2}$/.test(value) ? value : new Date(value).toLocaleString(locale.value, { timeZoneName: 'short' })
const nodeName = (id: string) => graph.value?.nodes.find(node => node.id === id)?.canonical_name || id

async function loadGraph() {
  const version = ++graphVersion
  graphLoading.value = true; graphError.value = ''
  try {
    const result = await getEntityGraph(props.entity.id, timeFilter())
    if (!result.success) throw new Error()
    if (version === graphVersion) graph.value = result.data
  } catch {
    if (version === graphVersion) graphError.value = t('entityActivity.graphFailed')
  } finally {
    if (version === graphVersion) graphLoading.value = false
  }
}
async function loadFacts() {
  const version = ++factsVersion
  factsLoading.value = true; factsError.value = ''
  try {
    const result = await getEntityFacts(props.entity.id, {
      ...timeFilter(), other_entity_id: otherID.value || undefined, direction: direction.value || undefined,
      limit: pageSize, offset: (page.value - 1) * pageSize,
    })
    if (!result.success) throw new Error()
    if (version !== factsVersion) return
    facts.value = result.data.items; total.value = result.data.total
    const lastPage = Math.max(1, Math.ceil(total.value / pageSize))
    if (page.value > lastPage) page.value = lastPage
  } catch {
    if (version === factsVersion) factsError.value = t('entityActivity.factsFailed')
  } finally {
    if (version === factsVersion) factsLoading.value = false
  }
}
function selectEdge(edge: EntityGraphEdge) {
  otherID.value = edge.subject_entity_id === props.entity.id ? edge.object_entity_id : edge.subject_entity_id
  direction.value = edge.subject_entity_id === props.entity.id ? 'outgoing' : 'incoming'
}
function selectCounterpart() { direction.value = '' }
function allFacts() { page.value = 1; otherID.value = ''; direction.value = '' }
function changeCenter(entity: Entity) { allFacts(); emit('select', entity) }
function refresh() { void loadGraph(); void loadFacts() }
watch(() => props.entity.id, () => {
  graphVersion++; factsVersion++; graph.value = null; facts.value = []; total.value = 0
  allFacts(); page.value = 1; refresh()
}, { immediate: true })
watch(range, () => {
  graph.value = null; allFacts(); page.value = 1; void loadGraph()
}, { deep: true })
watch([range, otherID, direction], () => {
  factsVersion++; facts.value = []; total.value = 0
  if (page.value !== 1) page.value = 1
  else void loadFacts()
}, { deep: true })
watch(page, () => { facts.value = []; void loadFacts() })
onBeforeUnmount(() => { graphVersion++; factsVersion++ })
</script>

<template>
  <section class="entity-activity">
    <header class="activity-heading">
      <h4>{{ t('entityActivity.graph') }}</h4>
      <t-button variant="text" :loading="graphLoading || factsLoading" @click="refresh">{{ t('common.refresh') }}</t-button>
    </header>
    <t-date-range-picker v-model="range" allow-input clearable :placeholder="[t('entityActivity.from'), t('entityActivity.to')]" :aria-label="t('entityActivity.period')" />
    <p class="muted">{{ t('entityActivity.description') }}</p>
    <p class="muted">{{ t('entityActivity.timeHint') }}</p>
    <div v-if="graphError" class="activity-error" role="alert">{{ graphError }} <t-button variant="text" :loading="graphLoading" @click="loadGraph">{{ t('common.retry') }}</t-button></div>
    <div class="activity-layout">
      <section :aria-label="t('entityActivity.graph')" :aria-busy="graphLoading" class="graph-panel">
        <t-loading :loading="graphLoading">
          <div v-if="graph" class="entity-graph">
            <button type="button" class="graph-node graph-center" @click="allFacts">{{ entity.canonical_name }}</button>
            <div class="graph-branches">
              <div v-for="branch in branches" :key="branch.node.id" class="graph-branch">
                <div class="graph-edges">
                  <button v-for="edge in branch.edges" :key="`${edge.subject_entity_id}:${edge.object_entity_id}`" type="button" class="graph-edge" :aria-pressed="otherID === branch.node.id && direction === (edge.subject_entity_id === entity.id ? 'outgoing' : 'incoming')" @click="selectEdge(edge)">
                    <span>{{ nodeName(edge.subject_entity_id) }} → {{ nodeName(edge.object_entity_id) }}</span>
                    <strong>{{ edge.latest_action }}</strong><small>{{ t('entityActivity.factCount', { count: edge.fact_count }) }}</small>
                  </button>
                </div>
                <button type="button" class="graph-node" :aria-label="t('entityActivity.centerOn', { name: branch.node.canonical_name })" @click="changeCenter(branch.node)">{{ branch.node.canonical_name }}</button>
              </div>
              <button v-for="edge in selfEdges" :key="edge.subject_entity_id" type="button" class="graph-edge" @click="selectEdge(edge)">
                {{ entity.canonical_name }} → {{ entity.canonical_name }} · {{ edge.latest_action }} · {{ t('entityActivity.factCount', { count: edge.fact_count }) }}
              </button>
              <t-empty v-if="!graph.edges.length" :description="t('entityActivity.noEdges')" />
            </div>
          </div>
        </t-loading>
      </section>
      <aside class="fact-panel" :aria-label="t('entityActivity.timeline')" :aria-busy="factsLoading">
        <header class="activity-heading"><h4>{{ t('entityActivity.timeline') }}</h4><t-button variant="text" @click="allFacts">{{ t('entityActivity.allFacts') }}</t-button></header>
        <t-select v-model="otherID" clearable :options="counterpartOptions" :placeholder="t('entityActivity.counterpart')" :aria-label="t('entityActivity.counterpart')" @change="selectCounterpart" />
        <p v-if="direction" class="muted">{{ t(direction === 'outgoing' ? 'entityActivity.outgoing' : 'entityActivity.incoming') }}</p>
        <div v-if="factsError" class="activity-error" role="alert">{{ factsError }} <t-button variant="text" :loading="factsLoading" @click="loadFacts">{{ t('common.retry') }}</t-button></div>
        <t-loading :loading="factsLoading">
          <ol v-if="facts.length" class="fact-list">
            <li v-for="fact in facts" :key="fact.id" class="fact-record">
              <p class="muted"><time v-if="fact.occurred_at" :datetime="fact.occurred_at">{{ occurrenceTime(fact.occurred_at) }}</time><span v-else>{{ t('stories.unknownTime') }}</span></p>
              <router-link :to="{ path: `/platform/stories/${encodeURIComponent(fact.story_id)}`, hash: `#fact-${fact.id}` }">{{ fact.title }}</router-link>
              <p>{{ fact.subject }} · {{ fact.action }} · {{ fact.object }}</p><p v-if="fact.conditions" class="muted">{{ fact.conditions }}</p>
              <p v-if="fact.manual_corrected" class="muted">{{ t('groupingCorrection.manual') }}</p>
              <p v-if="fact.no_source" class="muted">{{ t('stories.noSource') }}</p>
              <ul class="sources"><li v-for="source in fact.sources" :key="source.entry_id"><t-button variant="text" @click="emit('open-source', source.entry_id)">{{ source.title }}</t-button><blockquote>{{ source.quote }}</blockquote></li></ul>
            </li>
          </ol>
          <t-empty v-else-if="!factsLoading && !factsError" :description="t('entityActivity.noFacts')" />
        </t-loading>
        <t-pagination v-if="total" v-model="page" :total="total" :page-size="pageSize" :show-page-size="false" :disabled="factsLoading" size="small" />
      </aside>
    </div>
  </section>
</template>

<style scoped lang="less">
.entity-activity { margin: 20px 0; min-width: 0; container-type: inline-size; }
.activity-layout { display: grid; gap: 20px; min-width: 0; }
@container (min-width: 860px) { .activity-layout { grid-template-columns: minmax(0, 1fr) minmax(280px, 1fr); } }
.activity-heading { display: flex; gap: 12px; align-items: center; justify-content: space-between; }
.muted { color: var(--td-text-color-secondary); font-size: 13px; }
.activity-error { color: var(--td-error-color); margin: 12px 0; }
.graph-panel { min-height: 120px; margin: 16px 0; }
.entity-graph { display: grid; grid-template-columns: minmax(80px, 1fr) minmax(0, 4fr); gap: 16px; align-items: center; }
.graph-branches { border-left: 1px solid var(--td-component-stroke); display: grid; gap: 16px; max-height: 420px; overflow: auto; padding: 4px; }
.graph-branch { position: relative; padding-left: 12px; display: grid; grid-template-columns: minmax(0, 3fr) minmax(80px, 2fr); align-items: center; gap: 12px; }
.graph-branch::before { content: ""; position: absolute; width: 12px; left: 0; top: 50%; border-top: 1px solid var(--td-component-stroke); }
.graph-edges { display: grid; gap: 8px; border-top: 1px solid var(--td-component-stroke); padding-top: 8px; }
.graph-node, .graph-edge { min-width: 0; white-space: normal; overflow-wrap: anywhere; font: inherit; color: var(--td-text-color-primary); background: var(--td-bg-color-container); border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); cursor: pointer; padding: 10px; text-align: left; }
.graph-center { position: relative; border-color: var(--td-brand-color); font-weight: 600; }
.graph-center::after { content: ""; position: absolute; left: 100%; top: 50%; width: 16px; border-top: 1px solid var(--td-component-stroke); }
.graph-edge { display: grid; gap: 4px; width: 100%; font-size: 13px; }
.graph-node:hover, .graph-edge:hover, .graph-edge[aria-pressed="true"] { background: var(--td-bg-color-container-hover); border-color: var(--td-brand-color); }
.graph-node:active, .graph-edge:active { background: var(--td-bg-color-container-active); }
.graph-node:focus-visible, .graph-edge:focus-visible, a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.fact-panel { min-height: 160px; border-top: 1px solid var(--td-component-stroke); }
.fact-list, .sources { list-style: none; padding: 0; }
.fact-record { padding: 16px 0; border-bottom: 1px solid var(--td-component-stroke); overflow-wrap: anywhere; }
.fact-record p { margin: 8px 0; }
.fact-record a { color: var(--td-brand-color); }
.fact-record a:hover { text-decoration: underline; }
blockquote { border-left: 2px solid var(--td-component-stroke); margin: 8px 0; padding: 0 12px; white-space: pre-wrap; }
@container (max-width: 640px) { .graph-center::after { display: none; } .entity-graph { grid-template-columns: minmax(0, 1fr); } .graph-branch { grid-template-columns: minmax(0, 3fr) minmax(64px, 2fr); } }
</style>
