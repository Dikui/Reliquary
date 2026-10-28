<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { previewEntryBatch, submitEntryBatch, getEntryBatch, retryFailedEntryBatch, type EntryBatchAction, type EntryBatchSelection, type EntryBatchRequest, type EntryBatchPreview, type EntryBatch } from '@/api/entry-batches'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'
import { entryChannelLabel, entryFileFormatLabel } from '@/utils/entrySourceLabels'
const props = defineProps<{ visible: boolean; batchId?: string; ids?: string[]; selection?: EntryBatchSelection; scopeLabel?: string; libraries: LibraryCatalogItem[]; selectedLibraryIds?: string[]; initialAction?: EntryBatchAction; domainOptions?: { value: string; label: string }[]; entityOptions?: { value: string; label: string }[] }>()
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
const action = ref<EntryBatchAction>('star'), scope = ref('selected'), starred = ref(true)
const preview = ref<EntryBatchPreview>(), batch = ref<EntryBatch>(), confirmed = ref(false), busy = ref(false), error = ref(''), page = ref(1)
let generation = 0, timer: ReturnType<typeof setTimeout> | undefined, disposed = false, readGeneration = 0
const actions: EntryBatchAction[] = ['star', 'reprocess', 'reclassify_domains', 'delete']
const selection = computed<EntryBatchSelection>(() => scope.value === 'selected' && !props.scopeLabel ? { ids: [...(props.ids || [])] } : (props.selection || {}))
const request = computed<EntryBatchRequest>(() => ({ action: action.value, selection: selection.value, ...(action.value === 'star' ? { starred: starred.value } : {}) }))
const scopeValid = computed(() => Boolean(selection.value.filters || selection.value.ids?.length))
const scopeCount = ref<{ matched: number; unavailable: number }>(), scopeLoading = ref(false), scopeError = ref('')
let scopeGeneration = 0
const scopeKnown = computed(() => !scopeLoading.value && !scopeError.value && !!scopeCount.value)
function rememberScope(value: EntryBatchPreview) {
  scopeCount.value = { matched: value.matched, unavailable: value.reasons.find(reason => reason.code === 'unavailable')?.count || 0 }
}
async function loadScope() {
  if (!props.visible || props.batchId || batch.value || !scopeValid.value) return
  const current = ++scopeGeneration; scopeLoading.value = true; scopeError.value = ''; scopeCount.value = undefined
  try {
    // Count the exact batch selection, without reusing an action fingerprint.
    const response = await previewEntryBatch({ action: 'star', starred: true, selection: selection.value })
    if (disposed || current !== scopeGeneration) return
    if (!response.success) throw new Error()
    rememberScope(response.data)
  } catch { if (!disposed && current === scopeGeneration) scopeError.value = t('entryBatch.countFailed') }
  finally { if (!disposed && current === scopeGeneration) scopeLoading.value = false }
}
const filterSummary = computed(() => {
  const f = props.selection?.filters || {}
  const rows: { label: string; value: string }[] = []
  const add = (label: string, value?: string) => { if (value) rows.push({ label, value }) }
  const names = (ids: string[] | undefined, options: { value: string; label: string }[] = []) => ids?.map(id => options.find(option => option.value === id)?.label || id).join(', ')
  const range = (from?: string, to?: string) => from || to ? `${from || t('entryBatch.anyDate')} → ${to || t('entryBatch.anyDate')}` : ''
  add(t('ikb18.scope'), names(f.libraries, props.libraries.map(lib => ({ value: lib.id, label: lib.display_name || lib.name }))) || t('ikb18.all'))
  add(t('entryBatch.entities'), names(f.entity_ids, props.entityOptions))
  add(t('domains.title'), names(f.domain_ids, props.domainOptions))
  add(t('entryBatch.keyword'), f.keyword)
  if (f.starred !== undefined) add(t('articleJudgments.starLabel'), t(f.starred ? 'articleJudgments.starredOnly' : 'articleJudgments.unstarredOnly'))
  const statuses: Record<string, string> = { pending: t('library.parseStatusPending'), processing: t('library.parseStatusProcessing'), finalizing: t('library.parseStatusFinalizing'), completed: t('library.parseStatusCompleted'), failed: t('library.parseStatusFailed'), cancelled: t('library.parseStatusCancelled'), draft: t('library.parseStatusDraft'), rejected: t('library.intakeWaiting') }
  if (f.parse_status) add(t('library.parseStatusFilter'), statuses[f.parse_status] || f.parse_status)
  const sources: Record<string, string> = { article: t('knowledgeSearch.sources.article'), mine: t('knowledgeSearch.sources.mine'), report: t('knowledgeSearch.sources.report') }
  if (f.source_kind) add(t('library.materialCategory'), sources[f.source_kind] || f.source_kind)
  if (f.source) add(t('library.channelLabel'), entryChannelLabel({ channel: f.source }, t))
  if (f.file_type) add(t('library.fileTypeFilter'), entryFileFormatLabel({ file_type: f.file_type }, t))
  add(t('entryBatch.publishedDates'), range(f.published_from, f.published_to))
  add(t('entryBatch.updatedDates'), range(f.start_time, f.end_time))
  if (f.include_rejected) add(t('entryBatch.includeRejected'), t('common.yes'))
  return rows
})
const valid = computed(() => actions.includes(action.value) && scopeValid.value && scopeKnown.value)
const active = computed(() => Boolean(batch.value && ['pending', 'running'].includes(batch.value.status)))
const scopeText = computed(() => props.batchId && !preview.value ? t('entryBatch.fixedScope') : props.scopeLabel || t(scope.value === 'selected' ? 'entryBatch.selectedScope' : 'entryBatch.matchedScope', { count: props.ids?.length || 0 }))
async function changeStep(value: 'scope' | 'action') {
  if (busy.value || (value === 'action' && (!scopeValid.value || !scopeKnown.value))) return
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
  try { const response = await previewEntryBatch(request.value); if (current !== generation || disposed) return; if (!response.success) throw new Error(response.message || t('entryBatch.failed')); preview.value = response.data; rememberScope(response.data) }
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
  stop(); generation++
  if (!visible) return
  batch.value = undefined; preview.value = undefined; confirmed.value = false; error.value = ''; busy.value = false; page.value = 1
  if (batchId) { void refresh(); return }
  step.value = props.scopeLabel ? 'action' : 'scope'
  scope.value = props.scopeLabel || !props.ids?.length ? 'matched' : 'selected'; action.value = props.initialAction && actions.includes(props.initialAction) ? props.initialAction : 'star'
}, { immediate: true })
watch([() => props.visible, () => props.batchId, selection], ([visible, batchId]) => {
  scopeGeneration++; scopeLoading.value = false; scopeCount.value = undefined; scopeError.value = ''
  if (visible && !batchId) void loadScope()
}, { immediate: true, deep: true })
watch(page, () => { if (props.visible && batch.value) void refresh() })
// Opened from the domains page, the parent has not loaded the library catalog yet.
const fetchedLibraries = ref<LibraryCatalogItem[]>([])
watch(() => props.visible, async visible => {
  if (!visible || props.libraries.length || fetchedLibraries.value.length) return
  try { const response = await listLibraries(); if (response.success) fetchedLibraries.value = response.data || [] } catch { /* Names fall back to IDs. */ }
})
function libraryName(id: string) {
  const library = (props.libraries.length ? props.libraries : fetchedLibraries.value).find(item => item.id === id)
  return library?.display_name || library?.name || id
}
onUnmounted(() => { disposed = true; trigger = null; generation++; readGeneration++; scopeGeneration++; stop() })
</script>
<template>
  <t-dialog :visible="visible && ready" :header="t('entryBatch.title')" width="min(760px, 95vw)" top="16px" :dialog-style="{ maxHeight: 'calc(100dvh - 32px)', overflow: 'auto' }" :footer="false" :close-on-overlay-click="false" :close-on-esc-keydown="!busy" :close-btn="!busy" @opened="focusClose" @close="close">
    <div class="entry-batch" role="dialog" aria-modal="true" :aria-label="t('entryBatch.title')">
      <div class="entry-batch-content">
      <p v-if="batch || batchId">{{ scopeText }}</p>
      <p v-if="batchId && !batch && !error" role="status">{{ t('common.loading') }}</p>
      <template v-if="!batch && !batchId">
        <h3 ref="stepTitle" tabindex="-1" class="batch-step-title">{{ t(scopeLabel ? 'entryBatch.action' : step === 'scope' ? 'entryBatch.scopeStep' : 'entryBatch.actionStep') }}</h3>
        <label v-if="step === 'scope'"><span>{{ t('entryBatch.scope') }}</span><t-select v-model="scope" :disabled="busy" :aria-label="t('entryBatch.scope')" :options="[{ value: 'selected', label: t('entryBatch.selectedScope', { count: ids?.length || 0 }), disabled: !ids?.length }, { value: 'matched', label: t('entryBatch.matchedScope') }]" /></label>
        <section class="batch-scope-summary" :aria-label="t('entryBatch.scopeSummary')">
          <strong>{{ scopeText }}</strong>
          <p v-if="scopeLoading" role="status">{{ t('entryBatch.countLoading') }}</p>
          <p v-else-if="scopeError" role="alert">{{ scopeError }} <t-button variant="text" :loading="scopeLoading" @click="loadScope">{{ t('entryBatch.retryCount') }}</t-button></p>
          <p v-else-if="scopeCount" class="batch-scope-count" role="status">{{ selection.ids ? t('entryBatch.selectedCount', { selected: scopeCount.matched, found: scopeCount.matched - scopeCount.unavailable, unavailable: scopeCount.unavailable }) : t('entryBatch.matchedCount', { count: scopeCount.matched }) }}</p>
          <p v-if="selection.ids">{{ t('entryBatch.checkedHint') }}</p>
          <strong>{{ t(selection.ids ? 'entryBatch.checkedFilters' : 'entryBatch.currentFilters') }}</strong>
          <dl><div v-for="row in filterSummary" :key="row.label"><dt>{{ row.label }}</dt><dd>{{ row.value }}</dd></div></dl>
          <small>{{ t('entryBatch.countHint') }}</small>
        </section>
        <template v-if="step === 'scope'">
          <p v-if="scope !== 'selected'">{{ t('entryBatch.filterScopeHint') }}</p>
          <t-button theme="primary" :disabled="!scopeValid || !scopeKnown" @click="changeStep('action')">{{ t('entryBatch.nextAction') }}</t-button>
        </template>
        <template v-else>
        <t-button v-if="!scopeLabel" variant="text" :disabled="busy" @click="changeStep('scope')">{{ t('entryBatch.changeScope') }}</t-button>
        <label><span>{{ t('entryBatch.action') }}</span><t-select v-model="action" :disabled="busy || !!scopeLabel" :aria-label="t('entryBatch.action')" :options="actions.map(value => ({ value, label: t(`entryBatch.actions.${value}`) }))" /></label>
        <label v-if="action === 'star'"><span>{{ t('entryBatch.actions.star') }}</span><t-select v-model="starred" :disabled="busy" :aria-label="t('entryBatch.actions.star')" :options="[{ value: true, label: t('entryBatch.star') }, { value: false, label: t('entryBatch.unstar') }]" /></label>
        <t-alert v-if="action === 'delete'" theme="warning" :message="t('entryBatch.deleteHint')" />
        <t-alert v-if="action === 'reprocess'" theme="warning" :message="t('entryBatch.reprocessHint')" />
        <t-alert v-if="action === 'reclassify_domains'" theme="info" :message="t('domains.reclassifyHint')" />
        <t-button :loading="busy" :disabled="busy || !valid" @click="showPreview">{{ t('entryBatch.preview') }}</t-button>
        <section v-if="preview" aria-live="polite"><p>{{ t('entryBatch.previewCounts', { ...preview }) }}</p><ul><li v-for="reason in preview.reasons" :key="reason.code">{{ t('entryBatch.reasonCount', { reason: t(`entryBatch.reasons.${reason.code}`, reason.code), count: reason.count }) }}</li></ul><p>{{ t('entryBatch.samples') }}</p><ul><li v-for="item in preview.samples" :key="item.id">{{ item.title || item.id }} · {{ libraryName(item.library_id) }}</li></ul><t-checkbox v-model="confirmed" :disabled="busy || !preview.eligible">{{ t('entryBatch.confirmScope', { scope: scopeText, count: preview.eligible }) }}</t-checkbox><div><t-button :theme="action === 'delete' ? 'danger' : 'primary'" :disabled="busy || !confirmed || !preview.eligible || preview.blocked > 0" :loading="busy" @click="submit()">{{ t('entryBatch.confirm') }}</t-button></div></section>
        </template>
      </template>
      <section v-else-if="batch" aria-live="polite"><p><strong>{{ t(`entryBatch.actions.${batch.action}`) }}</strong> · {{ t(`entryBatch.status.${batch.status}`) }} · {{ t('entryBatch.resultCounts', { ...batch }) }}</p><a class="batch-id" :href="`/platform/entries?batch_id=${encodeURIComponent(batch.batch_id)}`">{{ t('entryBatch.resultLink') }} · {{ batch.batch_id }}</a><p v-if="active">{{ t('entryBatch.runningHint') }}</p><p v-if="batch.warnings">{{ t('entryBatch.warningCount', { count: batch.warnings }) }}</p><ul class="batch-results"><li v-for="item in batch.items" :key="item.entry_id"><strong>{{ item.title || item.entry_id }}</strong><span>{{ t(`entryBatch.status.${item.status}`) }}</span><p v-if="item.message || item.warning">{{ item.message === 'domains_manual' ? t('entryBatch.reasons.domains_manual') : item.message || item.warning }}</p><small v-if="item.task_id">{{ item.task_id }}</small></li></ul><t-pagination v-if="batch.total > 20" v-model:current="page" :total="batch.total" :page-size="20" :show-page-size="false" size="small" /><t-button v-if="!active && batch.failed > 0" :loading="busy" :disabled="busy" @click="submit(true)">{{ t('entryBatch.retryFailed') }}</t-button></section>
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
.entry-batch label:not(.t-checkbox) { display: flex; flex-direction: column; gap: 6px; }
.entry-batch small, .batch-id { color: var(--td-text-color-secondary); overflow-wrap: anywhere; }
.entry-batch section { display: flex; flex-direction: column; gap: 12px; }
.entry-batch ul { margin: 0; padding-left: 20px; overflow-wrap: anywhere; }
.batch-results { max-height: 320px; overflow: auto; }
.batch-results li { padding: 8px 0; border-bottom: 1px solid var(--td-component-stroke); }
.batch-results span { margin-left: 12px; }
.batch-scope-summary { padding: 12px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-secondarycontainer); }
.batch-scope-count { font-weight: 600; }
.batch-scope-summary dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px 16px; margin: 0; }
.batch-scope-summary dl > div { min-width: 0; }
.batch-scope-summary dt { color: var(--td-text-color-secondary); font-size: 12px; }
.batch-scope-summary dd { margin: 2px 0 0; overflow-wrap: anywhere; }
@media (max-width: 560px) { .batch-scope-summary dl { grid-template-columns: minmax(0, 1fr); } }
.batch-footer { display: flex; justify-content: flex-end; flex-shrink: 0; }
</style>
