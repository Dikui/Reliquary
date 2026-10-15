<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import SafeTextarea from '@/components/SafeTextarea.vue'
import FollowToggle from '@/components/FollowToggle.vue'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'
import { useFollowStore } from '@/stores/follows'
import { createTopic, deleteTopic, getTopicBackfill, listTopics, retryTopicBackfill, updateTopic, type Topic, type TopicBackfill } from '@/api/topics'

const { t } = useI18n()
const followStore = useFollowStore()
const topics = ref<Topic[]>([]), loading = ref(false), error = ref('')
const progress = reactive<Record<string, TopicBackfill | null>>({})
const progressErrors = reactive<Record<string, string>>({}), retrying = reactive<Record<string, boolean>>({})
const editorVisible = ref(false), editing = ref<Topic | null>(null), draft = ref({ name: '', description: '', library_ids: [] as string[] })
const libraries = ref<LibraryCatalogItem[]>([]), librariesError = ref(false)
const libraryOptions = computed(() => libraries.value.map(lib => ({ value: lib.id, label: lib.display_name || lib.name })))
async function loadLibraries() { try { const response = await listLibraries(); if (!response.success) throw new Error(); libraries.value = response.data || []; librariesError.value = false } catch { librariesError.value = true } }
const saving = ref(false), saveError = ref(''), deleting = ref(false), deleteError = ref(''), deleteTarget = ref<Topic | null>(null)
const deleteVisible = computed({ get: () => Boolean(deleteTarget.value), set: value => { if (!value && !deleting.value) deleteTarget.value = null } })
let generation = 0, pollTimer: ReturnType<typeof setTimeout> | undefined, disposed = false
const active = (run?: TopicBackfill | null) => Boolean(run && ['pending', 'running'].includes(run.status))
async function readProgress(id: string, current = generation) {
  try {
    const response = await getTopicBackfill(id)
    if (!response.success) throw new Error(response.message)
    if (current === generation && !disposed) { progress[id] = response.data; delete progressErrors[id] }
  } catch { if (current === generation && !disposed) progressErrors[id] = t('topicManagement.progressFailed') }
}
function schedulePoll() {
  clearTimeout(pollTimer)
  if (disposed || !topics.value.some(topic => active(progress[topic.id]))) return
  pollTimer = setTimeout(async () => {
    const current = generation
    await Promise.all(topics.value.filter(topic => active(progress[topic.id])).map(topic => readProgress(topic.id, current)))
    if (current !== generation || disposed) return
    try {
      const response = await listTopics()
      if (current === generation && !disposed && response.success) topics.value = response.data || []
    } catch { /* Existing counts remain visible until the next refresh. */ }
    schedulePoll()
  }, 2000)
}
async function load() {
  const current = ++generation
  clearTimeout(pollTimer)
  loading.value = true
  error.value = ''
  try {
    const response = await listTopics()
    if (!response.success) throw new Error(response.message)
    if (current !== generation || disposed) return
    topics.value = response.data || []
    await Promise.all(topics.value.map(topic => readProgress(topic.id, current)))
    if (current === generation) schedulePoll()
  } catch { if (current === generation && !disposed) error.value = t('topicManagement.loadFailed') }
  finally { if (current === generation && !disposed) loading.value = false }
}
function openEditor(topic?: Topic) {
  editing.value = topic || null
  draft.value = { name: topic?.name || '', description: topic?.description || '', library_ids: [...(topic?.library_ids || [])] }
  void loadLibraries()
  saveError.value = ''
  editorVisible.value = true
}
async function save() {
  if (saving.value) return
  const input = { name: draft.value.name.trim(), description: draft.value.description.trim(), library_ids: draft.value.library_ids }
  if (!input.name) { saveError.value = t('topicManagement.nameRequired'); return }
  saving.value = true
  saveError.value = ''
  try {
    const response = editing.value ? await updateTopic(editing.value.id, input) : await createTopic(input)
    if (!response.success) throw new Error(response.message)
    editorVisible.value = false
    MessagePlugin.success(t(editing.value ? 'topicManagement.updated' : 'topicManagement.created'))
    await load()
  } catch (cause: any) { saveError.value = cause?.status === 409 ? t('topicManagement.nameExists') : t('topicManagement.saveFailed') }
  finally { saving.value = false }
}
function requestDelete(topic: Topic) { deleteTarget.value = topic; deleteError.value = '' }
async function remove() {
  if (!deleteTarget.value || deleting.value) return
  const id = deleteTarget.value.id
  deleting.value = true
  deleteError.value = ''
  try {
    const response = await deleteTopic(id)
    if (!response.success) throw new Error(response.message)
    deleteTarget.value = null
    delete progress[id]
    MessagePlugin.success(t('topicManagement.deleted'))
    await followStore.refreshAfterTargetChange()
    await load()
  } catch { deleteError.value = t('topicManagement.deleteFailed') }
  finally { deleting.value = false }
}
async function retry(id: string) {
  if (retrying[id] || active(progress[id])) return
  retrying[id] = true
  delete progressErrors[id]
  try {
    const response = await retryTopicBackfill(id)
    if (!response.success) throw new Error(response.message)
    if (!disposed && topics.value.some(topic => topic.id === id)) progress[id] = response.data
    schedulePoll()
  } catch { progressErrors[id] = t('topicManagement.retryFailed') }
  finally { retrying[id] = false }
}
onMounted(load)
onUnmounted(() => { disposed = true; generation++; clearTimeout(pollTimer) })
</script>

<template>
  <main class="topic-page">
    <header class="topic-page-header"><div><h1>{{ t('topicManagement.title') }}</h1><p>{{ t('topicManagement.description') }}</p></div><t-button @click="openEditor()"><template #icon><t-icon name="add" /></template>{{ t('topicManagement.create') }}</t-button></header>
    <p class="topic-hint">{{ t('topicManagement.historyHint') }}</p>
    <div v-if="error" class="topic-error" role="alert">{{ error }} <t-button variant="text" @click="load">{{ t('common.retry') }}</t-button></div>
    <t-loading :loading="loading">
      <ul v-if="topics.length" class="topic-list">
        <li v-for="topic in topics" :key="topic.id" class="topic-card">
          <div class="topic-heading"><h2>{{ topic.name }}</h2><span>{{ t('topicManagement.articleCount', { count: topic.entry_count }) }}</span></div>
          <p class="topic-description">{{ topic.description || t('topicManagement.noDescription') }}</p>
          <div v-if="progress[topic.id]" class="topic-progress" aria-live="polite">
            <span>{{ t(`topicManagement.status.${progress[topic.id]!.status}`) }}</span>
            <span>{{ t('topicManagement.progress', { processed: progress[topic.id]!.processed, total: progress[topic.id]!.total }) }}</span>
            <span>{{ t('topicManagement.failures', { articles: progress[topic.id]!.failed_entries, batches: progress[topic.id]!.failed_batches }) }}</span>
            <progress :value="progress[topic.id]!.processed" :max="Math.max(1, progress[topic.id]!.total)" :aria-label="t('topicManagement.historyProgress')" />
            <t-button v-if="progress[topic.id]!.failed_batches > 0" size="small" variant="outline" :loading="retrying[topic.id]" :disabled="active(progress[topic.id])" @click="retry(topic.id)">{{ t('topicManagement.retryFailedBatches') }}</t-button>
          </div>
          <ul v-if="progress[topic.id]?.batches?.some(batch => batch.status === 'failed')" class="topic-batch-errors">
            <li v-for="batch in progress[topic.id]!.batches!.filter(batch => batch.status === 'failed')" :key="batch.id">{{ t('topicManagement.batchFailure', { ordinal: batch.ordinal, count: batch.entry_count, attempts: batch.attempts, reason: batch.error || t('common.error') }) }}</li>
          </ul>
          <div v-if="progressErrors[topic.id]" class="topic-error" role="alert">{{ progressErrors[topic.id] }} <t-button size="small" variant="text" @click="readProgress(topic.id).then(schedulePoll)">{{ t('common.retry') }}</t-button></div>
          <div class="topic-actions"><FollowToggle target-type="topic" :target-id="topic.id" :name="topic.name" /><t-button size="small" variant="outline" @click="openEditor(topic)">{{ t('topicManagement.edit') }}</t-button><t-button size="small" variant="text" theme="danger" @click="requestDelete(topic)">{{ t('common.delete') }}</t-button></div>
        </li>
      </ul>
      <t-empty v-else-if="!loading && !error" :description="t('topicManagement.empty')" />
    </t-loading>
    <t-dialog v-model:visible="editorVisible" :header="t(editing ? 'topicManagement.edit' : 'topicManagement.create')" :confirm-btn="{ content: t('common.save'), loading: saving, disabled: saving }" :cancel-btn="{ content: t('common.cancel'), disabled: saving }" :close-btn="!saving" :close-on-overlay-click="false" :close-on-esc-keydown="!saving" @confirm="save">
      <form class="topic-form" novalidate @submit.prevent="save">
        <label><span>{{ t('topicManagement.name') }}</span><t-input v-model="draft.name" :maxlength="255" :disabled="saving" :aria-label="t('topicManagement.name')" :aria-invalid="Boolean(saveError)" /></label>
        <label><span>{{ t('topicManagement.topicDescription') }}</span><SafeTextarea v-model="draft.description" :autosize="{ minRows: 3, maxRows: 8 }" :maxlength="2000" :disabled="saving" :aria-label="t('topicManagement.topicDescription')" /></label>
        <label><span>{{ t('topicManagement.libraries') }}</span><t-select v-model="draft.library_ids" multiple clearable :options="libraryOptions" :disabled="saving || librariesError" :placeholder="t('follows.allLibraries')" :aria-label="t('topicManagement.libraries')" /></label>
        <p class="topic-hint">{{ t('topicManagement.libraryHint') }}</p>
        <p v-if="librariesError" role="alert">{{ t('topicManagement.libraryLoadFailed') }} <t-button variant="text" @click="loadLibraries">{{ t('common.retry') }}</t-button></p>
        <p class="topic-hint">{{ t('topicManagement.descriptionHint') }}</p>
        <p v-if="saveError" class="topic-error" role="alert">{{ saveError }}</p>
      </form>
    </t-dialog>
    <t-dialog v-model:visible="deleteVisible" :header="t('topicManagement.deleteTitle')" :confirm-btn="{ content: t('common.delete'), theme: 'danger', loading: deleting, disabled: deleting }" :cancel-btn="{ content: t('common.cancel'), disabled: deleting }" :close-btn="!deleting" :close-on-overlay-click="false" :close-on-esc-keydown="!deleting" @confirm="remove">
      <p>{{ t('topicManagement.deleteDescription', { name: deleteTarget?.name || '' }) }}</p><p v-if="deleteError" class="topic-error" role="alert">{{ deleteError }}</p>
    </t-dialog>
  </main>
</template>

<style scoped lang="less">
.topic-page { height: 100%; overflow-y: auto; box-sizing: border-box; padding: 32px; color: var(--td-text-color-primary); }
.topic-page-header, .topic-heading { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.topic-page-header h1 { margin: 0; font-size: 24px; }
.topic-page-header p, .topic-hint { color: var(--td-text-color-secondary); line-height: 1.7; }
.topic-hint { font-size: 13px; }
.topic-list { display: grid; gap: 16px; padding: 0; list-style: none; }
.topic-card { padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); min-width: 0; overflow-wrap: anywhere; }
.topic-heading h2 { margin: 0; font-size: 16px; }
.topic-heading span { color: var(--td-text-color-secondary); font-size: 13px; flex-shrink: 0; }
.topic-description { margin: 12px 0; line-height: 1.7; white-space: pre-wrap; }
.topic-progress { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 16px; color: var(--td-text-color-secondary); font-size: 13px; }
.topic-progress progress { width: 160px; accent-color: var(--td-brand-color); }
.topic-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-top: 16px; }
.topic-form, .topic-form label { display: grid; gap: 8px; }
.topic-form { gap: 16px; }
.topic-error { color: var(--td-error-color); font-size: 13px; line-height: 1.7; }
.topic-batch-errors { padding-left: 20px; color: var(--td-error-color); font-size: 13px; line-height: 1.7; }
@media (max-width: 640px) { .topic-page { padding: 16px; } .topic-page-header { flex-direction: column; } .topic-heading { flex-wrap: wrap; } }
</style>
