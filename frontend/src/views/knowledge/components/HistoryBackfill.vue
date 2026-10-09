<template>
  <section :class="['history-backfill', { 'history-backfill--compact': compact }]" :aria-label="t('knowledgeWorkbench.backfill.title', { name })">
    <EventsCoverageNotice v-if="showCoverage" :count="estimateValid ? estimate?.template_only_message_count ?? null : null" :loading="loading" />
    <p v-if="error" class="history-backfill__error" role="alert">{{ error }}</p>
    <div class="history-backfill__summary" aria-live="polite">
      <p v-if="estimateValid && estimate">{{ t('knowledgeWorkbench.backfill.estimate', { count: estimate.pending_message_count, calls: estimate.estimated_model_calls }) }}</p>
      <p v-else>{{ t(loading ? 'knowledgeWorkbench.backfill.loading' : 'knowledgeWorkbench.backfill.unavailable') }}</p>
      <div class="history-backfill__actions">
        <t-button v-if="enabled" variant="outline" size="small" :loading="starting" :disabled="loading || !estimateValid || !estimate?.batch_message_count || activeBatch" @click="confirmStart">
          {{ t('knowledgeWorkbench.backfill.start') }}
        </t-button>
        <t-button v-if="enabled" variant="text" size="small" :disabled="starting" @click="openResults">{{ t('knowledgeWorkbench.backfill.results') }}</t-button>
        <t-button v-if="error" variant="text" size="small" :loading="loading" :disabled="starting" @click="refresh">{{ t('knowledgeDomains.retry') }}</t-button>
      </div>
    </div>
    <template v-if="estimateValid && estimate && !compact">
      <p>{{ t('knowledgeWorkbench.backfill.batchLimit', { count: estimate.batch_message_count, remaining: estimate.remaining_message_count }) }}</p>
      <p>{{ t('knowledgeWorkbench.backfill.callEstimateHint') }}</p>
      <p v-if="!enabled">{{ t('knowledgeWorkbench.backfill.starFirst') }}</p>
    </template>
    <p v-if="activeBatch" role="status">{{ t('knowledgeWorkbench.backfill.inProgress') }}</p>
    <p v-if="latestBatch" class="history-backfill__latest" role="status">{{ batchSummary(latestBatch) }}</p>
    <t-dialog v-model:visible="resultsVisible" :header="t('knowledgeWorkbench.backfill.title', { name })" :footer="false" width="min(680px, 95vw)">
      <div class="history-backfill__results">
        <div class="history-backfill__results-heading">
          <p>{{ t('knowledgeWorkbench.backfill.resultsHint') }}</p>
          <t-button variant="text" :loading="loading" :disabled="starting" @click="refresh">{{ t('knowledgeWorkbench.refresh') }}</t-button>
        </div>
        <p v-if="error" class="history-backfill__error" role="alert">{{ error }}</p>
        <t-empty v-if="!batches.length && !loading" :description="t('knowledgeWorkbench.backfill.noBatches')" />
        <article v-for="batch in batches" :key="batch.id" class="history-backfill__batch">
          <div class="history-backfill__batch-heading">
            <strong>{{ formatDate(batch.created_at) }}</strong>
            <t-tag :theme="batch.status === 'completed' ? 'success' : ['failed', 'completed_with_failures'].includes(batch.status) ? 'warning' : 'default'" variant="light">{{ statusLabel(batch.status) }}</t-tag>
          </div>
          <p>{{ t('knowledgeWorkbench.backfill.batchCounts', { total: batch.message_count, success: batch.success_count, failure: batch.failure_count }) }}</p>
          <t-button variant="text" size="small" :loading="detailsLoading && selectedBatchId === batch.id" @click="loadBatchDetails(batch.id)">{{ t('knowledgeWorkbench.backfill.messageResults') }}</t-button>
          <p v-if="detailsError && selectedBatchId === batch.id" class="history-backfill__error" role="alert">{{ detailsError }}</p>
          <ul v-if="selectedBatchId === batch.id && selectedBatch?.items?.some(item => item.error)" class="history-backfill__failures">
            <li v-for="item in selectedBatch.items.filter(item => item.error)" :key="item.evidence_id">
              <t-button variant="text" size="small" @click="emit('open-source', item.knowledge_id)">{{ t('knowledgeWorkbench.evidence.openKnowledge') }}</t-button>
              <span>{{ item.error }}</span>
            </li>
          </ul>
        </article>
      </div>
    </t-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useConfirmDelete } from '@/components/settings/useConfirmDelete'
import { getKnowledgeBackfillBatch, getKnowledgeBackfillEstimate, listKnowledgeBackfillBatches, startKnowledgeBackfill, type KnowledgeBackfillBatch, type KnowledgeBackfillEstimate, type KnowledgeBackfillTarget } from '@/api/knowledge-context'
import EventsCoverageNotice from './EventsCoverageNotice.vue'

const props = withDefaults(defineProps<{ kind: 'entity' | 'topic'; targetId: string; name: string; compact?: boolean; enabled?: boolean; showCoverage?: boolean }>(), { compact: false, enabled: true, showCoverage: false })
const emit = defineEmits<{ changed: []; 'open-source': [knowledgeId: string]; count: [count: number] }>()
const { t, locale } = useI18n()
const confirmBackfill = useConfirmDelete()
const estimate = ref<KnowledgeBackfillEstimate | null>(null)
const estimateValid = ref(false)
const batches = ref<KnowledgeBackfillBatch[]>([])
const loading = ref(false)
const starting = ref(false)
const error = ref('')
const resultsVisible = ref(false)
const selectedBatchId = ref('')
const selectedBatch = ref<KnowledgeBackfillBatch | null>(null)
const detailsLoading = ref(false)
const detailsError = ref('')
const activeBatch = computed(() => batches.value.some(batch => ['pending', 'processing'].includes(batch.status)))
const latestBatch = computed(() => batches.value[0])
const target = computed<KnowledgeBackfillTarget>(() => props.kind === 'entity' ? { entity_id: props.targetId } : { topic_id: props.targetId })
let request = 0
let detailsRequest = 0
let pollTimer: ReturnType<typeof setTimeout> | null = null
let confirmation: ReturnType<typeof confirmBackfill> | null = null

function statusLabel(status: string) {
  return ['pending', 'processing', 'completed', 'completed_with_failures', 'failed'].includes(status) ? t(`knowledgeWorkbench.backfill.status.${status}`) : status
}
function formatDate(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString(locale.value)
}
function batchSummary(batch: KnowledgeBackfillBatch) {
  return t('knowledgeWorkbench.backfill.latest', { status: statusLabel(batch.status), total: batch.message_count, success: batch.success_count, failure: batch.failure_count })
}
function clearPolling() {
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = null
}
function updatePolling() {
  clearPolling()
  if (activeBatch.value) pollTimer = setTimeout(() => { void refresh() }, 5000)
}
function getError(cause: unknown) {
  if (cause && typeof cause === 'object' && 'message' in cause && typeof cause.message === 'string' && cause.message) return cause.message
  return t('knowledgeWorkbench.requestFailed')
}
function data<T>(response: { success: boolean; data: T; error?: string }): T {
  if (!response.success) throw new Error(response.error || t('knowledgeWorkbench.requestFailed'))
  return response.data
}
async function refresh() {
  if (starting.value) return
  const current = ++request
  const activeIds = new Set(batches.value.filter(batch => ['pending', 'processing'].includes(batch.status)).map(batch => batch.id))
  loading.value = true
  estimateValid.value = false
  error.value = ''
  clearPolling()
  try {
    const [estimateResponse, batchResponse] = await Promise.allSettled([getKnowledgeBackfillEstimate(target.value), listKnowledgeBackfillBatches(target.value)])
    if (current !== request) return
    const failures: string[] = []
    try {
      if (estimateResponse.status === 'rejected') throw estimateResponse.reason
      estimate.value = data(estimateResponse.value)
      estimateValid.value = true
      emit('count', estimate.value.template_only_message_count)
    } catch (cause) { failures.push(getError(cause)) }
    try {
      if (batchResponse.status === 'rejected') throw batchResponse.reason
      batches.value = data(batchResponse.value) || []
      if (batches.value.some(batch => activeIds.has(batch.id) && ['completed', 'completed_with_failures', 'failed'].includes(batch.status))) emit('changed')
    } catch (cause) { failures.push(getError(cause)) }
    error.value = [...new Set(failures)].join(' ')
    updatePolling()
    if (resultsVisible.value && selectedBatchId.value) await loadBatchDetails(selectedBatchId.value)
  } catch (cause) { if (current === request) error.value = getError(cause) }
  finally { if (current === request) loading.value = false }
}
function confirmStart() {
  if (!props.enabled || loading.value || starting.value || !estimateValid.value || activeBatch.value || !estimate.value?.batch_message_count || confirmation) return
  const confirmedTarget = target.value
  const confirmedRequest = request
  confirmation = confirmBackfill({
    title: t('knowledgeWorkbench.backfill.title', { name: props.name }),
    body: t('knowledgeWorkbench.backfill.confirm', { name: props.name, count: estimate.value.batch_message_count, calls: estimate.value.estimated_model_calls, remaining: estimate.value.remaining_message_count }),
    confirmText: t('knowledgeWorkbench.backfill.start'),
    onClose: () => { confirmation = null },
    onConfirm: async () => {
      confirmation = null
      if (confirmedRequest !== request || !props.enabled) return
      await start(confirmedTarget)
    },
  })
}
async function start(confirmedTarget: KnowledgeBackfillTarget) {
  if (starting.value) return
  const current = ++request
  clearPolling()
  starting.value = true
  error.value = ''
  let queued = false
  try {
    const batch = data(await startKnowledgeBackfill(confirmedTarget))
    if (current !== request) return
    batches.value = [batch, ...batches.value.filter(item => item.id !== batch.id)]
    resultsVisible.value = true
    selectedBatchId.value = batch.id
    MessagePlugin.success(t('knowledgeWorkbench.backfill.queued'))
    emit('changed')
    queued = true
  } catch (cause) { if (current === request) error.value = getError(cause) }
  finally {
    if (current === request) {
      starting.value = false
      if (queued) await refresh()
    }
  }
}
async function openResults() {
  resultsVisible.value = true
  await refresh()
  if (!selectedBatchId.value && batches.value[0]) await loadBatchDetails(batches.value[0].id)
}
async function loadBatchDetails(id: string) {
  const current = ++detailsRequest
  selectedBatchId.value = id
  selectedBatch.value = null
  detailsLoading.value = true
  detailsError.value = ''
  try {
    const batch = data(await getKnowledgeBackfillBatch(id))
    if (current === detailsRequest) selectedBatch.value = batch
  } catch (cause) { if (current === detailsRequest) detailsError.value = getError(cause) }
  finally { if (current === detailsRequest) detailsLoading.value = false }
}
function dismissConfirmation() {
  confirmation?.hide()
  confirmation = null
}
defineExpose({ refresh })
watch(() => [props.kind, props.targetId], () => {
  request++
  detailsRequest++
  dismissConfirmation()
  clearPolling()
  estimate.value = null
  estimateValid.value = false
  batches.value = []
  starting.value = false
  resultsVisible.value = false
  selectedBatchId.value = ''
  selectedBatch.value = null
  detailsLoading.value = false
  detailsError.value = ''
  void refresh()
}, { immediate: true })
onBeforeUnmount(() => { request++; detailsRequest++; dismissConfirmation(); clearPolling() })
</script>

<style scoped lang="less">
.history-backfill { margin: 12px 0; color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.history-backfill p { margin: 5px 0; }
.history-backfill__summary, .history-backfill__actions, .history-backfill__results-heading, .history-backfill__batch-heading { display: flex; gap: 8px; align-items: center; justify-content: space-between; flex-wrap: wrap; }
.history-backfill__actions { justify-content: flex-start; }
.history-backfill__error { color: var(--td-error-color); }
.history-backfill__latest { color: var(--td-text-color-primary); }
.history-backfill--compact { width: 100%; margin: 0 0 10px; }
.history-backfill__results { max-height: 65vh; overflow-y: auto; min-height: 120px; }
.history-backfill__batch { padding: 12px 0; border-top: 1px solid var(--td-component-stroke); }
.history-backfill__failures { padding-left: 20px; }
.history-backfill__failures li { margin: 6px 0; }
</style>
