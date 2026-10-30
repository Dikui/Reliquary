<script setup lang="ts">
import { formatDateTime } from '@/utils/dateTime'
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Button, Dialog, EmptyState, ErrorState, Field, Input, LibMark, Select, Tag, Textarea } from '@/components/ui'
import { confirm, toast, type ConfirmHandle } from '@/components/ui/feedback'
import FollowToggle from '@/components/FollowToggle.vue'
import { CONTENT_TYPES, type ContentType } from '@/types/entryClassification'
import { useFollowStore } from '@/stores/follows'
import { listLibraries, listEntryFiles, type LibraryCatalogItem } from '@/api/library'
import { createTopic, deleteTopic, getTopicBackfill, listTopics, retryTopicBackfill, updateTopic, type Topic, type TopicBackfill } from '@/api/topics'

interface TopicEntry { id: string; title?: string; file_name?: string; library_id: string; content_type?: string; created_at?: string }
const { t, locale } = useI18n()
const route = useRoute(), router = useRouter(), followStore = useFollowStore()
const queryValue = (value: unknown) => typeof value === 'string' ? value : ''
const libraryId = computed(() => queryValue(route.query.libraries).split(',')[0] || '')
const libraries = ref<LibraryCatalogItem[]>([]), libraryError = ref(false), catalogLoaded = ref(false)
const topics = ref<Topic[]>([]), loading = ref(false), failedLibraries = ref<string[]>([])
const selectedId = ref(''), selected = computed(() => topics.value.find(topic => topic.id === selectedId.value))
const progress = reactive<Record<string, TopicBackfill | null>>({}), progressErrors = reactive<Record<string, string>>({})
const retrying = reactive<Record<string, boolean>>({})
const selectedProgress = computed(() => progress[selectedId.value])
const failedBatches = computed(() => selectedProgress.value?.batches?.filter(batch => batch.status === 'failed') || [])
const entries = ref<TopicEntry[]>([]), entryTotal = ref(0), entryPage = ref(1), entriesLoading = ref(false), entriesError = ref(false)
const pageSize = 20, pageCount = computed(() => Math.max(1, Math.ceil(entryTotal.value / pageSize)))
const editorVisible = ref(false), editing = ref<Topic | null>(null), draft = ref({ name: '', description: '', libraryId: '' })
const saving = ref(false), deleting = ref(false), saveError = ref(''), nameError = ref(''), scopeError = ref('')
const nameInput = ref<InstanceType<typeof Input>>()
let generation = 0, entryGeneration = 0, catalogGeneration = 0, disposed = false
let pollTimer: ReturnType<typeof setTimeout> | undefined, confirmation: ConfirmHandle | undefined
const progressSequence: Record<string, number> = {}
const active = (run?: TopicBackfill | null) => !!run && (run.status === 'pending' || run.status === 'running')
const library = (id: string) => libraries.value.find(item => item.id === id)
const libraryName = (id: string) => library(id)?.display_name || library(id)?.name || id
function errorCode(cause: unknown) {
  const error = cause as { status?: number; code?: number; error?: { code?: number } } | undefined
  return error?.status || error?.error?.code || error?.code
}
const missing = (cause: unknown) => [404, 1003].includes(Number(errorCode(cause)))
const conflict = (cause: unknown) => [409, 1005].includes(Number(errorCode(cause)))
function statusLabel(run?: TopicBackfill | null) {
  if (!run) return t('topicManagement.progressUnavailable')
  switch (run.status) {
    case 'pending': return t('topicManagement.status.pending')
    case 'running': return t('topicManagement.status.running')
    case 'completed': return t('topicManagement.status.completed')
    case 'failed': return t('topicManagement.status.failed')
    case 'cancelled': return t('topicManagement.status.cancelled')
  }
}
const percentage = (run: TopicBackfill) => run.total > 0 ? Math.min(100, Math.max(0, run.processed / run.total * 100)) : 0
const successPercentage = (run: TopicBackfill) => run.total > 0 ? Math.min(percentage(run), Math.max(0, (run.processed - run.failed_entries) / run.total * 100)) : 0
function contentTypeName(value?: string) {
  if (!value) return t('entityPage.unknownType')
  return CONTENT_TYPES.includes(value as ContentType) ? t(`library.contentTypes.${value}`) : value
}
function collected(value?: string) {
  return formatDateTime(value, locale.value, { dateStyle: 'medium', timeStyle: 'short' }, t('workbench.unknownDate'))
}
function setLibrary(value: string) {
  if (saving.value || deleting.value || editorVisible.value) return
  const query = { ...route.query }
  delete query.topic_id
  if (value) query.libraries = value
  else delete query.libraries
  void router.replace({ query })
}
function selectTopic(id: string) {
  selectedId.value = id
  return router.replace({ query: { ...route.query, topic_id: id } })
}
async function loadLibraries() {
  const current = ++catalogGeneration
  loading.value = true; libraryError.value = false
  try {
    const response = await listLibraries()
    if (!response.success) throw response
    if (disposed || current !== catalogGeneration) return
    libraries.value = response.data || []; catalogLoaded.value = true
    await load()
  } catch {
    if (!disposed && current === catalogGeneration) { libraryError.value = true; loading.value = false }
  }
}
async function fetchTopics(current: number) {
  const ids = libraryId.value ? [libraryId.value] : libraries.value.map(item => item.id)
  const results = await Promise.allSettled(ids.map(async id => {
    const response = await listTopics(id)
    if (!response.success) throw response
    return response.data || []
  }))
  if (disposed || current !== generation) return false
  const previous = topics.value
  failedLibraries.value = ids.filter((_, index) => results[index]?.status === 'rejected')
  topics.value = results.flatMap((result, index) => result.status === 'fulfilled' ? result.value : previous.filter(topic => topic.library_id === ids[index]))
  const wanted = queryValue(route.query.topic_id) || selectedId.value
  selectedId.value = topics.value.some(topic => topic.id === wanted) ? wanted : topics.value[0]?.id || ''
  return true
}
async function readProgress(id: string, current = generation) {
  const sequence = progressSequence[id] = (progressSequence[id] || 0) + 1
  try {
    const response = await getTopicBackfill(id)
    if (!response.success) throw response
    if (!disposed && current === generation && sequence === progressSequence[id]) { progress[id] = response.data; delete progressErrors[id] }
  } catch (cause) {
    if (!disposed && current === generation && sequence === progressSequence[id]) progressErrors[id] = missing(cause) ? t('topicManagement.notFound') : t('topicManagement.progressFailed')
  }
}
function schedulePoll() {
  clearTimeout(pollTimer)
  if (disposed || !topics.value.some(topic => active(progress[topic.id]))) return
  pollTimer = setTimeout(async () => {
    const current = generation
    const selectedWasActive = active(selectedProgress.value)
    const beforeCount = selected.value?.entry_count
    await Promise.all(topics.value.filter(topic => active(progress[topic.id])).map(topic => readProgress(topic.id, current)))
    if (!await fetchTopics(current)) return
    if (selectedWasActive && (!active(selectedProgress.value) || beforeCount !== selected.value?.entry_count)) void loadEntries()
    schedulePoll()
  }, 2000)
}
async function load() {
  const current = ++generation
  clearTimeout(pollTimer)
  loading.value = true
  try {
    if (!await fetchTopics(current)) return
    await Promise.all(topics.value.map(topic => readProgress(topic.id, current)))
    if (!disposed && current === generation) schedulePoll()
  } finally { if (!disposed && current === generation) loading.value = false }
}
async function loadEntries() {
  const current = ++entryGeneration, topic = selected.value
  if (!topic) { entries.value = []; entryTotal.value = 0; entriesLoading.value = false; return }
  entriesLoading.value = true; entriesError.value = false
  try {
    const response = await listEntryFiles(topic.library_id, { page: entryPage.value, page_size: pageSize, libraries: [topic.library_id], topic_ids: [topic.id] })
    if (!response.success || !Array.isArray(response.data)) throw response
    if (disposed || current !== entryGeneration) return
    entryTotal.value = response.total || 0
    if (entryPage.value > pageCount.value) { entryPage.value = pageCount.value; return }
    entries.value = response.data
  } catch { if (!disposed && current === entryGeneration) entriesError.value = true }
  finally { if (!disposed && current === entryGeneration) entriesLoading.value = false }
}
function openEditor(topic?: Topic) {
  editing.value = topic || null
  draft.value = { name: topic?.name || '', description: topic?.description || '', libraryId: topic?.library_id || libraryId.value || '' }
  saveError.value = ''; nameError.value = ''; scopeError.value = ''; editorVisible.value = true
}
function closeEditor(value: boolean) { if (!saving.value) editorVisible.value = value }
async function save() {
  if (saving.value) return
  const input = { name: draft.value.name.trim(), description: draft.value.description.trim() }
  nameError.value = input.name ? '' : t('topicManagement.nameRequired')
  scopeError.value = libraries.value.some(item => item.id === draft.value.libraryId) ? '' : t('topicManagement.libraryRequired')
  if (nameError.value || scopeError.value) { if (nameError.value) nameInput.value?.focus(); return }
  const current = generation, target = editing.value
  saving.value = true; saveError.value = ''
  try {
    const response = target ? await updateTopic(target.id, input) : await createTopic({ ...input, library_id: draft.value.libraryId })
    if (!response.success) throw response
    if (disposed || current !== generation) return
    editorVisible.value = false
    toast.success(t(target ? 'topicManagement.updated' : 'topicManagement.created'))
    if (!target && libraryId.value && libraryId.value !== draft.value.libraryId) {
      await router.replace({ query: { ...route.query, libraries: draft.value.libraryId, topic_id: response.data.id } })
      return
    }
    if (response.data?.id) await selectTopic(response.data.id)
    await load(); await loadEntries()
  } catch (cause) {
    if (!disposed && current === generation) {
      if (conflict(cause)) { nameError.value = t('topicManagement.nameExists'); nameInput.value?.focus() }
      else saveError.value = missing(cause) ? t('topicManagement.notFound') : t('topicManagement.saveFailed')
    }
  } finally { saving.value = false }
}
function requestDelete(topic: Topic) {
  if (deleting.value) return
  confirmation?.destroy()
  const description = t('topicManagement.deleteDescription', { name: topic.name })
  confirmation = confirm({
    header: t('topicManagement.deleteTitle'), body: description,
    confirmBtn: t('common.delete'), cancelBtn: t('common.cancel'), theme: 'danger',
    onConfirm: async () => {
      deleting.value = true
      try {
        const response = await deleteTopic(topic.id)
        if (!response.success) throw response
        if (disposed) return
        topics.value = topics.value.filter(item => item.id !== topic.id)
        delete progress[topic.id]; delete progressErrors[topic.id]
        toast.success(t('topicManagement.deleted'))
        await followStore.refreshAfterTargetChange()
        await load()
        if (!disposed && route.query.topic_id === topic.id) {
          const query = { ...route.query }
          if (selectedId.value) query.topic_id = selectedId.value
          else delete query.topic_id
          await router.replace({ query })
        }
      } catch (cause) {
        const message = missing(cause) ? t('topicManagement.notFound') : t('topicManagement.deleteFailed')
        confirmation?.update({ body: `${description}\n\n${message}` })
        throw new Error(message)
      }
      finally { deleting.value = false }
    },
  })
}
async function retry(id: string) {
  if (retrying[id] || active(progress[id]) || progress[id]?.status === 'cancelled') return
  const current = generation
  retrying[id] = true; delete progressErrors[id]
  progressSequence[id] = (progressSequence[id] || 0) + 1
  try {
    const response = await retryTopicBackfill(id)
    if (!response.success) throw response
    if (!disposed && current === generation) { progress[id] = response.data; schedulePoll() }
  } catch (cause) {
    if (!disposed && current === generation) progressErrors[id] = missing(cause) ? t('topicManagement.notFound') : conflict(cause) ? t('topicManagement.retryConflict') : t('topicManagement.retryFailed')
  } finally { retrying[id] = false }
}
watch(libraryId, () => { selectedId.value = ''; topics.value = []; editorVisible.value = false; confirmation?.destroy(); if (catalogLoaded.value) void load() })
watch(() => queryValue(route.query.topic_id), id => { if (topics.value.some(topic => topic.id === id)) selectedId.value = id })
watch(selectedId, () => { entryPage.value = 1; entries.value = []; entryTotal.value = 0; void loadEntries() })
watch(entryPage, () => { void loadEntries() })
onMounted(loadLibraries)
onUnmounted(() => { disposed = true; generation++; entryGeneration++; catalogGeneration++; clearTimeout(pollTimer); confirmation?.destroy() })
</script>

<template>
  <main class="topic-page" :class="{ 'is-empty': !selected }">
    <aside class="topic-sidebar" :aria-label="t('topicManagement.title')">
      <header class="topic-list-head">
        <h1>{{ t('topicManagement.title') }}</h1>
        <Select :model-value="libraryId" :aria-label="t('topicManagement.libraries')" :disabled="saving || deleting || editorVisible || !catalogLoaded" @update:model-value="setLibrary">
          <option value="">{{ t('topicManagement.allLibraries') }}</option>
          <option v-for="item in libraries" :key="item.id" :value="item.id">{{ item.display_name || item.name }}</option>
        </Select>
        <Button variant="primary" :disabled="!libraries.length || libraryError" @click="openEditor()">{{ t('topicManagement.create') }}</Button>
      </header>
      <ErrorState v-if="libraryError" :title="t('topicManagement.libraryLoadFailed')"><Button @click="loadLibraries">{{ t('common.retry') }}</Button></ErrorState>
      <div v-if="failedLibraries.length" class="topic-list-error" role="alert">
        <p>{{ t('topicManagement.loadFailed') }} {{ failedLibraries.map(libraryName).join('、') }}</p><Button @click="load">{{ t('common.retry') }}</Button>
      </div>
      <p v-if="loading" class="topic-list-status" role="status">{{ t('common.loading') }}</p>
      <ul v-if="topics.length" class="topic-list" :aria-busy="loading">
        <li v-for="topic in topics" :key="topic.id">
          <button type="button" :aria-current="selectedId === topic.id ? 'true' : undefined" @click="selectTopic(topic.id)">
            <strong>{{ topic.name }}</strong><span class="mono">{{ topic.entry_count.toLocaleString(locale) }}</span>
            <small v-if="!libraryId" class="topic-library"><LibMark v-if="library(topic.library_id)" :color="library(topic.library_id)!.color" />{{ libraryName(topic.library_id) }}</small>
            <small :class="{ 'is-failed': progress[topic.id]?.failed_batches || progressErrors[topic.id] }">{{ progressErrors[topic.id] || statusLabel(progress[topic.id]) }}<template v-if="active(progress[topic.id])"> · {{ Math.round(percentage(progress[topic.id]!)) }}%</template></small>
          </button>
        </li>
      </ul>
      <EmptyState v-else-if="!loading && !libraryError && !failedLibraries.length" :title="t(libraries.length ? 'topicManagement.emptyTitle' : 'topicManagement.noLibraries')" :description="t(libraries.length ? 'topicManagement.empty' : 'topicManagement.noLibrariesHint')" />
    </aside>

    <section v-if="selected" class="topic-detail" :aria-label="selected.name">
      <div class="topic-scope"><span>{{ t('topicManagement.libraries') }}:</span><LibMark v-if="library(selected.library_id)" :color="library(selected.library_id)!.color" />{{ libraryName(selected.library_id) }}</div>
      <header class="topic-detail-head">
        <h2>{{ selected.name }}</h2>
        <div class="topic-actions"><FollowToggle :key="selected.id" target-type="topic" :target-id="selected.id" :name="selected.name" /><Button :disabled="deleting" @click="openEditor(selected)">{{ t('topicManagement.edit') }}</Button><Button variant="danger" :disabled="deleting" @click="requestDelete(selected)">{{ t('common.delete') }}</Button></div>
      </header>
      <p class="topic-description">{{ selected.description || t('topicManagement.noDescription') }}</p>
      <section class="topic-progress" :aria-label="t('topicManagement.historyProgress')">
        <template v-if="selectedProgress">
          <div class="topic-progress-row" aria-live="polite"><strong>{{ statusLabel(selectedProgress) }}</strong><span class="mono">{{ t('topicManagement.progress', { processed: selectedProgress.processed, total: selectedProgress.total }) }}</span></div>
          <div v-if="selectedProgress.total > 0" class="topic-progress-bar" role="progressbar" :aria-label="t('topicManagement.historyProgress')" :aria-valuenow="percentage(selectedProgress)" aria-valuemin="0" aria-valuemax="100" :aria-valuetext="t('topicManagement.progress', { processed: selectedProgress.processed, total: selectedProgress.total })"><i :style="{ width: `${successPercentage(selectedProgress)}%` }" /><i class="failed" :style="{ width: `${percentage(selectedProgress) - successPercentage(selectedProgress)}%` }" /></div>
          <div class="topic-progress-row topic-hint"><span>{{ t('topicManagement.assignmentHint') }}</span><span v-if="selectedProgress.failed_batches">{{ t('topicManagement.failures', { articles: selectedProgress.failed_entries, batches: selectedProgress.failed_batches }) }}</span></div>
          <ul v-if="failedBatches.length" class="topic-failures"><li v-for="batch in failedBatches" :key="batch.id">{{ t('topicManagement.batchFailure', { ordinal: batch.ordinal, count: batch.entry_count, attempts: batch.attempts, reason: batch.error || t('common.error') }) }}</li></ul>
          <Button v-if="selectedProgress.failed_batches > 0" :loading="retrying[selected.id]" :disabled="active(selectedProgress) || selectedProgress.status === 'cancelled'" @click="retry(selected.id)">{{ t('topicManagement.retryFailedBatches') }}</Button>
        </template>
        <p v-else-if="!progressErrors[selected.id]" class="topic-hint">{{ t('topicManagement.progressUnavailable') }}</p>
        <div v-if="progressErrors[selected.id]" class="topic-inline-error" role="alert">{{ progressErrors[selected.id] }} <Button @click="readProgress(selected.id).then(schedulePoll)">{{ t('common.retry') }}</Button></div>
      </section>

      <section class="topic-materials" :aria-busy="entriesLoading">
        <header class="topic-materials-head"><h3>{{ t('topicManagement.articleCount', { count: entryTotal }) }}</h3><a class="topic-link" :href="`/platform/entries?tab=documents&libraries=${encodeURIComponent(selected.library_id)}&topic_ids=${encodeURIComponent(selected.id)}`">{{ t('entryBatch.manageRelated') }}</a></header>
        <div v-if="entriesError" class="topic-inline-error" role="alert">{{ t('topicManagement.entriesFailed') }} <Button @click="loadEntries">{{ t('common.retry') }}</Button></div>
        <p v-if="entriesLoading" role="status">{{ t('common.loading') }}</p>
        <div v-if="entries.length" class="topic-table-scroll">
          <table :aria-label="t('topicManagement.materials')"><thead><tr><th scope="col">{{ t('topicManagement.entryId') }}</th><th scope="col">{{ t('topicManagement.entryTitle') }}</th><th scope="col">{{ t('topicManagement.libraries') }}</th><th scope="col">{{ t('topicManagement.contentType') }}</th><th scope="col">{{ t('topicManagement.collectedAt') }}</th></tr></thead>
            <tbody><tr v-for="entry in entries" :key="entry.id"><td class="mono" :title="entry.id">{{ entry.id.slice(0, 8) }}</td><td><a class="topic-link" :href="`/platform/entries/${encodeURIComponent(entry.id)}`">{{ entry.title || entry.file_name || t('library.untitledDocument') }}</a></td><td><span class="topic-scope"><LibMark v-if="library(entry.library_id)" :color="library(entry.library_id)!.color" />{{ libraryName(entry.library_id) }}</span></td><td><Tag>{{ contentTypeName(entry.content_type) }}</Tag></td><td class="mono">{{ collected(entry.created_at) }}</td></tr></tbody>
          </table>
        </div>
        <EmptyState v-else-if="!entriesLoading && !entriesError" :title="t('topicManagement.noEntries')" />
        <nav v-if="pageCount > 1" class="topic-pagination" :aria-label="t('topicManagement.pagination')"><Button :disabled="entryPage <= 1 || entriesLoading" @click="entryPage--">{{ t('topicManagement.previous') }}</Button><span class="mono">{{ t('topicManagement.page', { page: entryPage, total: pageCount }) }}</span><Button :disabled="entryPage >= pageCount || entriesLoading" @click="entryPage++">{{ t('topicManagement.next') }}</Button></nav>
      </section>
    </section>

    <Dialog :model-value="editorVisible" :title="t(editing ? 'topicManagement.edit' : 'topicManagement.create')" :closable="!saving" :close-on-overlay-click="false" :close-on-esc-keydown="!saving" @update:model-value="closeEditor">
      <form id="topic-editor" class="topic-form" novalidate @submit.prevent="save">
        <Field :label="t('topicManagement.name')" :error="nameError"><Input ref="nameInput" v-model="draft.name" :maxlength="255" :disabled="saving" /></Field>
        <Field :label="t('topicManagement.topicDescription')" :hint="t('topicManagement.descriptionHint')"><Textarea v-model="draft.description" :maxlength="2000" rows="4" :disabled="saving" /></Field>
        <Field :label="t('topicManagement.libraries')" :hint="t('topicManagement.libraryHint')" :error="scopeError"><Select v-model="draft.libraryId" :disabled="saving || !!editing"><option disabled value="">{{ t('topicManagement.chooseLibrary') }}</option><option v-for="item in libraries" :key="item.id" :value="item.id">{{ item.display_name || item.name }}</option></Select></Field>
        <p v-if="saveError" class="topic-inline-error" role="alert">{{ saveError }}</p>
      </form>
      <template #footer><Button :disabled="saving" @click="closeEditor(false)">{{ t('common.cancel') }}</Button><Button type="submit" form="topic-editor" variant="primary" :loading="saving">{{ t(editing ? 'common.save' : 'topicManagement.saveAndAssign') }}</Button></template>
    </Dialog>
  </main>
</template>

<style scoped>
.topic-page { display: grid; grid-template-columns: 304px minmax(0, 1fr); grid-template-rows: minmax(0, 1fr); flex: 1; min-height: 0; min-width: 0; overflow-y: auto; background: var(--bg); color: var(--text); font-family: var(--font); }
.topic-page.is-empty { grid-template-columns: minmax(0, 1fr); }
.topic-sidebar { min-width: 0; border-right: 2px solid var(--rule); overflow: auto; }
.is-empty .topic-sidebar { border-right: 0; }
.topic-list-head { display: grid; gap: var(--space-3); padding: var(--space-5) var(--space-4) var(--space-4); }
h1, h2 { margin: 0; font-size: var(--type-page-size); font-weight: var(--type-page-weight); line-height: 1.05; letter-spacing: -0.035em; overflow-wrap: anywhere; }
h3 { margin: 0; font-size: var(--type-subsection-size); font-weight: var(--type-subsection-weight); }
.topic-list, .topic-failures { list-style: none; padding: 0; margin: 0; }
.topic-list { border-top: 2px solid var(--rule); }
.topic-list button { display: grid; width: 100%; grid-template-columns: minmax(0, 1fr) auto; gap: var(--space-1) var(--space-2); text-align: left; padding: var(--space-3) var(--space-4); border: 0; border-bottom: 1px solid var(--border); background: var(--bg); color: var(--text); font: inherit; cursor: pointer; overflow-wrap: anywhere; }
.topic-list small { grid-column: 1 / 3; color: var(--muted); font-size: var(--text-xs); }
.topic-list .topic-library { display: flex; align-items: center; gap: var(--space-2); }
.topic-list .is-failed { color: var(--danger); }
.topic-list button[aria-current="true"] { background: var(--text); color: var(--bg); }
.topic-list button[aria-current="true"] small { color: var(--bg); }
.topic-list button:hover:not([aria-current="true"]), .topic-list button:focus-visible:not([aria-current="true"]) { background: var(--select); color: var(--on-select); }
.topic-list button:hover:not([aria-current="true"]) small, .topic-list button:focus-visible:not([aria-current="true"]) small { color: var(--on-select); }
.topic-list button:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.topic-list-status, .topic-list-error { padding: var(--space-3) var(--space-4); }
.topic-list-error, .topic-inline-error { color: var(--danger); }
.topic-detail { display: flex; flex-direction: column; gap: var(--space-5); min-width: 0; overflow: auto; max-width: 1024px; padding: var(--space-5) var(--space-6) var(--space-7); }
.topic-scope { display: inline-flex; align-items: center; gap: var(--space-2); flex-wrap: wrap; font-size: var(--text-xs); }
.topic-detail > * { flex-shrink: 0; }
.topic-detail-head, .topic-actions, .topic-progress-row, .topic-materials-head, .topic-pagination { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-3); }
.topic-detail-head, .topic-progress-row, .topic-materials-head { justify-content: space-between; }
.topic-detail-head { align-items: end; }
.topic-description { margin: 0; max-width: 44em; font-size: var(--text-lg); line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; }
.topic-detail :deep(.follow-error) { color: var(--danger); }
.topic-progress { display: grid; gap: var(--space-2); padding-top: var(--space-3); border-top: 2px solid var(--rule); }
.topic-progress > .rq-button { justify-self: start; }
.topic-progress-bar { display: flex; height: var(--space-3); background: var(--surface-soft); }
.topic-progress-bar i { display: block; height: 100%; background: var(--text); }
.topic-progress-bar i.failed { background: var(--danger); }
.topic-hint { margin: 0; color: var(--muted); font-size: var(--text-xs); line-height: 1.6; }
.topic-failures { color: var(--danger); font-size: var(--text-sm); line-height: 1.6; overflow-wrap: anywhere; }
.topic-failures li { padding-block: var(--space-2); border-bottom: 1px solid var(--border); }
.topic-materials { display: grid; gap: var(--space-3); min-width: 0; }
.topic-materials-head { padding-top: var(--space-3); border-top: 2px solid var(--rule); }
.topic-link { color: var(--text); text-decoration: underline; text-underline-offset: 3px; }
tbody .topic-link { font-weight: 700; }
.topic-link:hover { color: var(--accent); }
.topic-link:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
.topic-table-scroll { overflow-x: auto; min-width: 0; }
table { width: 100%; min-width: 760px; border-collapse: collapse; font-size: var(--text-sm); }
th, td { padding: var(--space-3) var(--space-2); border-bottom: 1px solid var(--border); text-align: left; vertical-align: top; }
th { border-bottom: 2px solid var(--rule); font-size: var(--text-xs); }
th:first-child, td:first-child { min-width: 96px; white-space: nowrap; }
th:nth-child(2), td:nth-child(2) { min-width: 220px; overflow-wrap: anywhere; }
th:nth-child(3), td:nth-child(3) { min-width: 120px; }
th:nth-child(4), td:nth-child(4) { min-width: 96px; white-space: nowrap; }
th:last-child, td:last-child { min-width: 168px; white-space: nowrap; }
tbody tr:hover, tbody tr:focus-within { background: var(--select); color: var(--on-select); }
tbody tr:hover .topic-link, tbody tr:focus-within .topic-link { color: var(--on-select); }
tbody tr:hover :deep(.rq-tag), tbody tr:focus-within :deep(.rq-tag) { color: var(--on-select); background: transparent; }
.mono { font-family: var(--mono); font-variant-numeric: tabular-nums; }
.topic-pagination { justify-content: flex-end; }
.topic-form { display: grid; gap: var(--space-4); }
@media (max-width: 900px) {
  .topic-page { grid-template-columns: minmax(0, 1fr); display: block; }
  .topic-sidebar, .topic-detail { overflow: visible; }
  .topic-sidebar { border-right: 0; border-bottom: 2px solid var(--rule); }
  .topic-detail { padding: var(--space-4); }
}
</style>
