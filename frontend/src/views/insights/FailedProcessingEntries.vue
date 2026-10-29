<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, DataTable, EmptyState, ErrorState, LibMark, Tag, confirm, toast } from '@/components/ui'
import type { ConfirmHandle } from '@/components/ui/feedback'
import EntryBatchDialog from '@/components/EntryBatchDialog.vue'
import { listEntryFiles, reparseEntry, type LibraryCatalogItem } from '@/api/library'
import { entryProcessingState } from '@/utils/entryProcessingStatus'
import { entryDetailHref } from '@/utils/bodyReference'
import { CONTENT_TYPES, type ClassifiedEntry } from '@/types/entryClassification'

interface FailedEntry extends ClassifiedEntry { id: string; title?: string; file_name?: string; parse_status?: string; updated_at?: string }
const props = defineProps<{ libraries: LibraryCatalogItem[]; libraryId: string }>()
const { t, locale } = useI18n()
const items = ref<FailedEntry[]>([]), total = ref(0), page = ref(1), loading = ref(false), error = ref('')
const focused = ref(''), retrying = ref(''), batchOpen = ref(false)
const pageSize = 20
let generation = 0, disposed = false, confirmation: ConfirmHandle | undefined
const selection = computed(() => ({ filters: { parse_status: 'failed', ...(props.libraryId ? { libraries: [props.libraryId] } : {}) } }))
const columns = computed(() => [
  { key: 'id', label: t('entries.columns.id') }, { key: 'title', label: t('entries.columns.title') },
  { key: 'stage', label: t('ingestDiagnostics.stage') }, { key: 'reason', label: t('ingestDiagnostics.reason') },
  { key: 'time', label: t('ingestDiagnostics.updatedAt') }, { key: 'action', label: t('entityPage.actions') },
])
const library = (entry: FailedEntry) => props.libraries.find(item => item.id === entry.library_id)
const libraryName = (entry: FailedEntry) => library(entry)?.name || entry.library_name || (entry.library_id ? shortID(entry.library_id) : '')
const managementHref = computed(() => '/platform/entries?' + new URLSearchParams({ parse_status: 'failed', ...(props.libraryId ? { libraries: props.libraryId } : {}) }).toString())
const title = (entry: FailedEntry) => entry.title || entry.file_name || t('library.untitledDocument')
const contentType = (value: string) => CONTENT_TYPES.includes(value as typeof CONTENT_TYPES[number]) ? t(`library.contentTypes.${value}`) : value
const shortID = (id: string) => id.length > 12 ? `${id.slice(0, 8)}…` : id
const stamp = (value: string) => new Date(value).toLocaleString(locale.value)
function errorMessage(cause: unknown, fallback = t('ingestDiagnostics.failedEntriesLoadFailed')) {
  const value = cause as { status?: number; code?: number; error?: { code?: number }; message?: string }
  const code = value?.error?.code ?? value?.code
  return value?.status === 404 || code === 1003 ? t('ingestDiagnostics.entryNotFound') : value?.status === 409 || code === 1005 ? t('ingestDiagnostics.entryChanged') : value?.message || fallback
}
async function refresh() {
  const ticket = ++generation
  loading.value = true; error.value = ''
  try {
    const response = await listEntryFiles('', { page: page.value, page_size: pageSize, ...selection.value.filters })
    if (!response.success) throw new Error(response.message || t('ingestDiagnostics.failedEntriesLoadFailed'))
    if (disposed || ticket !== generation) return
    items.value = response.data || []; total.value = response.total || 0
    const lastPage = Math.max(1, Math.ceil(total.value / pageSize))
    if (page.value > lastPage) { page.value = lastPage; void refresh() }
  } catch (cause) { if (!disposed && ticket === generation) error.value = errorMessage(cause) }
  finally { if (!disposed && ticket === generation) loading.value = false }
}
async function retry(entry: FailedEntry) {
  if (retrying.value) return
  retrying.value = entry.id
  const scope = props.libraryId
  confirmation = confirm({ header: t('ingestDiagnostics.retryTitle'), body: t('ingestDiagnostics.retryBody', { title: title(entry) }), confirmBtn: t('common.retry'), cancelBtn: t('common.cancel') })
  const accepted = await confirmation.result
  confirmation = undefined
  if (!accepted || disposed) { retrying.value = ''; return }
  try {
    const response = await reparseEntry(entry.id)
    if (!response.success) throw new Error(response.message || t('ingestDiagnostics.retryFailed'))
    if (disposed) return
    toast.success(t('ingestDiagnostics.retryQueued'))
    if (scope === props.libraryId) await refresh()
  } catch (cause) { if (!disposed) error.value = errorMessage(cause, t('ingestDiagnostics.retryFailed')) }
  finally { if (!disposed) retrying.value = '' }
}
watch(() => props.libraryId, () => { page.value = 1; items.value = []; total.value = 0; focused.value = ''; void refresh() }, { immediate: true })
onUnmounted(() => { disposed = true; generation++; confirmation?.destroy() })
defineExpose({ refresh })
</script>

<template>
  <section class="failed-entries" :aria-busy="loading">
    <header class="diagnostics-heading"><h2>{{ t('ingestDiagnostics.failedEntries') }} <span class="diagnostics-mono">{{ total.toLocaleString(locale) }}</span></h2><div class="diagnostics-actions"><a :href="managementHref">{{ t('ingestDiagnostics.materialManagement') }}</a><Button v-if="total" :disabled="loading || !!retrying" @click="batchOpen = true">{{ t('ingestDiagnostics.retryAll') }}</Button></div></header>
    <p class="diagnostics-muted">{{ t('ingestDiagnostics.processingHint') }}</p>
    <ErrorState v-if="error" :title="error"><Button :loading="loading" @click="refresh">{{ t('common.retry') }}</Button></ErrorState>
    <p v-if="loading && !items.length" role="status">{{ t('common.loading') }}</p>
    <DataTable v-if="items.length" class="failed-entry-table" :rows="items" :columns="columns" :label="t('ingestDiagnostics.failedEntries')" v-model:focused="focused">
      <template #cell-id="{ row }"><span class="diagnostics-mono" :title="row.id">{{ shortID(row.id) }}</span></template>
      <template #cell-title="{ row }"><a :href="entryDetailHref(row.id)" :title="row.id"><strong>{{ title(row) }}</strong></a><Tag v-if="row.content_type">{{ contentType(row.content_type) }}</Tag><small v-if="libraryName(row)" class="diagnostics-library failed-entry-library" :title="row.library_id"><LibMark v-if="library(row)" :color="library(row)!.color" />{{ libraryName(row) }}</small></template>
      <template #cell-stage="{ row }">{{ t(entryProcessingState(row).label) }}</template>
      <template #cell-reason="{ row }"><p class="diagnostics-reason">{{ row.error_message || t('ingestDiagnostics.noFailureReason') }}</p></template>
      <template #cell-time="{ row }"><time v-if="row.updated_at" class="diagnostics-nowrap" :datetime="row.updated_at">{{ stamp(row.updated_at) }}</time></template>
      <template #cell-action="{ row }"><Button variant="ghost" :disabled="!!retrying" :loading="retrying === row.id" @click="retry(row)">{{ t('common.retry') }}</Button></template>
    </DataTable>
    <EmptyState v-else-if="!loading && !error" :title="t('ingestDiagnostics.noFailedEntries')" />
    <nav v-if="total" class="diagnostics-pagination" :aria-label="t('entries.pagination')"><span>{{ t('ingestDiagnostics.rangeCount', { start: (page - 1) * pageSize + 1, end: Math.min(page * pageSize, total), total }) }}</span><Button :disabled="page <= 1 || loading || !!retrying" @click="page--; refresh()">{{ t('entries.previous') }}</Button><Button :disabled="page * pageSize >= total || loading || !!retrying" @click="page++; refresh()">{{ t('entries.next') }}</Button></nav>
    <EntryBatchDialog v-if="batchOpen" v-model:visible="batchOpen" :libraries="libraries" :selection="selection" :scope-label="t('ingestDiagnostics.failedEntries')" initial-action="reprocess" @changed="refresh" />
  </section>
</template>
