<template>
  <section class="entry-workbench">
    <header class="workbench-header">
      <div><h2>{{ t('knowledgeWorkbench.title') }}</h2><p>{{ t('knowledgeWorkbench.subtitle') }}</p></div>
      <t-button variant="outline" :loading="loading" @click="refreshAll"><template #icon><t-icon name="refresh" /></template>{{ t('knowledgeWorkbench.refresh') }}</t-button>
    </header>
    <LibraryScopeSelect v-model="libraryScope" single :disabled="merging" />
    <section class="name-embedding-rebuild" :aria-label="t('knowledgeWorkbench.nameEmbeddings.title')">
      <div class="name-embedding-rebuild__summary">
        <div><h3>{{ t('knowledgeWorkbench.nameEmbeddings.title') }}</h3><p role="status">{{ t('knowledgeWorkbench.nameEmbeddings.pending', { count: nameEmbeddingStatus?.pending_count ?? (nameEmbeddingStatusLoading ? t('common.loading') : t('knowledgeWorkbench.nameEmbeddings.unknown')) }, nameEmbeddingStatus?.pending_count ?? 0) }}</p></div>
        <div class="name-embedding-rebuild__actions">
          <t-button variant="text" :loading="nameEmbeddingStatusLoading" :disabled="nameEmbeddingRebuilding" @click="loadNameEmbeddingStatus">{{ t('knowledgeWorkbench.refresh') }}</t-button>
          <t-button variant="outline" :loading="nameEmbeddingRebuilding" :disabled="nameEmbeddingStatusLoading || !nameEmbeddingStatus?.pending_count || nameEmbeddingBusy" @click="rebuildNameEmbeddings">{{ t('knowledgeWorkbench.nameEmbeddings.rebuild') }}</t-button>
        </div>
      </div>
      <p v-if="nameEmbeddingStatus?.latest_run" class="name-embedding-rebuild__latest" role="status">{{ t('knowledgeWorkbench.nameEmbeddings.latest', { status: nameEmbeddingRunStatus, processed: nameEmbeddingStatus.latest_run.processed_count, total: nameEmbeddingStatus.latest_run.total_count }, nameEmbeddingStatus.latest_run.total_count) }} <span>{{ formatDate(nameEmbeddingStatus.latest_run.finished_at || nameEmbeddingStatus.latest_run.started_at || nameEmbeddingStatus.latest_run.created_at) }}</span></p>
      <p v-else-if="nameEmbeddingStatus" class="muted">{{ t('knowledgeWorkbench.nameEmbeddings.noRun') }}</p>
      <p v-if="nameEmbeddingStatus?.latest_run?.error" class="run-error" role="alert">{{ t('knowledgeWorkbench.nameEmbeddings.failure', { reason: nameEmbeddingStatus.latest_run.error }) }}</p>
      <p v-if="nameEmbeddingError" class="run-error" role="alert">{{ nameEmbeddingError }}</p>
    </section>

    <nav class="workbench-nav" :aria-label="t('knowledgeWorkbench.sections')">
      <button v-for="section in sections" :key="section" type="button" :class="['workbench-nav__item', { active: activeSection === section }]" :aria-pressed="activeSection === section" @click="activeSection = section">{{ t(`knowledgeWorkbench.nav.${section}`) }}</button>
    </nav>
    <t-alert v-if="loadError" theme="error" :message="loadError" role="alert"><template #operation><t-button variant="text" :loading="loading" @click="refreshAll">{{ t('common.retry') }}</t-button></template></t-alert>
    <section v-if="activeSection === 'entities'" class="workbench-panel">
      <div class="panel-heading"><div><h3>{{ t('knowledgeWorkbench.entities.title') }}</h3><p>{{ t('knowledgeWorkbench.entities.description') }}</p></div>
        <div class="entity-filters"><t-input v-model="entityFilter" clearable :placeholder="t('knowledgeWorkbench.entities.filter')" :aria-label="t('knowledgeWorkbench.entities.filter')" @compositionstart="entityComposing = true" @compositionend="endEntityComposition" @enter="onEntityEnter" /><t-select v-model="entityTypeFilter" clearable :options="entityTypeOptions" :placeholder="t('knowledgeWorkbench.entities.allTypes')" :aria-label="t('knowledgeWorkbench.entities.type')" /></div>
      </div>
      <t-loading :loading="entitiesLoading" show-overlay>
        <div class="entity-layout" :class="{ 'entity-layout--graph': relationView === 'graph' }">
          <div class="entity-list">
            <t-empty v-if="!entities.length && !loadError" :description="t(entityFilter || entityTypeFilter ? 'knowledgeWorkbench.entities.noResults' : 'knowledgeWorkbench.entities.empty')" />
            <button v-for="entity in entities" :key="entity.id" type="button" :class="['entity-list__item', { selected: selectedEntity?.id === entity.id }]" @click="selectEntity(entity)">
              <span class="entity-list__avatar">{{ entity.canonical_name.slice(0, 1) }}</span><span class="entity-list__copy"><strong>{{ entity.canonical_name }}</strong><small>{{ entityTypeLabel(entity.entity_type) }} · {{ entity.aliases?.length || 0 }} {{ t('knowledgeWorkbench.entities.aliases', entity.aliases?.length || 0) }}</small></span>
            </button>
            <t-button v-if="hasMoreEntities" block variant="text" :loading="entitiesLoading" @click="loadEntities(true)">{{ t('knowledgeWorkbench.loadMore') }}</t-button>
            <p v-if="entities.length" class="muted" role="status">{{ t('knowledgeWorkbench.entities.loaded', { count: entities.length }) }}</p>
          </div>
          <div class="entity-detail">
            <t-empty v-if="!selectedEntity" :description="t('knowledgeWorkbench.entities.choose')" />
            <section v-else>
              <div class="entity-detail__header"><div><span class="eyebrow">{{ entityTypeLabel(selectedEntity.entity_type) }}</span><h3>{{ selectedEntity.canonical_name }}</h3><p v-if="selectedEntity.description">{{ selectedEntity.description }}</p><h4>{{ t('knowledgeWorkbench.entities.aliasLabel') }}</h4><div v-if="selectedEntity.aliases?.length" class="chips"><span v-for="alias in selectedEntity.aliases" :key="alias" class="chip">{{ alias }}</span></div><p v-else class="muted">{{ t('knowledgeWorkbench.entities.noAliases') }}</p></div><div class="entity-detail__actions"><FollowToggle target-type="entity" :target-id="selectedEntity.id" :name="selectedEntity.canonical_name" /><t-button variant="outline" @click="openMerge">{{ t('knowledgeWorkbench.entities.merge') }}</t-button></div></div>

              <div class="relation-view-switch" role="group" :aria-label="t('entityGraph.view')">
                <button type="button" :aria-pressed="relationView === 'list'" @click="relationView = 'list'">{{ t('entityGraph.list') }}</button>
                <button type="button" :aria-pressed="relationView === 'graph'" @click="graphVisited = true; relationView = 'graph'">{{ t('entityGraph.graph') }}</button>
              </div>
              <EntityRelations v-if="relationView === 'list'" :entity-id="selectedEntity.id" @open-source="(id, location) => emit('open-source', id, location)" @open-entity="selectInitialEntity" />
              <EntityGraph v-if="graphVisited" v-show="relationView === 'graph'" :entity="selectedEntity" :active="relationView === 'graph'" @open-source="(id, location) => emit('open-source', id, location)" @open-entity="selectInitialEntity" @open-profile="id => { relationView = 'list'; void selectInitialEntity(id) }" />
              <t-alert v-if="entityDetailError" theme="error" :message="entityDetailError" role="alert"><template #operation><t-button variant="text" :loading="entityDetailLoading" @click="selectEntity(selectedEntity)">{{ t('knowledgeWorkbench.refresh') }}</t-button></template></t-alert>
              <t-loading :loading="entityDetailLoading" show-overlay><div class="entity-articles"><h4>{{ t('knowledgeWorkbench.entities.articles') }}</h4><a v-if="selectedEntity" :href="`/platform/entries?tab=documents&libraries=${encodeURIComponent(selectedEntity.library_id)}&entity_ids=${encodeURIComponent(selectedEntity.id)}`">{{ t('entryBatch.manageRelated') }}</a><t-empty v-if="!entityEntries.length && !entityDetailLoading && !entityDetailError" :description="t('knowledgeWorkbench.entities.noArticles')" /><section v-for="group in entityLibraryGroups" :key="group.id" class="entity-library-group"><h5>{{ group.name }} ({{ group.entries.length }})</h5><ul><li v-for="article in group.entries" :key="article.id"><t-button variant="text" @click="emit('open-source', article.id)">{{ article.title || article.file_name || t('knowledgeWorkbench.entities.untitledArticle') }}</t-button><small>{{ formatDate(article.created_at) }}</small></li></ul></section></div></t-loading>
            </section>
          </div>
        </div>
      </t-loading>
    </section>
    <MergeSuggestions v-else ref="mergeSuggestions" :library-id="selectedLibraryId" @open-source="emit('open-source', $event)" @changed="refreshAfterMerge" />
    <t-dialog v-model:visible="mergeVisible" :header="t('knowledgeWorkbench.entities.mergeTitle')" :footer="false" :close-on-overlay-click="false" :close-btn="!merging" :close-on-esc-keydown="!merging" width="min(520px, 95vw)">
      <div class="merge-form"><p>{{ t('knowledgeWorkbench.entities.mergeHint') }}</p><p>{{ t('knowledgeWorkbench.entities.mergeSource') }}: {{ selectedEntity?.canonical_name }}</p><label><span>{{ t('knowledgeWorkbench.entities.mergeTarget') }}</span><t-select v-model="mergeTargetId" filterable :options="mergeTargetOptions" :loading="mergeTargetsLoading" :disabled="merging" :placeholder="t('knowledgeWorkbench.entities.mergeTargetPlaceholder')" :aria-label="t('knowledgeWorkbench.entities.mergeTarget')" /></label><t-alert theme="warning" :message="t('knowledgeWorkbench.entities.mergeWarning')" /><p v-if="mergeError" class="run-error" role="alert">{{ mergeError }}</p><div class="merge-actions"><t-button variant="outline" :disabled="merging" @click="mergeVisible = false">{{ t('common.cancel') }}</t-button><t-button theme="primary" :loading="merging" :disabled="!mergeTargetId || mergeTargetsLoading" @click="confirmMerge">{{ t('knowledgeWorkbench.entities.merge') }}</t-button></div><t-button v-if="hasMoreMergeTargets" variant="text" :loading="mergeTargetsLoading" :disabled="merging" @click="loadMergeTargets(true)">{{ t('knowledgeWorkbench.loadMore') }}</t-button></div>
    </t-dialog>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { ENTITY_TYPES, getEntity, getEntityNameEmbeddingRebuildStatus, listEntities, mergeEntities, rebuildEntityNameEmbeddings, type Entity, type EntityEntry, type EntityNameEmbeddingRebuildStatus, type EntityType } from '@/api/entities'
import EntityRelations from './EntityRelations.vue'
import EntityGraph from './EntityGraph.vue'
import type { SourceLocation } from '@/types/search'
import MergeSuggestions from './MergeSuggestions.vue'
import FollowToggle from '@/components/FollowToggle.vue'
import LibraryScopeSelect from '@/components/LibraryScopeSelect.vue'
import { useFollowStore } from '@/stores/follows'

const props = withDefaults(defineProps<{ initialEntityId?: string; libraryId?: string }>(), { initialEntityId: '', libraryId: '' })
const emit = defineEmits<{ 'open-source': [entryId: string, location?: SourceLocation] }>()
const { t, locale } = useI18n()
const followStore = useFollowStore()
const libraryScope = ref<string[]>(props.libraryId ? [props.libraryId] : [])
const selectedLibraryId = computed(() => libraryScope.value[0] || '')
const pageSize = 50
const sections = ['entities', 'merges'] as const
const activeSection = ref<typeof sections[number]>('entities')
const relationView = ref<'list' | 'graph'>('list'), graphVisited = ref(false)
const entityFilter = ref(''), entityTypeFilter = ref<EntityType | ''>(''), entityComposing = ref(false)
const entityTypeOptions = computed(() => ENTITY_TYPES.map(value => ({ value, label: entityTypeLabel(value) })))
const entities = ref<Entity[]>([]), entitiesLoading = ref(false), hasMoreEntities = ref(false), loadError = ref('')
const selectedEntity = ref<Entity | null>(null), entityEntries = ref<EntityEntry[]>([]), entityDetailLoading = ref(false), entityDetailError = ref('')
const entityLibraryGroups = computed(() => {
  const groups = new Map<string, { id: string; name: string; entries: EntityEntry[] }>()
  for (const entry of entityEntries.value) {
    if (!groups.has(entry.library_id)) groups.set(entry.library_id, { id: entry.library_id, name: entry.library_name || entry.library_id, entries: [] })
    groups.get(entry.library_id)!.entries.push(entry)
  }
  return [...groups.values()]
})
const loading = ref(false), mergeVisible = ref(false), mergeTargetId = ref(''), merging = ref(false), mergeError = ref('')
const mergeTargets = ref<Entity[]>([]), mergeTargetsLoading = ref(false), hasMoreMergeTargets = ref(false)
const mergeTargetOptions = computed(() => mergeTargets.value.filter(item => item.id !== selectedEntity.value?.id && !item.merged_into_id).map(item => ({ value: item.id, label: `${item.canonical_name} · ${entityTypeLabel(item.entity_type)}` })))
const mergeSuggestions = ref<InstanceType<typeof MergeSuggestions>>()
let entityRequest = 0, detailRequest = 0, mergeTargetRequest = 0
let entitySearchTimer: ReturnType<typeof setTimeout> | null = null
function entityTypeLabel(value: string) { return t(`knowledgeWorkbench.entities.types.${ENTITY_TYPES.includes(value as EntityType) ? value : 'other'}`) }
function formatDate(value?: string) { return value ? new Date(value).toLocaleString(locale.value) : t('knowledgeWorkbench.unknownDate') }
function envelopeData<T>(response: { success: boolean; data: T; error?: string }): T { if (!response.success) throw new Error(response.error || t('knowledgeWorkbench.requestFailed')); return response.data }
function errorMessage(cause: unknown) { return cause instanceof Error ? cause.message : t('knowledgeWorkbench.requestFailed') }
async function loadEntities(more = false) {
  if (more && entitiesLoading.value) return
  const current = ++entityRequest
  if (!selectedLibraryId.value) { entities.value = []; hasMoreEntities.value = false; entitiesLoading.value = false; return }
  entitiesLoading.value = true; loadError.value = ''
  try { const page = envelopeData(await listEntities({ library_id: selectedLibraryId.value, entity: entityFilter.value.trim(), entity_type: entityTypeFilter.value || undefined, limit: pageSize, offset: more ? entities.value.length : 0 })) || []; if (current === entityRequest) { entities.value = more ? [...entities.value, ...page] : page; hasMoreEntities.value = page.length === pageSize } }
  catch (cause) { if (current === entityRequest) loadError.value = errorMessage(cause) }
  finally { if (current === entityRequest) entitiesLoading.value = false }
}
async function selectEntity(entity: Entity) {
  const current = ++detailRequest
  selectedEntity.value = entity; entityEntries.value = []; entityDetailError.value = ''; entityDetailLoading.value = true
  try { const result = envelopeData(await getEntity(entity.id)); if (current === detailRequest) { libraryScope.value = [result.entity.library_id]; selectedEntity.value = result.entity; entityEntries.value = result.entries || [] } }
  catch (cause) { if (current === detailRequest) entityDetailError.value = errorMessage(cause) }
  finally { if (current === detailRequest) entityDetailLoading.value = false }
}
function resetEntityList() { entityRequest++; detailRequest++; entities.value = []; selectedEntity.value = null; entityEntries.value = []; hasMoreEntities.value = false; entityDetailLoading.value = false; entityDetailError.value = ''; void loadEntities() }
function scheduleEntitySearch() { if (entitySearchTimer) clearTimeout(entitySearchTimer); entityRequest++; if (entityComposing.value) return; if (!entityFilter.value.trim()) { resetEntityList(); return }; entitySearchTimer = setTimeout(() => { entitySearchTimer = null; resetEntityList() }, 300) }
function endEntityComposition() { entityComposing.value = false; scheduleEntitySearch() }
function onEntityEnter(_value: unknown, options?: { e?: KeyboardEvent }) { if (entityComposing.value || (options?.e?.isComposing || options?.e?.keyCode === 229)) return; if (entitySearchTimer) clearTimeout(entitySearchTimer); entitySearchTimer = null; resetEntityList() }
async function refreshAfterMerge() { selectedEntity.value = null; entityEntries.value = []; detailRequest++; await followStore.refreshAfterTargetChange(); await loadEntities(); await loadNameEmbeddingStatus() }
async function refreshAll() { loading.value = true; try { await Promise.all([loadEntities(), loadNameEmbeddingStatus(), selectedEntity.value ? selectEntity(selectedEntity.value) : Promise.resolve(), mergeSuggestions.value?.refresh()]) } finally { loading.value = false } }
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
  merging.value = true; mergeError.value = ''
  try { const response = await mergeEntities({ keep_entity_id: mergeTargetId.value, merge_entity_id: selectedEntity.value.id }); if (!response.success) throw new Error(response.error || t('knowledgeWorkbench.requestFailed')); mergeVisible.value = false; MessagePlugin.success(t('knowledgeWorkbench.entities.mergeSaved')); await refreshAfterMerge(); await mergeSuggestions.value?.refresh() }
  catch (cause) { mergeError.value = errorMessage(cause) }
  finally { merging.value = false }
}
const nameEmbeddingStatus = ref<EntityNameEmbeddingRebuildStatus | null>(null), nameEmbeddingStatusLoading = ref(false), nameEmbeddingRebuilding = ref(false), nameEmbeddingError = ref('')
const nameEmbeddingBusy = computed(() => ['pending', 'running'].includes(nameEmbeddingStatus.value?.latest_run?.status || ''))
const nameEmbeddingRunStatus = computed(() => { const status = nameEmbeddingStatus.value?.latest_run?.status || ''; return ['pending', 'running', 'completed', 'failed'].includes(status) ? t(`knowledgeWorkbench.nameEmbeddings.status.${status}`) : status })
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
  try { const result = envelopeData(await rebuildEntityNameEmbeddings()); if (current === nameEmbeddingRequest) { nameEmbeddingStatus.value = result; MessagePlugin.success(t('knowledgeWorkbench.nameEmbeddings.queued')); scheduleNameEmbeddingPoll() } }
  catch (cause) { if (current === nameEmbeddingRequest) nameEmbeddingError.value = errorMessage(cause) }
  finally { if (current === nameEmbeddingRequest) nameEmbeddingRebuilding.value = false }
}
watch(selectedLibraryId, () => { mergeTargetRequest++; mergeVisible.value = false; mergeTargets.value = []; resetEntityList() }, { flush: 'sync' })
watch(entityFilter, scheduleEntitySearch)
watch(entityTypeFilter, resetEntityList)
async function selectInitialEntity(id: string) {
  if (!id) return;
  activeSection.value = 'entities';
  const current = ++detailRequest;
  entityEntries.value = []; entityDetailError.value = ''; entityDetailLoading.value = true;
  try { const result = envelopeData(await getEntity(id)); if (current === detailRequest) { libraryScope.value = [result.entity.library_id]; selectedEntity.value = result.entity; entityEntries.value = result.entries || [] } }
  catch (cause) { if (current === detailRequest) entityDetailError.value = errorMessage(cause) }
  finally { if (current === detailRequest) entityDetailLoading.value = false }
}
watch(() => props.initialEntityId, id => { void selectInitialEntity(id) }, { immediate: true })
onMounted(() => { void refreshAll() })
onBeforeUnmount(() => { entityRequest++; detailRequest++; mergeTargetRequest++; nameEmbeddingRequest++; clearNameEmbeddingPoll(); if (entitySearchTimer) clearTimeout(entitySearchTimer) })
</script>
<style scoped lang="less">
.entry-workbench { padding: 24px; color: var(--td-text-color-primary); }
.workbench-header, .panel-heading, .name-embedding-rebuild__summary, .entity-detail__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; }
.workbench-header h2 { font-size: 20px; margin: 0 0 8px; }
.workbench-header p, .panel-heading p, .name-embedding-rebuild p { color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.6; }
.name-embedding-rebuild { margin: 20px 0; padding: 16px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); }
.name-embedding-rebuild h3 { margin: 0; font-size: 15px; }
.name-embedding-rebuild__actions, .merge-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.name-embedding-rebuild__latest { margin-bottom: 0; }
.merge-form label { display: flex; flex-direction: column; gap: 8px; min-width: 220px; font-size: 13px; }
.workbench-nav { display: flex; gap: 8px; border-bottom: 1px solid var(--td-component-stroke); margin: 20px 0; }
.workbench-nav__item { background: none; border: 0; padding: 12px 16px; cursor: pointer; color: var(--td-text-color-secondary); border-bottom: 2px solid transparent; }
.workbench-nav__item:hover, .workbench-nav__item.active { color: var(--td-brand-color); background: var(--td-bg-color-container-hover); }
.workbench-nav__item.active { border-bottom-color: var(--td-brand-color); }
.workbench-nav__item:focus-visible, .entity-list__item:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.workbench-panel { margin-top: 20px; }
.panel-heading { margin-bottom: 20px; }
.panel-heading h3 { margin: 0; font-size: 16px; }
.entity-filters { display: flex; gap: 12px; width: min(480px, 100%); }
.entity-filters > :deep(*) { min-width: 0; }
.entity-layout { display: grid; grid-template-columns: minmax(220px, 1fr) minmax(0, 2fr); gap: 24px; min-height: 300px; }
.entity-layout--graph { grid-template-columns: minmax(180px, .7fr) minmax(0, 2.3fr); }
.relation-view-switch { display: flex; gap: 20px; margin-top: 20px; border-bottom: 1px solid var(--td-component-stroke); }
.relation-view-switch button { padding: 10px 4px; background: transparent; border: 0; border-bottom: 2px solid transparent; color: var(--td-text-color-secondary); font: inherit; font-size: 13px; cursor: pointer; }
.relation-view-switch button:hover { background: var(--td-bg-color-container-hover); color: var(--td-brand-color); }
.relation-view-switch button[aria-pressed="true"] { color: var(--td-brand-color); border-bottom-color: var(--td-brand-color); }
.relation-view-switch button:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.entity-list { min-width: 0; }
.entity-list__item { display: flex; align-items: center; gap: 12px; width: 100%; padding: 12px; border: 1px solid transparent; background: transparent; border-radius: var(--td-radius-medium); text-align: left; color: var(--td-text-color-primary); cursor: pointer; }
.entity-list__item:hover { background: var(--td-bg-color-container-hover); }
.entity-list__item.selected { background: var(--td-brand-color-light); border-color: var(--td-brand-color); }
.entity-list__avatar { display: grid; place-items: center; min-width: 36px; height: 36px; background: var(--td-bg-color-secondarycontainer); border-radius: var(--td-radius-medium); }
.entity-list__copy { display: flex; flex-direction: column; gap: 6px; min-width: 0; overflow-wrap: anywhere; }
.entity-list__copy small, .entity-articles small, .muted { color: var(--td-text-color-secondary); font-size: 12px; line-height: 1.7; }
.entity-detail { min-width: 0; padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); }
.entity-detail__header h3 { margin: 8px 0; }
.entity-detail__header > div { min-width: 0; overflow-wrap: anywhere; }
.entity-detail__actions { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; flex-shrink: 0; }
.eyebrow { color: var(--td-text-color-secondary); font-size: 12px; }
.chips { display: flex; flex-wrap: wrap; gap: 8px; }
.chip { padding: 4px 8px; background: var(--td-bg-color-secondarycontainer); border-radius: var(--td-radius-small); font-size: 12px; overflow-wrap: anywhere; }
.entity-articles { min-height: 180px; }
.entity-articles ul { list-style: none; padding: 0; }
.entity-articles li { display: flex; flex-direction: column; align-items: flex-start; margin: 12px 0; min-width: 0; }
.entity-articles :deep(.t-button) { height: auto; max-width: 100%; white-space: normal; text-align: left; }
.entity-articles :deep(.t-button__text) { white-space: normal; overflow-wrap: anywhere; }
.merge-form { display: flex; flex-direction: column; gap: 16px; }
.merge-actions { justify-content: flex-end; }
.run-error { color: var(--td-error-color); font-size: 13px; overflow-wrap: anywhere; }
@media (max-width: 720px) { .entry-workbench { padding: 16px; } .panel-heading, .name-embedding-rebuild__summary, .entity-detail__header { flex-direction: column; } .entity-layout { grid-template-columns: minmax(0, 1fr); } .entity-filters { flex-direction: column; } }
</style>
