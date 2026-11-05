<template>
  <section class="entry-workbench" :class="{ 'is-empty': !entities.length && !entitiesLoading && !selectedEntity && !entityDetailLoading && !initialEntityId }">
    <aside class="entity-list" :aria-label="t('workbench.entities.title')">
      <div class="entity-list-head">
        <h1>{{ t('workbench.entities.title') }}</h1>
        <div class="entity-filters">
          <Select :model-value="selectedLibraryId" @update:model-value="changeLibrary" :disabled="merging || catalogLoading" :aria-label="t('entityPage.library')"><option value="" disabled>{{ t('searchPage.chooseLibrary') }}</option><option v-for="library in libraries" :key="library.id" :value="library.id">{{ library.display_name || library.name }}</option></Select>
          <Select v-if="managed" v-model="kindFilter" :aria-label="t('managed.kind')"><option value="">{{ t('workbench.entities.allTypes') }}</option><option v-for="kind in selectedLibrary?.entity_schema?.kinds" :key="kind.key" :value="kind.key">{{ kind.name }}</option></Select>
          <Select v-if="!managed" v-model="entityTypeFilter" :disabled="merging" :aria-label="t('workbench.entities.type')"><option value="">{{ t('workbench.entities.allTypes') }}</option><option v-for="option in entityTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option></Select>
        </div>
        <div class="entity-search"><Input ref="searchInput" v-model="entityFilter" type="search" :disabled="merging" :placeholder="t('workbench.entities.filter')" :aria-label="t('workbench.entities.filter')" @compositionstart="entityComposing = true" @compositionend="endEntityComposition" @keydown.enter="onEntityEnter(undefined, { e: $event })" /><Button v-if="entityFilter" variant="ghost" :aria-label="t('common.clear')" @click="clearSearch">×</Button></div>
      </div>
      <ManagedEntityEditor v-if="managed && selectedLibrary" :library="selectedLibrary" @saved="managedSaved" />
      <label v-if="managed"><input v-model="includeArchived" type="checkbox">{{ t('managed.includeArchived') }}</label>
      <a v-if="!managed" class="merge-link" :href="pendingHref">{{ t('mergeSuggestions.title') }}<span v-if="pendingCount !== null" class="mono">{{ pendingCount }}</span></a>
      <button type="button" class="merge-link overview-link" :disabled="!selectedLibraryId || merging" :aria-pressed="overview" @click="overview = true">{{ t('entityGraph.overview') }}</button>
      <ErrorState v-if="catalogError" :title="catalogError"><Button @click="loadCatalog">{{ t('common.retry') }}</Button></ErrorState>
      <ErrorState v-else-if="loadError" :title="loadError"><Button @click="loadEntities()">{{ t('common.retry') }}</Button></ErrorState>
      <Skeleton v-if="entitiesLoading && !entities.length" :label="t('common.loading')" />
      <ul v-else class="entity-items">
        <li v-for="entity in entities" :key="entity.id"><button type="button" :disabled="merging" :aria-current="!overview && selectedEntity?.id === entity.id ? 'true' : undefined" @click="selectEntity(entity)"><strong>{{ entity.canonical_name }}</strong><small>{{ entityTypeLabel(entity.entity_type) }} · {{ entity.aliases?.length || 0 }} {{ t('workbench.entities.aliases', entity.aliases?.length || 0) }}</small></button></li>
      </ul>
      <EmptyState v-if="!entities.length && !entitiesLoading && !loadError && !catalogLoading && !catalogError" :title="t(entityFilter || entityTypeFilter ? 'workbench.entities.noResults' : 'entityPage.empty')" :description="t('entityPage.emptyHint')"><a href="/platform/entries">{{ t('entityPage.viewMaterials') }}</a></EmptyState>
      <Button v-if="hasMoreEntities" :loading="entitiesLoading" @click="loadEntities(true)">{{ t('workbench.loadMore') }}</Button>
    </aside>
    <div v-if="overview || selectedEntity || entityDetailLoading || entityDetailError || entityNotFound || entities.length" class="entity-detail">
      <template v-if="overview">
        <div class="entity-eyebrow"><LibMark v-if="selectedLibrary" :color="selectedLibrary.color" />{{ selectedLibrary?.display_name || selectedLibrary?.name || selectedLibraryId }}</div>
        <div class="entity-head"><h2>{{ t('entityGraph.overview') }}</h2></div>
        <EntityGraph :managed="managed" :library-id="selectedLibraryId" :relation-type-options="graphRelationTypes" @open-source="(id, location) => emit('open-source', id, location)" @open-entity="selectInitialEntity" @relation-types-changed="loadCatalog" />
      </template>
      <EmptyState v-else-if="entityNotFound" :title="t('entityPage.notFound')" :description="t('entityPage.notFoundHint')" />
      <ErrorState v-else-if="entityDetailError" :title="entityDetailError"><Button @click="selectedEntity ? selectEntity(selectedEntity) : selectInitialEntity(initialEntityId)">{{ t('common.retry') }}</Button></ErrorState>
      <Skeleton v-else-if="entityDetailLoading" :label="t('common.loading')" />
      <EmptyState v-else-if="!selectedEntity" :title="t('workbench.entities.choose')" />
      <template v-else>
        <div class="entity-eyebrow"><LibMark v-if="selectedLibrary" :color="selectedLibrary.color" />{{ selectedLibrary?.display_name || selectedLibrary?.name || selectedEntity.library_id }} · {{ entityTypeLabel(selectedEntity.entity_type) }} · <span class="mono">{{ selectedEntity.id }}</span></div>
        <div class="entity-head"><h2>{{ selectedEntity.canonical_name }}</h2><div class="entity-actions"><FollowToggle target-type="entity" :target-id="selectedEntity.id" :name="selectedEntity.canonical_name" /><Button v-if="!managed" @click="openMerge">{{ t('workbench.entities.merge') }}</Button></div></div>
        <ManagedEntityEditor v-if="managed && selectedLibrary" :key="selectedEntity.id" :library="selectedLibrary" :entity="selectedEntity" @saved="managedSaved" />
        <ManagedRelationEditor v-if="managed && selectedLibrary && !selectedEntity.archived_at" :key="`${selectedEntity.id}-relation`" :library="selectedLibrary" :entity="selectedEntity" @saved="loadRelations()" />
        <dl v-if="managed" class="entity-facts"><div><dt>{{ t('managed.kind') }}</dt><dd>{{ selectedLibrary?.entity_schema?.kinds?.find(k => k.key === selectedEntity?.kind)?.name || selectedEntity.kind }}</dd></div><div><dt>{{ t('managed.code') }}</dt><dd>{{ selectedEntity.code || '—' }}</dd></div><div v-for="(value, key) in selectedEntity.attributes" :key="key"><dt>{{ selectedLibrary?.entity_schema?.fields?.find(f => f.key === key)?.name || key }}</dt><dd>{{ typeof value === 'boolean' ? t(value ? 'managed.yes' : 'managed.no') : value }} {{ selectedLibrary?.entity_schema?.fields?.find(f => f.key === key)?.unit }}</dd></div></dl>
        <p v-if="selectedEntity.description" class="entity-intro">{{ selectedEntity.description }}</p>
        <dl class="entity-facts"><div><dt>{{ t('workbench.entities.aliasLabel') }}</dt><dd>{{ selectedEntity.aliases?.join(' · ') || t('workbench.entities.noAliases') }}</dd></div><div><dt>{{ t('entityPage.materials') }}</dt><dd class="mono">{{ entityEntries.length }}</dd></div><div><dt>{{ t('ikb18.relations') }}</dt><dd class="mono">{{ relationsLoading || relationError ? t('workbench.nameEmbeddings.unknown') : relationCount }}</dd></div></dl>
        <Tabs v-model="relationView" :items="detailTabs" :label="t('entityGraph.view')">
          <template #graph><EntityGraph :managed="managed" :entity="selectedEntity" :relation-type-options="graphRelationTypes" :active="relationView === 'graph'" @open-source="(id, location) => emit('open-source', id, location)" @open-entity="selectInitialEntity" @relation-types-changed="loadCatalog" /></template>
          <template #list><EntityRelations :entity-id="selectedEntity.id" :entity-name="selectedEntity.canonical_name" :groups="relations" :managed-library="managed ? selectedLibrary : undefined" :entity="selectedEntity" :loading="relationsLoading" :error="relationError" @open-source="(id, location) => emit('open-source', id, location)" @open-entity="selectInitialEntity" @retry="loadRelations()" @changed="loadRelations()" /></template>
          <template #entries>
            <section class="entity-articles">
              <a :href="`/platform/entries?libraries=${encodeURIComponent(selectedEntity.library_id)}&entity_ids=${encodeURIComponent(selectedEntity.id)}`">{{ t('entryBatch.manageRelated') }}</a>
              <EmptyState v-if="!entityEntries.length" :title="t('workbench.entities.noArticles')" />
              <div v-else class="entity-table-scroll">
                <table><thead><tr><th>{{ t('entityPage.number') }}</th><th>{{ t('entityPage.title') }}</th><th>{{ t('entityPage.type') }}</th><th>{{ t('entityPage.collected') }}</th></tr></thead>
                  <tbody><tr v-for="article in entityEntries" :key="article.id"><td class="mono" :title="article.id">{{ article.id.slice(0, 8) }}</td><td><button class="entity-link" type="button" @click="emit('open-source', article.id)">{{ article.title || article.file_name || t('workbench.entities.untitledArticle') }}</button></td><td><Tag v-if="article.content_type">{{ contentTypeLabel(article.content_type, t) }}</Tag><span v-else>{{ t('entityPage.unknownType') }}</span></td><td class="mono">{{ formatDate(article.created_at) }}</td></tr></tbody>
                </table>
              </div>
            </section>
          </template>
          <template #aliases><section class="entity-aliases"><h3>{{ t('workbench.entities.aliasLabel') }}</h3><ul v-if="selectedEntity.aliases?.length"><li v-for="alias in selectedEntity.aliases" :key="alias">{{ alias }}</li></ul><p v-else>{{ t('workbench.entities.noAliases') }}</p><p>{{ t('workbench.entities.mergeHint') }}</p><Button v-if="!managed" @click="openMerge">{{ t('workbench.entities.merge') }}</Button>
            <details class="name-embedding-rebuild"><summary>{{ t('workbench.nameEmbeddings.title') }}</summary><p>{{ t('workbench.nameEmbeddings.pending', { count: nameEmbeddingStatus?.pending_count ?? t('workbench.nameEmbeddings.unknown') }, nameEmbeddingStatus?.pending_count ?? 0) }}</p><div class="entity-actions"><Button :loading="nameEmbeddingStatusLoading" @click="loadNameEmbeddingStatus">{{ t('workbench.refresh') }}</Button><Button :loading="nameEmbeddingRebuilding" :disabled="nameEmbeddingStatusLoading || !nameEmbeddingStatus?.pending_count || nameEmbeddingBusy" @click="rebuildNameEmbeddings">{{ t('workbench.nameEmbeddings.rebuild') }}</Button></div><p v-if="nameEmbeddingStatus?.latest_run" role="status">{{ t('workbench.nameEmbeddings.latest', { status: nameEmbeddingRunStatus, processed: nameEmbeddingStatus.latest_run.processed_count, total: nameEmbeddingStatus.latest_run.total_count }, nameEmbeddingStatus.latest_run.total_count) }}</p><p v-if="nameEmbeddingStatus?.latest_run?.error" class="run-error" role="alert">{{ nameEmbeddingStatus.latest_run.error }}</p><p v-if="nameEmbeddingError" class="run-error" role="alert">{{ nameEmbeddingError }}</p></details>
          </section></template>
        </Tabs>
      </template>
    </div>
    <Dialog v-model="mergeVisible" :title="t('workbench.entities.mergeTitle')" :closable="!merging" :close-on-overlay-click="false" :close-on-esc-keydown="!merging">
      <div class="merge-form"><p>{{ t('workbench.entities.mergeSource') }}: {{ selectedEntity?.canonical_name }}</p><p>{{ t('workbench.entities.mergeHint') }}</p><label for="entity-merge-target">{{ t('workbench.entities.mergeTarget') }}</label><Combobox id="entity-merge-target" v-model="mergeTargetId" :options="mergeTargetOptions" :disabled="merging || mergeTargetsLoading" :placeholder="t('workbench.entities.mergeTargetPlaceholder')" /><p v-if="mergeTargetsLoading" role="status">{{ t('common.loading') }}</p><Button v-if="hasMoreMergeTargets" :loading="mergeTargetsLoading" :disabled="merging" @click="loadMergeTargets(true)">{{ t('workbench.loadMore') }}</Button><p>{{ t('workbench.entities.mergeWarning') }}</p><p v-if="mergeError" class="run-error" role="alert">{{ mergeError }}</p><Button v-if="mergeError" :disabled="merging" @click="loadMergeTargets()">{{ t('common.retry') }}</Button></div>
      <template #footer><Button :disabled="merging" @click="mergeVisible = false">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="merging" :disabled="!mergeTargetId || mergeTargetsLoading" @click="confirmMerge">{{ t('workbench.entities.merge') }}</Button></template>
    </Dialog>
  </section>
</template>
<script setup lang="ts">
import { contentTypeLabel } from '@/utils/contentTypeLabel'
import { formatDateTime } from '@/utils/dateTime'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Combobox, Dialog, EmptyState, ErrorState, Input, LibMark, Select, Skeleton, Tabs, Tag } from '@/components/ui'
import { toast } from '@/components/ui/feedback'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'
import { listEntityRelations, type RelationGroup } from '@/api/relations'
import { searchErrorCode } from '@/utils/search'
import { ENTITY_TYPES, listEntityMergeSuggestions, getEntity, getEntityNameEmbeddingRebuildStatus, listEntities, mergeEntities, rebuildEntityNameEmbeddings, type Entity, type EntityEntry, type EntityNameEmbeddingRebuildStatus, type EntityType } from '@/api/entities'
import ManagedEntityEditor from './ManagedEntityEditor.vue'
import ManagedRelationEditor from './ManagedRelationEditor.vue'
import EntityRelations from './EntityRelations.vue'
import EntityGraph from './EntityGraph.vue'
import type { SourceLocation } from '@/types/search'
import FollowToggle from '@/components/FollowToggle.vue'
import { useFollowStore } from '@/stores/follows'

const props = withDefaults(defineProps<{ initialEntityId?: string; libraryId?: string }>(), { initialEntityId: '', libraryId: '' })
const emit = defineEmits<{ 'open-source': [entryId: string, location?: SourceLocation]; 'select-entity': [id: string, libraryId: string] }>()
const { t, locale } = useI18n()
const followStore = useFollowStore()
const libraryScope = ref<string[]>(props.libraryId ? [props.libraryId] : [])
const selectedLibraryId = computed(() => libraryScope.value[0] || '')
const pageSize = 50
const relationView = ref('graph'), overview = ref(false)
const searchInput = ref<InstanceType<typeof Input>>()
function clearSearch() { entityFilter.value = ''; searchInput.value?.focus() }
const includeArchived = ref(false), kindFilter = ref('')
const managed = computed(() => selectedLibrary.value?.entity_mode === 'managed')
async function managedSaved(entity: Entity) { await loadEntities(); await selectInitialEntity(entity.id) }
const entityFilter = ref(''), entityTypeFilter = ref<EntityType | ''>(''), entityComposing = ref(false)
const entityTypeOptions = computed(() => ENTITY_TYPES.map(value => ({ value, label: entityTypeLabel(value) })))
const entities = ref<Entity[]>([]), entitiesLoading = ref(false), hasMoreEntities = ref(false), loadError = ref('')
const selectedEntity = ref<Entity | null>(null), entityEntries = ref<EntityEntry[]>([]), entityDetailLoading = ref(false), entityDetailError = ref('')
const loading = ref(false), mergeVisible = ref(false), mergeTargetId = ref(''), merging = ref(false), mergeError = ref('')
const mergeTargets = ref<Entity[]>([]), mergeTargetsLoading = ref(false), hasMoreMergeTargets = ref(false)
const mergeTargetOptions = computed(() => mergeTargets.value.filter(item => item.id !== selectedEntity.value?.id && !item.merged_into_id).map(item => ({ value: item.id, label: `${item.canonical_name} · ${entityTypeLabel(item.entity_type)}` })))
let entityRequest = 0, detailRequest = 0, mergeTargetRequest = 0
let entitySearchTimer: ReturnType<typeof setTimeout> | null = null
function entityTypeLabel(value: string) { return t(`workbench.entities.types.${ENTITY_TYPES.includes(value as EntityType) ? value : 'other'}`) }
function changeLibrary(id: string) { libraryScope.value = id ? [id] : []; emit('select-entity', '', id) }
function formatDate(value?: string) { return formatDateTime(value, locale.value, { dateStyle: 'medium', timeStyle: 'short' }, t('workbench.unknownDate')) }
function envelopeData<T>(response: { success: boolean; data: T; error?: string }): T { if (!response.success) throw new Error(response.error || t('workbench.requestFailed')); return response.data }
function errorMessage(cause: unknown) { return cause instanceof Error ? cause.message : t('workbench.requestFailed') }
async function loadEntities(more = false) {
  if (more && entitiesLoading.value) return
  const current = ++entityRequest
  if (!selectedLibraryId.value) { entities.value = []; hasMoreEntities.value = false; entitiesLoading.value = false; return }
  entitiesLoading.value = true; loadError.value = ''
  try { const page = envelopeData(await listEntities({ library_id: selectedLibraryId.value, entity: entityFilter.value.trim(), entity_type: managed.value ? undefined : entityTypeFilter.value || undefined, include_archived: includeArchived.value || undefined, kind: managed.value ? kindFilter.value || undefined : undefined, limit: pageSize, offset: more ? entities.value.length : 0 })) || []; if (current === entityRequest) { entities.value = more ? [...entities.value, ...page] : page; hasMoreEntities.value = page.length === pageSize } }
  catch (cause) { if (current === entityRequest) loadError.value = errorMessage(cause) }
  finally { if (current === entityRequest) entitiesLoading.value = false }
}
function selectEntity(entity: Entity) { return selectInitialEntity(entity.id, entity) }
function resetEntityList() { if (entitySearchTimer) clearTimeout(entitySearchTimer); entitySearchTimer = null; entityRequest++; detailRequest++; entities.value = []; selectedEntity.value = null; entityEntries.value = []; hasMoreEntities.value = false; entityDetailLoading.value = false; entityDetailError.value = ''; entityNotFound.value = false; relations.value = []; relationRequest++; relationsLoading.value = false; relationError.value = ''; void loadEntities() }
function scheduleEntitySearch() { if (entitySearchTimer) clearTimeout(entitySearchTimer); entityRequest++; if (entityComposing.value) return; if (!entityFilter.value.trim()) { resetEntityList(); return }; entitySearchTimer = setTimeout(() => { entitySearchTimer = null; resetEntityList() }, 300) }
function endEntityComposition() { entityComposing.value = false; scheduleEntitySearch() }
function onEntityEnter(_value: unknown, options?: { e?: KeyboardEvent }) { if (entityComposing.value || (options?.e?.isComposing || options?.e?.keyCode === 229)) return; if (entitySearchTimer) clearTimeout(entitySearchTimer); entitySearchTimer = null; resetEntityList() }
async function refreshAfterMerge(retainedId?: string) {
  if (!retainedId) { selectedEntity.value = null; entityEntries.value = []; relations.value = []; detailRequest++; relationRequest++ }
  await Promise.all([followStore.refreshAfterTargetChange(), loadEntities(), loadNameEmbeddingStatus(), loadPendingCount()])
  if (retainedId) await selectInitialEntity(retainedId)
}
async function refreshAll() { loading.value = true; try { await Promise.all([loadEntities(), loadNameEmbeddingStatus(), selectedEntity.value ? selectEntity(selectedEntity.value) : Promise.resolve()]) } finally { loading.value = false } }
async function loadMergeTargets(more = false) {
  if (more && mergeTargetsLoading.value) return
  if (!selectedLibraryId.value) return
  const current = ++mergeTargetRequest; mergeTargetsLoading.value = true; mergeError.value = ''
  try { const page = envelopeData(await listEntities({ library_id: selectedLibraryId.value, limit: pageSize, offset: more ? mergeTargets.value.length : 0 })) || []; if (current === mergeTargetRequest) { mergeTargets.value = more ? [...mergeTargets.value, ...page] : page; hasMoreMergeTargets.value = page.length === pageSize } }
  catch (cause) { if (current === mergeTargetRequest) mergeError.value = errorMessage(cause) }
  finally { if (current === mergeTargetRequest) mergeTargetsLoading.value = false }
}
function openMerge() { mergeVisible.value = true; mergeTargetId.value = ''; mergeTargets.value = []; hasMoreMergeTargets.value = false; void loadMergeTargets() }
async function confirmMerge() {
  if (merging.value || !selectedEntity.value || !mergeTargetId.value) return
  const retainedId = mergeTargetId.value, sourceId = selectedEntity.value.id
  merging.value = true; mergeError.value = ''
  try { const response = await mergeEntities({ keep_entity_id: retainedId, merge_entity_id: sourceId }); if (!response.success) throw new Error(response.error || t('workbench.requestFailed')); mergeVisible.value = false; toast.success(t('workbench.entities.mergeSaved')); await refreshAfterMerge(retainedId) }
  catch (cause) { mergeError.value = errorMessage(cause) }
  finally { merging.value = false }
}
const nameEmbeddingStatus = ref<EntityNameEmbeddingRebuildStatus | null>(null), nameEmbeddingStatusLoading = ref(false), nameEmbeddingRebuilding = ref(false), nameEmbeddingError = ref('')
const nameEmbeddingBusy = computed(() => ['pending', 'running'].includes(nameEmbeddingStatus.value?.latest_run?.status || ''))
const nameEmbeddingRunStatus = computed(() => { const status = nameEmbeddingStatus.value?.latest_run?.status || ''; return ['pending', 'running', 'completed', 'failed'].includes(status) ? t(`workbench.nameEmbeddings.status.${status}`) : status })
let nameEmbeddingRequest = 0
let nameEmbeddingPollTimer: ReturnType<typeof setTimeout> | null = null
function clearNameEmbeddingPoll() { if (nameEmbeddingPollTimer) clearTimeout(nameEmbeddingPollTimer); nameEmbeddingPollTimer = null }
function scheduleNameEmbeddingPoll() { clearNameEmbeddingPoll(); if (nameEmbeddingBusy.value) nameEmbeddingPollTimer = setTimeout(() => { nameEmbeddingPollTimer = null; void loadNameEmbeddingStatus() }, 5000) }
async function loadNameEmbeddingStatus() {
  const current = ++nameEmbeddingRequest; clearNameEmbeddingPoll(); nameEmbeddingStatusLoading.value = true; nameEmbeddingError.value = ''
  try { const result = envelopeData(await getEntityNameEmbeddingRebuildStatus()); if (current === nameEmbeddingRequest) { nameEmbeddingStatus.value = result; scheduleNameEmbeddingPoll() } }
  catch (cause) { if (current === nameEmbeddingRequest) nameEmbeddingError.value = errorMessage(cause) }
  finally { if (current === nameEmbeddingRequest) nameEmbeddingStatusLoading.value = false }
}
async function rebuildNameEmbeddings() {
  if (nameEmbeddingRebuilding.value || nameEmbeddingBusy.value || !nameEmbeddingStatus.value?.pending_count) return
  const current = ++nameEmbeddingRequest; clearNameEmbeddingPoll(); nameEmbeddingRebuilding.value = true; nameEmbeddingError.value = ''
  try { const result = envelopeData(await rebuildEntityNameEmbeddings()); if (current === nameEmbeddingRequest) { nameEmbeddingStatus.value = result; toast.success(t('workbench.nameEmbeddings.queued')); scheduleNameEmbeddingPoll() } }
  catch (cause) { if (current === nameEmbeddingRequest) nameEmbeddingError.value = errorMessage(cause) }
  finally { if (current === nameEmbeddingRequest) nameEmbeddingRebuilding.value = false }
}
async function selectInitialEntity(id: string, preview?: Entity) {
  if (!id) return
  overview.value = false
  const current = ++detailRequest
  relationRequest++
  relations.value = []; relationError.value = ''; relationsLoading.value = false
  entityNotFound.value = false; selectedEntity.value = preview || null
  entityEntries.value = []; entityDetailError.value = ''; entityDetailLoading.value = true
  try {
    const result = envelopeData(await getEntity(id))
    if (current !== detailRequest) return
    if (selectedLibraryId.value !== result.entity.library_id) libraryScope.value = [result.entity.library_id]
    selectedEntity.value = result.entity; entityEntries.value = result.entries || []
    entityDetailLoading.value = false
    void loadRelations(result.entity.id)
    emit('select-entity', result.entity.id, result.entity.library_id)
  } catch (cause) {
    if (current !== detailRequest) return
    if (isMissingEntity(cause)) { entityNotFound.value = true; selectedEntity.value = null }
    else entityDetailError.value = errorMessage(cause)
  } finally { if (current === detailRequest) entityDetailLoading.value = false }
}
const libraries = ref<LibraryCatalogItem[]>([]), catalogLoading = ref(false), catalogError = ref('')
const selectedLibrary = computed(() => libraries.value.find(item => item.id === selectedLibraryId.value))
const graphRelationTypes = computed(() => (selectedLibrary.value?.relation_types || []).filter(type => type.enabled).map(type => type.name))
const relations = ref<RelationGroup[]>([]), relationsLoading = ref(false), relationError = ref('')
const relationCount = computed(() => relations.value.reduce((count, group) => count + group.relations.length, 0))
const entityNotFound = ref(false), pendingCount = ref<number | null>(null)
const detailTabs = computed(() => [
  { value: 'graph', label: t('entityGraph.graph') },
  { value: 'list', label: t('ikb18.relations') + (relationError.value || relationsLoading.value ? '' : ` ${relationCount.value}`) },
  { value: 'entries', label: t('entityPage.materials') + (entityDetailLoading.value || entityDetailError.value ? '' : ` ${entityEntries.value.length}`) },
  { value: 'aliases', label: t('entityPage.aliasesAndMerge') },
])
const pendingHref = computed(() => `/platform/pending?tab=merges&libraries=${encodeURIComponent(selectedLibraryId.value)}`)
let catalogRequest = 0, pendingRequest = 0, relationRequest = 0
function isMissingEntity(cause: unknown) { return !!cause && typeof cause === 'object' && 'status' in cause && cause.status === 404 && searchErrorCode(cause) === 1003 }
async function loadCatalog() {
  const current = ++catalogRequest; catalogLoading.value = true; catalogError.value = ''
  try {
    const response = await listLibraries()
    if (!response.success) throw new Error(response.message || t('workbench.requestFailed'))
    if (current !== catalogRequest) return
    libraries.value = response.data || []
    if (!selectedLibraryId.value && !props.initialEntityId) libraryScope.value = libraries.value[0] ? [libraries.value[0].id] : []
  } catch (cause) { if (current === catalogRequest) catalogError.value = errorMessage(cause) }
  finally { if (current === catalogRequest) catalogLoading.value = false }
}
async function loadRelations(id = selectedEntity.value?.id) {
  const current = ++relationRequest; relations.value = []; relationError.value = ''; relationsLoading.value = false
  if (!id) return
  relationsLoading.value = true
  try {
    const response = await listEntityRelations(id)
    if (!response.success) throw new Error(response.message || t('workbench.requestFailed'))
    if (current === relationRequest) relations.value = response.data || []
  } catch (cause) { if (current === relationRequest) relationError.value = errorMessage(cause) }
  finally { if (current === relationRequest) relationsLoading.value = false }
}
async function loadPendingCount() {
  const current = ++pendingRequest; pendingCount.value = null
  if (!selectedLibraryId.value) return
  try {
    let count = 0
    for (let offset = 0; current === pendingRequest; offset += 100) {
      const response = await listEntityMergeSuggestions({ library_id: selectedLibraryId.value, status: 'pending', limit: 100, offset })
      if (!response.success) return
      count += response.data.length
      if (response.data.length < 100) { if (current === pendingRequest) pendingCount.value = count; return }
    }
  } catch { /* An unavailable count stays unknown. */ }
}
watch(() => props.libraryId, id => { if (id && id !== selectedLibraryId.value) libraryScope.value = [id] })
watch(selectedLibraryId, () => { mergeTargetRequest++; mergeVisible.value = false; mergeTargets.value = []; resetEntityList(); void loadPendingCount() }, { flush: 'sync' })
watch(entityFilter, scheduleEntitySearch)
watch(entityTypeFilter, resetEntityList)
watch(includeArchived, resetEntityList)
watch(kindFilter, resetEntityList)
watch(() => props.initialEntityId, id => { if (id && id !== selectedEntity.value?.id) void selectInitialEntity(id) }, { immediate: true })
onMounted(() => { void loadCatalog(); void refreshAll(); void loadPendingCount() })
onBeforeUnmount(() => { entityRequest++; detailRequest++; mergeTargetRequest++; nameEmbeddingRequest++; catalogRequest++; pendingRequest++; relationRequest++; clearNameEmbeddingPoll(); if (entitySearchTimer) clearTimeout(entitySearchTimer) })
</script>
<style scoped>
.entry-workbench { display: grid; grid-template-columns: 304px minmax(0, 1fr); grid-template-rows: minmax(0, 1fr); height: 100%; font-family: var(--font); color: var(--text); background: var(--bg); font-size: var(--type-body-size); }
.entry-workbench.is-empty { grid-template-columns: minmax(0, 1fr); }
.entry-workbench.is-empty .entity-list { border-right: 0; }
.entity-list { border-right: 2px solid var(--rule); min-width: 0; overflow: auto; }
.entity-list-head { display: grid; gap: var(--space-3); padding: var(--space-5) var(--space-4) var(--space-3); }
h1, h2 { margin: 0; font-size: var(--type-page-size); font-weight: var(--type-page-weight); line-height: 1.05; letter-spacing: -0.045em; overflow-wrap: anywhere; }
.entity-search { display: flex; gap: var(--space-1); min-width: 0; }
.entity-search :deep(input) { flex: 1; min-width: 0; width: 100%; }
.entity-filters { display: flex; gap: var(--space-2); }
.entity-filters :deep(select) { min-width: 0; width: 50%; }
.merge-link { display: flex; justify-content: space-between; padding: var(--space-2) var(--space-4); font-weight: 700; background: var(--surface-soft); color: var(--text); text-decoration: underline; }
.overview-link { width: 100%; border: 0; font: inherit; font-weight: 700; text-align: left; cursor: pointer; }
.overview-link[aria-pressed="true"] { background: var(--text); color: var(--bg); }
.overview-link:disabled { cursor: not-allowed; opacity: 0.5; }
.merge-link:hover, .merge-link:focus-visible { background: var(--select); color: var(--on-select); }
.entity-items { list-style: none; margin: 0; padding: 0; border-top: 2px solid var(--rule); }
.entity-items button { display: grid; gap: var(--space-1); width: 100%; padding: var(--space-2) var(--space-4); border: 0; border-bottom: 1px solid var(--border); color: var(--text); background: var(--bg); font: inherit; text-align: left; cursor: pointer; overflow-wrap: anywhere; }
.entity-items small { color: var(--muted); font-size: var(--text-xs); }
.entity-items button[aria-current="true"] { color: var(--bg); background: var(--text); }
.entity-items button[aria-current="true"] small { color: var(--bg); opacity: .7; }
.entity-items button:hover:not([aria-current="true"]), .entity-items button:focus-visible:not([aria-current="true"]) { background: var(--select); color: var(--on-select); }
.entity-items button:hover:not([aria-current="true"]) small, .entity-items button:focus-visible:not([aria-current="true"]) small { color: var(--on-select); }
.entity-detail { display: flex; flex-direction: column; gap: var(--space-4); min-width: 0; overflow: auto; padding: var(--space-5) var(--space-6) var(--space-7); }
.entity-detail > * { flex-shrink: 0; }
.entity-eyebrow { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); font-size: var(--type-eyebrow-size); font-weight: var(--type-eyebrow-weight); color: var(--muted); overflow-wrap: anywhere; }
.entity-head, .entity-actions { display: flex; align-items: end; justify-content: space-between; flex-wrap: wrap; gap: var(--space-3); }
.entity-intro { max-width: 46em; font-size: var(--text-lg); line-height: 1.6; margin: 0; }
.entity-facts { display: grid; grid-template-columns: minmax(0, 2fr) repeat(2, minmax(70px, 1fr)); gap: var(--space-3); border-top: 2px solid var(--rule); padding-top: var(--space-3); margin: 0; }
.entity-facts dt { color: var(--muted); font-size: var(--type-eyebrow-size); font-weight: var(--type-eyebrow-weight); }
.entity-facts dd { margin: 0; font-weight: 700; overflow-wrap: anywhere; }
.mono { font-family: var(--mono); }
.entity-table-scroll { overflow-x: auto; margin-top: var(--space-3); }
.entity-articles table { width: 100%; min-width: 680px; border-collapse: collapse; font-size: var(--text-sm); }
.entity-articles th, .entity-articles td { padding: var(--space-3); border-bottom: 1px solid var(--border); text-align: left; vertical-align: top; overflow-wrap: anywhere; }
.entity-articles th { border-bottom: 2px solid var(--rule); font-size: var(--text-xs); }
.entity-articles td:first-child { min-width: 104px; white-space: nowrap; }
.entity-articles td:nth-child(3) { min-width: 104px; white-space: nowrap; }
.entity-articles td:nth-child(2) { min-width: 220px; }
.entity-articles td:last-child { min-width: 168px; white-space: nowrap; }
.entity-articles .mono { font-size: var(--text-xs); }
.entity-articles tr:hover, .entity-articles tr:focus-within { background: var(--select); color: var(--on-select); }
.entity-articles tr:hover .entity-link, .entity-articles tr:focus-within .entity-link { color: var(--on-select); }
.entity-link { padding: 0; border: 0; color: inherit; background: none; font: inherit; text-align: left; text-decoration: underline; cursor: pointer; }
a { color: inherit; text-decoration: underline; }
a:hover, .entity-link:hover { color: var(--accent); }
button:focus-visible, a:focus-visible, summary:focus-visible { outline: 2px solid currentColor; outline-offset: 3px; }
.entity-aliases h3 { margin-top: 0; }
.name-embedding-rebuild { margin-block: var(--space-5); border-top: 1px solid var(--border); padding-top: var(--space-3); }
.name-embedding-rebuild summary { cursor: pointer; }
.merge-form { display: grid; gap: var(--space-3); }
.run-error, .entity-detail :deep(.follow-error) { color: var(--danger); overflow-wrap: anywhere; }
@media (max-width: 900px) {
  .entry-workbench { grid-template-columns: minmax(0, 1fr); grid-template-rows: none; height: auto; min-height: 100%; }
  .entity-list, .entity-detail { overflow: visible; }
  .entity-list { border-right: 0; border-bottom: 2px solid var(--rule); }
  .entity-items { max-height: 240px; overflow: auto; }
  .entity-detail { padding: var(--space-4); }
}
</style>
