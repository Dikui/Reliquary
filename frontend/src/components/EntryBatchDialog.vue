<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { previewEntryBatch, submitEntryBatch, getEntryBatch, retryFailedEntryBatch, type EntryBatchAction, type EntryBatchSelection, type EntryBatchRequest, type EntryBatchPreview, type EntryBatch } from '@/api/entry-batches'
import { listTopics, type Topic } from '@/api/topics'
import type { LibraryCatalogItem } from '@/api/library'
const props = defineProps<{ visible: boolean; batchId?: string; ids?: string[]; selection?: EntryBatchSelection; scopeLabel?: string; libraries: LibraryCatalogItem[]; selectedLibraryIds?: string[]; initialAction?: EntryBatchAction }>()
const emit = defineEmits<{ 'update:visible': [value: boolean]; changed: []; created: [id: string] }>()
const { t } = useI18n()
const ready = ref(false), closeButton = ref<{ $el?: HTMLElement } | null>(null)
let trigger: HTMLElement | null = null
onMounted(() => { ready.value = true })
function focusClose() { closeButton.value?.$el?.focus?.({ preventScroll: true }) }
watch(() => props.visible, async (visible, _previous, onCleanup) => {
  let cancelled = false
  onCleanup(() => { cancelled = true })
  if (visible) {
    const active = typeof document === 'undefined' ? null : document.activeElement
    trigger = typeof HTMLElement !== 'undefined' && active instanceof HTMLElement ? active : null
  } else {
    const target = trigger; trigger = null
    await nextTick()
    if (!cancelled && target?.isConnected) target.focus({ preventScroll: true })
  }
}, { immediate: true })
const step = ref<'scope' | 'action'>('scope'), stepTitle = ref<HTMLElement>()
const action = ref<EntryBatchAction>('star'), scope = ref('selected'), starred = ref(true), contentType = ref(''), topicIds = ref<string[]>([]), topicMode = ref<'add' | 'remove'>('add')
const topicsError = ref(''), topicsLoading = ref(false)
let topicsGeneration = 0
async function loadTopics() {
  const current = ++topicsGeneration; topicsLoading.value = true; topicsError.value = ''
  try { const response = await listTopics(); if (disposed || current !== topicsGeneration || !props.visible) return; if (!response.success) throw new Error(); topics.value = response.data || [] }
  catch { if (!disposed && current === topicsGeneration && props.visible) topicsError.value = t('entryBatch.choicesFailed') }
  finally { if (!disposed && current === topicsGeneration) topicsLoading.value = false }
}
const preview = ref<EntryBatchPreview>(), batch = ref<EntryBatch>(), confirmed = ref(false), busy = ref(false), error = ref(''), topics = ref<Topic[]>([]), page = ref(1)
let generation = 0, timer: ReturnType<typeof setTimeout> | undefined, disposed = false, readGeneration = 0
const actions: EntryBatchAction[] = ['star', 'content_type', 'topics', 'review', 'reprocess', 'delete']
const selection = computed<EntryBatchSelection>(() => scope.value === 'selected' && !props.scopeLabel ? { ids: [...(props.ids || [])] } : (props.selection || {}))
const singleLibrary = computed(() => { const ids = selection.value.ids ? props.selectedLibraryIds : selection.value.filters?.libraries; return ids?.length === 1 ? props.libraries.find(lib => lib.id === ids[0]) : undefined })
const contentTypes = computed(() => (singleLibrary.value?.content_types || []).filter(row => row.enabled).map(row => ({ value: row.name, label: row.name })))
const request = computed<EntryBatchRequest>(() => ({ action: action.value, selection: selection.value, ...(action.value === 'star' ? { starred: starred.value } : {}), ...(action.value === 'content_type' ? { content_type: contentType.value } : {}), ...(action.value === 'topics' ? { topic_ids: topicIds.value, topic_mode: topicMode.value } : {}) }))
const scopeValid = computed(() => Boolean(selection.value.filters || selection.value.ids?.length))
const valid = computed(() => scopeValid.value && (action.value !== 'content_type' || Boolean(contentType.value)) && (action.value !== 'topics' || topicIds.value.length > 0))
const active = computed(() => Boolean(batch.value && ['pending', 'running'].includes(batch.value.status)))
const scopeText = computed(() => props.batchId && !preview.value ? t('entryBatch.fixedScope') : props.scopeLabel || t(scope.value === 'selected' ? 'entryBatch.selectedScope' : 'entryBatch.matchedScope', { count: props.ids?.length || 0 }))
async function changeStep(value: 'scope' | 'action') {
  if (busy.value || (value === 'action' && !scopeValid.value)) return
  step.value = value; preview.value = undefined; confirmed.value = false; error.value = ''
  await nextTick(); stepTitle.value?.focus?.({ preventScroll: true })
}
function stop() { clearTimeout(timer); timer = undefined }
function close() { if (!busy.value) { stop(); generation++; emit('update:visible', false) } }
function fail(cause: unknown) { error.value = typeof (cause as { message?: unknown })?.message === 'string' ? (cause as { message: string }).message : t('entryBatch.failed') }
function receive(value: EntryBatch) { batch.value = value; if (!['pending', 'running'].includes(value.status)) emit('changed') }
async function refresh() {
  if ((!batch.value && !props.batchId) || busy.value) return
  stop()
  const current = generation, read = ++readGeneration
  try {
    const response = await getEntryBatch(batch.value?.batch_id || props.batchId!, page.value)
    if (disposed || current !== generation || read !== readGeneration) return
    if (!response.success) throw new Error(response.message || t('entryBatch.failed'))
    error.value = ''; receive(response.data)
    if (active.value) timer = setTimeout(refresh, 2000)
  } catch (cause) { if (!disposed && current === generation && read === readGeneration) fail(cause) }
}
async function showPreview() {
  if (busy.value || step.value !== 'action' || !valid.value || batch.value) return
  const current = generation; busy.value = true; error.value = ''; preview.value = undefined; confirmed.value = false
  try { const response = await previewEntryBatch(request.value); if (current !== generation || disposed) return; if (!response.success) throw new Error(response.message || t('entryBatch.failed')); preview.value = response.data }
  catch (cause) { if (current === generation && !disposed) fail(cause) }
  finally { if (current === generation && !disposed) busy.value = false }
}
async function submit(retry = false) {
  if (busy.value || (!retry && (!preview.value || !confirmed.value || !preview.value.eligible)) || (retry && (!batch.value || active.value || !batch.value.failed))) return
  stop(); const current = generation; busy.value = true; error.value = ''
  try {
    const response = retry ? await retryFailedEntryBatch(batch.value!.batch_id) : await submitEntryBatch({ ...request.value, preview_fingerprint: preview.value!.fingerprint })
    if (current !== generation || disposed) return
    if (!response.success) throw new Error(response.message || t('entryBatch.failed'))
    page.value = 1; receive(response.data); emit('created', response.data.batch_id)
  } catch (cause) { if (current === generation && !disposed) fail(cause) }
  finally { if (current === generation && !disposed) { busy.value = false; if (active.value) timer = setTimeout(refresh, 1000) } }
}
watch(request, () => { if (!batch.value) { generation++; busy.value = false; preview.value = undefined; confirmed.value = false } }, { deep: true })
watch(() => [props.visible, props.batchId] as const, async ([visible, batchId]) => {
  if (visible && batchId && batch.value?.batch_id === batchId) return
  stop(); generation++; topicsGeneration++
  if (!visible) return
  batch.value = undefined; preview.value = undefined; confirmed.value = false; error.value = ''; busy.value = false; page.value = 1
  if (batchId) { void refresh(); return }
  step.value = props.scopeLabel ? 'action' : 'scope'
  scope.value = props.scopeLabel || !props.ids?.length ? 'matched' : 'selected'; action.value = props.initialAction || 'star'; contentType.value = ''; topicIds.value = []
  void loadTopics()
}, { immediate: true })
watch(page, () => { if (props.visible && batch.value) void refresh() })
onUnmounted(() => { disposed = true; trigger = null; generation++; readGeneration++; stop() })
</script>
<template>
  <t-dialog :visible="visible && ready" :header="t('entryBatch.title')" width="min(760px, 95vw)" top="16px" :dialog-style="{ maxHeight: 'calc(100dvh - 32px)', overflow: 'auto' }" :footer="false" :close-on-overlay-click="false" :close-on-esc-keydown="!busy" :close-btn="!busy" @opened="focusClose" @close="close">
    <div class="entry-batch" role="dialog" aria-modal="true" :aria-label="t('entryBatch.title')">
      <div class="entry-batch-content">
      <p>{{ scopeText }}</p>
      <p v-if="batchId && !batch && !error" role="status">{{ t('common.loading') }}</p>
      <template v-if="!batch && !batchId">
        <h3 ref="stepTitle" tabindex="-1" class="batch-step-title">{{ t(scopeLabel ? 'entryBatch.action' : step === 'scope' ? 'entryBatch.scopeStep' : 'entryBatch.actionStep') }}</h3>
        <template v-if="step === 'scope'">
          <label><span>{{ t('entryBatch.scope') }}</span><t-select v-model="scope" :disabled="busy" :aria-label="t('entryBatch.scope')" :options="[{ value: 'selected', label: t('entryBatch.selectedScope', { count: ids?.length || 0 }), disabled: !ids?.length }, { value: 'matched', label: t('entryBatch.matchedScope') }]" /></label>
          <p v-if="scope !== 'selected'">{{ t('entryBatch.filterScopeHint') }}</p>
          <t-button theme="primary" :disabled="!scopeValid" @click="changeStep('action')">{{ t('entryBatch.nextAction') }}</t-button>
        </template>
        <template v-else>
        <t-button v-if="!scopeLabel" variant="text" :disabled="busy" @click="changeStep('scope')">{{ t('entryBatch.changeScope') }}</t-button>
        <p v-if="scope !== 'selected'">{{ t('entryBatch.snapshotHint') }}</p>
        <label><span>{{ t('entryBatch.action') }}</span><t-select v-model="action" :disabled="busy || !!scopeLabel" :aria-label="t('entryBatch.action')" :options="actions.map(value => ({ value, label: t(`entryBatch.actions.${value}`) }))" /></label>
        <label v-if="action === 'star'"><span>{{ t('entryBatch.actions.star') }}</span><t-select v-model="starred" :disabled="busy" :aria-label="t('entryBatch.actions.star')" :options="[{ value: true, label: t('entryBatch.star') }, { value: false, label: t('entryBatch.unstar') }]" /></label>
        <label v-if="action === 'content_type'"><span>{{ t('entryBatch.actions.content_type') }}</span><t-select v-model="contentType" :disabled="busy" :aria-label="t('entryBatch.actions.content_type')" :options="contentTypes" /><small v-if="!singleLibrary">{{ t('entryBatch.singleLibrary') }}</small></label>
        <template v-if="action === 'topics'"><label><span>{{ t('entryBatch.topicMode') }}</span><t-select v-model="topicMode" :disabled="busy" :aria-label="t('entryBatch.topicMode')" :options="[{ value: 'add', label: t('entryBatch.addTopics') }, { value: 'remove', label: t('entryBatch.removeTopics') }]" /></label><label><span>{{ t('entryBatch.actions.topics') }}</span><t-select v-model="topicIds" multiple filterable :loading="topicsLoading" :disabled="busy" :aria-label="t('entryBatch.actions.topics')" :options="topics.map(topic => ({ value: topic.id, label: topic.name }))" /></label></template>
        <p v-if="action === 'topics' && topicsError" role="alert">{{ topicsError }} <t-button variant="text" :loading="topicsLoading" @click="loadTopics">{{ t('common.retry') }}</t-button></p>
        <t-alert v-if="action === 'delete'" theme="warning" :message="t('entryBatch.deleteHint')" />
        <t-alert v-if="action === 'review' || action === 'reprocess'" theme="warning" :message="t(action === 'review' ? 'entryBatch.reviewHint' : 'entryBatch.reprocessHint')" />
        <t-button :loading="busy" :disabled="busy || !valid" @click="showPreview">{{ t('entryBatch.preview') }}</t-button>
        <section v-if="preview" aria-live="polite"><p>{{ t('entryBatch.previewCounts', { ...preview }) }}</p><ul><li v-for="reason in preview.reasons" :key="reason.code">{{ t('entryBatch.reasonCount', { reason: t(`entryBatch.reasons.${reason.code}`, reason.code), count: reason.count }) }}</li></ul><p>{{ t('entryBatch.samples') }}</p><ul><li v-for="item in preview.samples" :key="item.id">{{ item.title || item.id }} · {{ libraries.find(lib => lib.id === item.library_id)?.display_name || item.library_id }}</li></ul><t-checkbox v-model="confirmed" :disabled="busy || !preview.eligible">{{ t('entryBatch.confirmScope', { scope: scopeText, count: preview.eligible }) }}</t-checkbox><div><t-button :theme="action === 'delete' ? 'danger' : 'primary'" :disabled="busy || !confirmed || !preview.eligible || preview.blocked > 0" :loading="busy" @click="submit()">{{ t('entryBatch.confirm') }}</t-button></div></section>
        </template>
      </template>
      <section v-else-if="batch" aria-live="polite"><p><strong>{{ t(`entryBatch.actions.${batch.action}`) }}</strong> · {{ t(`entryBatch.status.${batch.status}`) }} · {{ t('entryBatch.resultCounts', { ...batch }) }}</p><a class="batch-id" :href="`/platform/entries?batch_id=${encodeURIComponent(batch.batch_id)}`">{{ t('entryBatch.resultLink') }} · {{ batch.batch_id }}</a><p v-if="active">{{ t('entryBatch.runningHint') }}</p><p v-if="batch.warnings">{{ t('entryBatch.warningCount', { count: batch.warnings }) }}</p><ul class="batch-results"><li v-for="item in batch.items" :key="item.entry_id"><strong>{{ item.title || item.entry_id }}</strong><span>{{ t(`entryBatch.status.${item.status}`) }}</span><p v-if="item.message || item.warning">{{ item.message || item.warning }}</p><small v-if="item.task_id">{{ item.task_id }}</small></li></ul><t-pagination v-if="batch.total > 20" v-model:current="page" :total="batch.total" :page-size="20" :show-page-size="false" size="small" /><t-button v-if="!active && batch.failed > 0" :loading="busy" :disabled="busy" @click="submit(true)">{{ t('entryBatch.retryFailed') }}</t-button></section>
      <t-alert v-if="error" theme="error" :message="error" role="alert" /><t-button v-if="(batch || batchId) && error" variant="outline" :disabled="busy" @click="refresh">{{ t('entryBatch.refreshStatus') }}</t-button>
      </div>
      <div class="batch-footer"><t-button ref="closeButton" variant="outline" :disabled="busy" @click="close">{{ t('common.close') }}</t-button></div>
    </div>
  </t-dialog>
</template>
<style scoped>
.entry-batch { display: flex; flex-direction: column; gap: 12px; min-width: 0; max-height: calc(100dvh - 144px); }
.entry-batch-content { display: flex; flex-direction: column; gap: 12px; min-height: 0; overflow: auto; scrollbar-gutter: stable; }
.entry-batch-content > * { flex-shrink: 0; }
.batch-step-title { margin: 0; font-size: 16px; font-weight: 600; }
.entry-batch p { margin: 0; overflow-wrap: anywhere; }
.entry-batch label { display: flex; flex-direction: column; gap: 6px; }
.entry-batch small, .batch-id { color: var(--td-text-color-secondary); overflow-wrap: anywhere; }
.entry-batch section { display: flex; flex-direction: column; gap: 12px; }
.entry-batch ul { margin: 0; padding-left: 20px; overflow-wrap: anywhere; }
.batch-results { max-height: 320px; overflow: auto; }
.batch-results li { padding: 8px 0; border-bottom: 1px solid var(--td-component-stroke); }
.batch-results span { margin-left: 12px; }
.batch-footer { display: flex; justify-content: flex-end; flex-shrink: 0; }
</style>
