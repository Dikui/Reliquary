<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { searchGraph } from '@/api/graph'
import type { LibraryCatalogItem } from '@/api/library'
import type { GraphSearchResponse, GraphSource } from '@/types/graph'
import type { SourceLocation } from '@/types/search'
import { Button, EmptyState, ErrorState, Input, Select, Skeleton, Tag } from '@/components/ui'
import { entryDetailLocation, entryDetailHref } from '@/utils/bodyReference'
import { searchErrorCode } from '@/utils/search'

const props = defineProps<{ libraries: LibraryCatalogItem[] }>()
const { t } = useI18n()
const router = useRouter()
const entity = ref(''), library = ref(''), hops = ref('1')
const selectedEntityID = ref('')
const data = ref<GraphSearchResponse | null>(null)
const loading = ref(false), error = ref('')
const entityNotFound = ref(false)
const libraryName = computed(() => {
  const selected = props.libraries.find(item => item.id === library.value)
  return selected?.display_name || selected?.name || library.value
})
let sequence = 0
let controller: AbortController | undefined

function invalidate() {
  sequence++
  controller?.abort()
  loading.value = false
  error.value = ''
  entityNotFound.value = false
  data.value = null
}
watch(() => props.libraries, libraries => {
  if (!libraries.some(item => item.id === library.value)) library.value = libraries[0]?.id || ''
}, { immediate: true })
watch([entity, library, hops], invalidate, { flush: 'sync' })
watch([entity, library], () => { selectedEntityID.value = '' }, { flush: 'sync' })
const names = computed(() => new Map(data.value?.entities.map(item => [item.id, item.canonical_name]) || []))
const rows = computed(() => data.value?.edges.flatMap(edge => edge.sources.flatMap(source => {
  const evidence = source.evidence?.length ? source.evidence : [{ locations: [] }]
  return evidence.flatMap((item, index) => (item.locations?.length ? item.locations : [undefined]).map((location, position) => ({
    key: `${edge.id}:${source.entry_id}:${index}:${position}`, edge, source, location, speaker: item.speaker?.name,
  })))
})) || [])

async function runSearch(id?: string) {
  invalidate()
  if (id) selectedEntityID.value = id
  if (!library.value || !entity.value.trim()) { error.value = t('searchPage.graphRequired'); return }
  if (![1, 2, 3].includes(Number(hops.value))) { error.value = t('searchPage.invalidHops'); return }
  const current = sequence
  controller = new AbortController()
  loading.value = true
  const value = entity.value.trim()
  const input = selectedEntityID.value || /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i.test(value) ? { id: selectedEntityID.value || value } : { name: value }
  try {
    const response = await searchGraph({ libraries: [library.value], entity: input, mode: 'neighbors', hops: Number(hops.value), relation_types: [], limit: 50 }, controller.signal)
    if (current !== sequence) return
    if (!response.success) throw new Error('Graph search failed')
    data.value = response.data
  } catch (cause: unknown) {
    if (current !== sequence) return
    if (cause && typeof cause === 'object' && 'status' in cause && cause.status === 404 && searchErrorCode(cause) === 1003) entityNotFound.value = true
    else error.value = t('searchPage.graphFailed')
  } finally { if (current === sequence) loading.value = false }
}
function openSource(source: GraphSource, location?: SourceLocation) { void router.push(entryDetailLocation(source.entry_id, location)) }
onBeforeUnmount(invalidate)
</script>

<template>
  <section class="graph-search">
    <form class="graph-form" novalidate @submit.prevent="runSearch()">
      <label class="graph-field">{{ t('searchPage.entityInput') }}<Input v-model="entity" name="entity" :aria-label="t('searchPage.entityInput')" /></label>
      <label class="graph-field">{{ t('ikb18.scope') }}<Select v-model="library" name="library" :aria-label="t('ikb18.scope')"><option value="" disabled>{{ t('searchPage.chooseLibrary') }}</option><option v-for="item in libraries" :key="item.id" :value="item.id">{{ item.display_name || item.name }}</option></Select></label>
      <label class="graph-field">{{ t('entityGraph.hops') }}<Select v-model="hops" name="hops" :aria-label="t('entityGraph.hops')"><option v-for="count in [1, 2, 3]" :key="count" :value="String(count)">{{ t('entityGraph.hopCount', { count }, count) }}</option></Select></label>
      <Button type="submit" variant="primary" :loading="loading">{{ t('searchPage.queryRelations') }}</Button>
    </form>
    <section class="graph-results" :aria-busy="loading" :aria-label="t('searchPage.graphResults')">
      <ErrorState v-if="error" :title="error"><Button @click="runSearch()">{{ t('common.retry') }}</Button></ErrorState>
      <div v-else-if="loading" class="graph-loading" role="status"><Skeleton v-for="n in 3" :key="n" :label="t('search.searching')" /></div>
      <EmptyState v-else-if="entityNotFound" :title="t('searchPage.entityNotFound')" :description="t('searchPage.entityNotFoundHint', { library: libraryName })" />
      <template v-else-if="data">
        <div class="graph-summary" role="status">
          <strong>{{ t('searchPage.graphCount', { relations: data.edges.length, entities: data.entities.length }) }}</strong>
          <span v-if="data.pending_sync.global" class="graph-hint">{{ t('searchPage.pendingSync', { count: data.pending_sync.global, involved: data.pending_sync.involved }) }}</span>
          <span v-if="data.truncated" class="graph-hint">{{ t('searchPage.truncated') }}</span>
        </div>
        <section v-if="data.status === 'needs_selection'" class="graph-candidates">
          <h2>{{ t('searchPage.chooseEntity') }}</h2>
          <Button v-for="candidate in data.candidates.entity" :key="candidate.id" @click="runSearch(candidate.id)">{{ candidate.canonical_name }} <code>{{ candidate.id }}</code></Button>
        </section>
        <div v-else-if="rows.length" class="graph-table-wrap">
          <table class="graph-table">
            <thead><tr><th>{{ t('searchPage.subject') }}</th><th>{{ t('searchPage.relation') }}</th><th>{{ t('searchPage.object') }}</th><th>{{ t('searchPage.source') }}</th><th>{{ t('searchPage.position') }}</th><th>{{ t('searchPage.speaker') }}</th></tr></thead>
            <tbody><tr v-for="row in rows" :key="row.key">
              <td><strong>{{ names.get(row.edge.subject_id) || row.edge.subject_id }}</strong></td><td>{{ row.edge.relation_type }}</td><td><strong>{{ names.get(row.edge.object_id) || row.edge.object_id }}</strong></td>
              <td><a :href="entryDetailHref(row.source.entry_id, row.location)" @click.prevent="openSource(row.source, row.location)">{{ row.source.title }}</a><div class="graph-source-meta"><span>{{ row.source.library_name }}</span><Tag v-if="row.source.source_unverified">{{ t('review.sourceUnverified') }}</Tag><Tag v-if="row.source.acquisition === 'model_written'">{{ t('ingestStatus.modelWritten') }}</Tag></div></td>
              <td><a v-if="row.location" :href="entryDetailHref(row.source.entry_id, row.location)" @click.prevent="openSource(row.source, row.location)">¶{{ row.location.paragraph }}</a><span v-else>{{ t('searchPage.noPosition') }}</span></td>
              <td>{{ row.speaker || t('searchPage.noSpeaker') }}</td>
            </tr></tbody>
          </table>
        </div>
        <EmptyState v-else :title="t('searchPage.noRelations')" />
      </template>
      <EmptyState v-else :title="t('searchPage.graphIdle')" />
    </section>
  </section>
</template>

<style scoped>
.graph-search, .graph-results, .graph-loading { display: grid; gap: var(--space-4); min-width: 0; }
.graph-form { display: grid; grid-template-columns: minmax(0, 2fr) minmax(0, 1fr) minmax(0, 1fr) auto; gap: var(--space-3); align-items: end; }
.graph-field { display: grid; gap: var(--space-2); min-width: 0; font-size: var(--text-sm); }
.graph-summary { display: flex; flex-wrap: wrap; justify-content: space-between; gap: var(--space-2); padding-top: var(--space-3); border-top: 2px solid var(--rule); }
.graph-hint, .graph-source-meta { color: var(--muted); font-size: var(--text-xs); }
.graph-source-meta { display: flex; flex-wrap: wrap; gap: var(--space-2); margin-top: var(--space-2); }
.graph-candidates { display: grid; justify-items: start; gap: var(--space-3); }
.graph-candidates h2 { font-size: var(--text-lg); }
.graph-candidates code { font-family: var(--mono); overflow-wrap: anywhere; }
.graph-table-wrap { overflow-x: auto; }
.graph-table { width: 100%; border-collapse: collapse; font-size: var(--text-sm); }
.graph-table th, .graph-table td { padding: var(--space-3); border-bottom: 1px solid var(--border); text-align: left; vertical-align: top; overflow-wrap: anywhere; min-width: 5rem; }
.graph-table th { font-size: var(--text-xs); border-bottom: 2px solid var(--rule); }
.graph-table tbody tr:hover, .graph-table tbody tr:focus-within { background: var(--select); color: var(--on-select); }
.graph-table tbody tr:hover .graph-source-meta, .graph-table tbody tr:focus-within .graph-source-meta { color: var(--on-select); }
.graph-table tbody tr:hover :deep(.rq-tag), .graph-table tbody tr:focus-within :deep(.rq-tag) { background: var(--clear); color: var(--on-select); }
.graph-table a { color: inherit; text-decoration: underline; text-underline-offset: .18em; }
.graph-table a:hover { color: var(--accent); }
@media (max-width: 760px) { .graph-form { grid-template-columns: minmax(0, 1fr); } }
</style>
