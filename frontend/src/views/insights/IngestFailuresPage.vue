<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { listIngestFailures, getIngestFailure, readFailureText, readFailureImage, type IngestFailure, type FailureMaterial, type FailureQuery } from '@/api/ingest-diagnostics'

import { Button, DataTable, Drawer, EmptyState, ErrorState, Field, Input, LibMark, Select, Tabs, Tag } from '@/components/ui'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'
import { entryDetailHref } from '@/utils/bodyReference'
import FailedProcessingEntries from './FailedProcessingEntries.vue'

const { t, locale } = useI18n(), route = useRoute(), router = useRouter()
const pageSize = 20
const libraries = ref<LibraryCatalogItem[]>([])
const focused = ref(''), materialTab = ref('parameters')
const failedEntries = ref<InstanceType<typeof FailedProcessingEntries>>()
const emptyFilters = () => ({ from: '', to: '', api_key_id: '', library_id: '', error_code: '', request_id: '', id: '' })
const filters = ref(emptyFilters()), applied = ref(emptyFilters()), page = ref(1)
const items = ref<IngestFailure[]>([]), total = ref(0), loading = ref(false), error = ref('')
const detail = ref<IngestFailure | null>(null), detailLoading = ref(false), detailError = ref('')
const selectedMaterial = ref<FailureMaterial | null>(null), materialText = ref(''), imageURL = ref(''), materialLoading = ref(false), materialError = ref('')
const nextOffset = ref<number | null>(null), readLength = ref(0), totalLength = ref(0)
let listVersion = 0, detailVersion = 0, materialVersion = 0, disposed = false
const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
const selectedID = computed(() => typeof route.query.failure === 'string' ? route.query.failure : '')
const fields = computed(() => [
  { key: 'api_key_id' as const, label: t('ingestDiagnostics.apiKey') },
  { key: 'library_id' as const, label: t('ingestDiagnostics.library') },
  { key: 'error_code' as const, label: t('ingestDiagnostics.errorCode') },
  { key: 'request_id' as const, label: t('ingestDiagnostics.requestId') },
  { key: 'id' as const, label: t('ingestDiagnostics.failureId') },
])
const hasFilters = computed(() => Object.values(applied.value).some(Boolean))
const statuses = computed<Record<string, string>>(() => ({ available: t('ingestDiagnostics.available'), partial: t('ingestDiagnostics.partial'), unavailable: t('ingestDiagnostics.unavailable'), expired: t('ingestDiagnostics.expired') }))
const outcomes = computed<Record<string, string>>(() => ({ rolled_back: t('ingestDiagnostics.rolledBack'), unknown: t('ingestDiagnostics.unknown') }))
const timestamp = (value: string) => new Date(value).toLocaleString(locale.value, { timeZoneName: 'short' })
const namedID = (name?: string, id?: string) => name && id ? `${name} (${id})` : name || id || '—'
const entryURL = entryDetailHref
const shortID = (id: string) => id.length > 12 ? `${id.slice(0, 8)}…` : id
const columns = computed(() => ['createdAt', 'requestId', 'library', 'stage', 'reason', 'outcome', 'retryable', 'packageStatus', 'view'].map(key => ({ key, label: t(`ingestDiagnostics.${key}`) })))
const materialGroups = computed(() => {
  const materials = detail.value?.materials || []
  return { parameters: materials.filter(m => m.source === 'request' || m.name === 'input.json'), input: materials.filter(m => m.source !== 'request' && m.name !== 'input.json' && m.source !== 'model_call' && m.source !== 'log'), logs: materials.filter(m => m.source === 'model_call' || m.source === 'log') }
})
const tabs = computed(() => [
  ...(materialGroups.value.parameters.length ? [{ value: 'parameters', label: t('ingestDiagnostics.requestParameters') }] : []),
  ...(materialGroups.value.input.length ? [{ value: 'input', label: t('ingestDiagnostics.originalInput') }] : []),
  ...(detail.value?.version || Object.keys(detail.value?.config || {}).length ? [{ value: 'conditions', label: t('ingestDiagnostics.conditions') }] : []),
  ...(materialGroups.value.logs.length ? [{ value: 'logs', label: t('ingestDiagnostics.logs') }] : []),
])
const tabMaterials = (tab: string) => materialGroups.value[tab as keyof typeof materialGroups.value] || []
const libraryName = (item: IngestFailure) => libraries.value.find(lib => lib.id === item.library_id)?.name || item.library || (item.library_id ? shortID(item.library_id) : '')
const libraryColor = (item: IngestFailure) => libraries.value.find(lib => lib.id === item.library_id)?.color
const isImage = (material: FailureMaterial) => material.type.startsWith('image/')
function packageStatus(failure: IngestFailure) { return ['available', 'partial'].includes(failure.package_status) && new Date(failure.expires_at).getTime() <= Date.now() ? 'expired' : failure.package_status }
function readable(failure: IngestFailure) { return ['available', 'partial'].includes(packageStatus(failure)) }
function errorText(value: unknown, fallback: string) {
  const error = value as { status?: number; code?: number; error?: { code?: number }; message?: string } | null
  const code = error?.error?.code ?? error?.code
  return error?.status === 410 ? t('ingestDiagnostics.expiredMaterial') : error?.status === 404 || code === 1003 ? t('ingestDiagnostics.notFound') : error?.message || fallback
}
function releaseMaterial() {
  materialVersion++; selectedMaterial.value = null; materialText.value = ''; materialError.value = ''; materialLoading.value = false
  nextOffset.value = null; readLength.value = 0; totalLength.value = 0
  if (imageURL.value) URL.revokeObjectURL(imageURL.value)
  imageURL.value = ''
}
async function load() {
  const ticket = ++listVersion
  loading.value = true; error.value = ''
  try {
    const query: FailureQuery = { page: page.value, page_size: pageSize }
    for (const key of Object.keys(applied.value) as (keyof ReturnType<typeof emptyFilters>)[]) {
      const value = applied.value[key]
      if (value) query[key] = key === 'from' || key === 'to' ? new Date(`${value}T00:00:00`).toISOString() : value
    }
    const response = await listIngestFailures(query)
    if (!response.success) throw new Error(response.message || t('ingestDiagnostics.loadFailed'))
    if (ticket !== listVersion || disposed) return
    items.value = response.data.items || []; total.value = response.data.total || 0
    const lastPage = Math.max(1, Math.ceil(total.value / pageSize))
    if (page.value > lastPage) void changePage(lastPage)
  } catch (e) { if (ticket === listVersion && !disposed) error.value = errorText(e, t('ingestDiagnostics.loadFailed')) }
  finally { if (ticket === listVersion && !disposed) loading.value = false }
}
async function loadDetail() {
  const ticket = ++detailVersion, id = selectedID.value
  releaseMaterial(); detail.value = null; detailError.value = ''; detailLoading.value = !!id
  if (!id) return
  try {
    const response = await getIngestFailure(id)
    if (!response.success) throw new Error(response.message || t('ingestDiagnostics.detailFailed'))
    if (ticket === detailVersion && !disposed) { detail.value = response.data; materialTab.value = tabs.value[0]?.value || '' }
  } catch (e) { if (ticket === detailVersion && !disposed) detailError.value = errorText(e, t('ingestDiagnostics.detailFailed')) }
  finally { if (ticket === detailVersion && !disposed) detailLoading.value = false }
}
async function readMaterial(material: FailureMaterial, more = false) {
  const failure = detail.value
  if (!failure || materialLoading.value || !readable(failure)) return
  if (!more) releaseMaterial()
  selectedMaterial.value = material
  const ticket = ++materialVersion, offset = more ? nextOffset.value : 0
  if (offset === null) return
  materialLoading.value = true; materialError.value = ''
  try {
    if (isImage(material)) {
      const blob = await readFailureImage(failure.id, material.id)
      if (ticket === materialVersion && !disposed) imageURL.value = URL.createObjectURL(blob)
    } else {
      const response = await readFailureText(failure.id, material.id, offset)
      if (!response.success) throw new Error(response.message || t('ingestDiagnostics.materialFailed'))
      if (ticket !== materialVersion || disposed) return
      materialText.value += response.data.content || ''; nextOffset.value = response.data.next_offset ?? null
      readLength.value = response.data.offset + response.data.length; totalLength.value = response.data.total_length
    }
  } catch (e) { if (ticket === materialVersion && !disposed) materialError.value = errorText(e, t('ingestDiagnostics.materialFailed')) }
  finally { if (ticket === materialVersion && !disposed) materialLoading.value = false }
}
function clearFilter(key: keyof ReturnType<typeof emptyFilters>, event: MouseEvent) {
  const input = (event.currentTarget as HTMLElement).parentElement?.querySelector('input')
  filters.value[key] = ''; apply(); input?.focus()
}
function apply(reset = false) {
  if (reset) filters.value = emptyFilters()
  if (filters.value.from && filters.value.to && filters.value.from >= filters.value.to) { error.value = t('ingestDiagnostics.invalidRange'); return }
  const query = { ...route.query }
  for (const key of Object.keys(emptyFilters())) delete query[key]
  delete query.page; delete query.failure
  for (const [key, value] of Object.entries(filters.value)) if (value.trim()) query[key] = value.trim()
  if (JSON.stringify(query) === JSON.stringify(route.query)) { applied.value = { ...filters.value }; void load() }
  else void router.replace({ query })
}
function changePage(value: number) { return router.replace({ query: { ...route.query, page: value > 1 ? String(value) : undefined } }) }
function selectFailure(id?: string) { void router.push({ query: { ...route.query, failure: id } }) }
watch(() => route.query, () => {
  const next = emptyFilters()
  for (const key of Object.keys(next) as (keyof typeof next)[]) if (typeof route.query[key] === 'string') next[key] = route.query[key] as string
  const valuePage = Number(route.query.page), nextPage = Number.isInteger(valuePage) && valuePage > 0 ? valuePage : 1
  const changed = JSON.stringify(next) !== JSON.stringify(applied.value) || nextPage !== page.value
  filters.value = { ...next }; applied.value = next; page.value = nextPage
  if (changed || listVersion === 0) { items.value = []; total.value = 0; focused.value = ''; void load() }
}, { immediate: true })
watch(materialTab, releaseMaterial)
onMounted(async () => {
  try { const response = await listLibraries(); if (!disposed && response.success) libraries.value = response.data || [] } catch { /* Saved failure metadata remains readable without the catalog. */ }
})
watch(selectedID, () => { void loadDetail() }, { immediate: true })
watch(locale, () => { if (typeof document !== 'undefined') document.title = `${t('ingestDiagnostics.title')} · Reliquary` }, { immediate: true })
onUnmounted(() => { disposed = true; listVersion++; detailVersion++; releaseMaterial() })
</script>

<template>
  <main class="rq-ui diagnostics-page">
    <div class="diagnostics-wrap ingest-wrap">
      <header class="diagnostics-heading"><h1>{{ t('ingestDiagnostics.title') }}</h1><Button :loading="loading" @click="load(); failedEntries?.refresh()">{{ t('ingestDiagnostics.refresh') }}</Button></header>
      <p class="diagnostics-muted">{{ t('ingestDiagnostics.description') }} {{ t('ingestDiagnostics.retention') }}</p>
      <form novalidate class="diagnostics-filters" :aria-label="t('ingestDiagnostics.parameters')" @submit.prevent="apply()">
        <Field :label="t('ingestDiagnostics.from')"><Input v-model="filters.from" type="date" /></Field>
        <Field :label="t('ingestDiagnostics.to')"><Input v-model="filters.to" type="date" /></Field>
        <Field v-for="field in fields" :key="field.key" :label="field.label">
          <Select v-if="field.key === 'library_id' && libraries.length" v-model="filters.library_id">
            <option value="">{{ t('ikb18.all') }}</option><option v-for="lib in libraries" :key="lib.id" :value="lib.id">{{ lib.name }}</option>
            <option v-if="filters.library_id && !libraries.some(lib => lib.id === filters.library_id)" :value="filters.library_id">{{ filters.library_id }}</option>
          </Select>
          <div v-else class="diagnostics-input"><Input v-model="filters[field.key]" /><Button v-if="filters[field.key]" variant="ghost" :aria-label="t('common.clear') + ' ' + field.label" @click="clearFilter(field.key, $event)">×</Button></div>
        </Field>
        <div class="diagnostics-actions"><Button type="submit" variant="primary" :disabled="loading">{{ t('ingestDiagnostics.apply') }}</Button><Button :disabled="loading" @click="apply(true)">{{ t('ingestDiagnostics.reset') }}</Button></div>
      </form>
      <p class="diagnostics-muted diagnostics-small">{{ t('ingestDiagnostics.timezone', { timezone }) }}</p>
      <ErrorState v-if="error" :title="error"><Button :disabled="loading" @click="load">{{ t('common.retry') }}</Button></ErrorState>
      <section :aria-label="t('ingestDiagnostics.title')" :aria-busy="loading">
        <p v-if="loading && !items.length" role="status">{{ t('common.loading') }}</p>
        <DataTable v-if="items.length" class="failure-table" :rows="items" :columns="columns" :label="t('ingestDiagnostics.title')" v-model:focused="focused" @activate="selectFailure($event.id)">
          <template #cell-createdAt="{ row }"><time class="diagnostics-mono diagnostics-nowrap" :datetime="row.created_at">{{ timestamp(row.created_at) }}</time></template>
          <template #cell-requestId="{ row }"><span class="diagnostics-mono" :title="row.request_id">{{ shortID(row.request_id) }}</span></template>
          <template #cell-library="{ row }"><span class="diagnostics-library" :title="row.library_id"><LibMark v-if="libraryColor(row)" :color="libraryColor(row)!" />{{ libraryName(row) }}</span></template>
          <template #cell-stage="{ row }"><strong :title="row.error_code">{{ row.stage }}</strong></template>
          <template #cell-reason="{ row }"><p class="diagnostics-reason">{{ row.reason }}<code>{{ row.error_code }}</code></p></template>
          <template #cell-outcome="{ row }">{{ outcomes[row.outcome] || row.outcome }}</template>
          <template #cell-retryable="{ row }"><Tag :class="{ 'retry-yes': row.retryable }">{{ row.retryable ? t('ingestDiagnostics.yes') : t('ingestDiagnostics.no') }}</Tag></template>
          <template #cell-packageStatus="{ row }">{{ statuses[packageStatus(row)] || row.package_status }}</template>
          <template #cell-view="{ row }"><Button variant="ghost" @click="selectFailure(row.id)">{{ t('ingestDiagnostics.view') }}</Button></template>
        </DataTable>
        <EmptyState v-else-if="!loading && !error" :title="hasFilters ? t('ingestDiagnostics.noResults') : t('ingestDiagnostics.empty')" />
        <nav v-if="total > 0" class="diagnostics-pagination" :aria-label="t('entries.pagination')"><span>{{ t('ingestDiagnostics.rangeCount', { start: (page - 1) * pageSize + 1, end: Math.min(page * pageSize, total), total }) }}</span><Button :disabled="page <= 1 || loading" @click="changePage(page - 1)">{{ t('entries.previous') }}</Button><Button :disabled="page * pageSize >= total || loading" @click="changePage(page + 1)">{{ t('entries.next') }}</Button></nav>
      </section>
      <FailedProcessingEntries ref="failedEntries" :libraries="libraries" :library-id="applied.library_id" />
      <nav class="diagnostics-actions" :aria-label="t('ingestDiagnostics.diagnostics')"><router-link to="/platform/settings?section=runtime-queues">{{ t('ingestDiagnostics.queues') }}</router-link><router-link to="/platform/settings?section=operation-logs">{{ t('ingestDiagnostics.logs') }}</router-link><router-link to="/platform/settings?section=vault">{{ t('ingestDiagnostics.vault') }}</router-link></nav>
    </div>
    <Drawer :model-value="!!selectedID" :title="t('ingestDiagnostics.detail')" content-class="failure-drawer" @update:model-value="!$event && selectFailure()">
      <div class="failure-detail" :aria-busy="detailLoading">
        <Button :loading="detailLoading" @click="loadDetail">{{ t('ingestDiagnostics.refresh') }}</Button>
        <ErrorState v-if="detailError" :title="detailError"><Button @click="loadDetail">{{ t('common.retry') }}</Button></ErrorState>
        <p v-if="detailLoading && !detail" role="status">{{ t('common.loading') }}</p>
        <template v-if="detail">
          <p v-if="detail.outcome === 'unknown'" class="diagnostics-warning" role="status">{{ t('ingestDiagnostics.unknownOutcome') }}</p>
          <p v-if="packageStatus(detail) === 'partial'" class="diagnostics-warning" role="status">{{ t('ingestDiagnostics.partialHint') }}</p>
          <p v-else-if="packageStatus(detail) === 'expired'" class="diagnostics-warning" role="status">{{ t('ingestDiagnostics.expiredMaterial') }}</p>
          <dl class="diagnostics-kv">
            <dt>{{ t('ingestDiagnostics.failureId') }}</dt><dd class="diagnostics-mono">{{ detail.id }}</dd><dt>{{ t('ingestDiagnostics.requestId') }}</dt><dd class="diagnostics-mono">{{ detail.request_id }}</dd>
            <dt>{{ t('ingestDiagnostics.createdAt') }}</dt><dd>{{ timestamp(detail.created_at) }}</dd><dt>{{ t('ingestDiagnostics.expiresAt') }}</dt><dd>{{ timestamp(detail.expires_at) }}</dd>
            <dt>{{ t('ingestDiagnostics.transport') }}</dt><dd>{{ detail.transport }}</dd><dt>{{ t('ingestDiagnostics.apiKey') }}</dt><dd>{{ namedID(detail.api_key_name, detail.api_key_id) }}</dd>
            <dt>{{ t('ingestDiagnostics.library') }}</dt><dd>{{ namedID(libraryName(detail), detail.library_id) }}</dd><dt>{{ t('ingestDiagnostics.itemIndex') }}</dt><dd>{{ detail.item_index }}</dd>
            <dt>{{ t('ingestDiagnostics.stage') }}</dt><dd><strong>{{ detail.stage }}</strong></dd><dt>{{ t('ingestDiagnostics.errorCode') }}</dt><dd class="diagnostics-mono">{{ detail.error_code }}</dd><dt>{{ t('ingestDiagnostics.reason') }}</dt><dd>{{ detail.reason }}</dd>
            <dt>{{ t('ingestDiagnostics.retryable') }}</dt><dd>{{ detail.retryable ? t('ingestDiagnostics.yes') : t('ingestDiagnostics.no') }}</dd><dt>{{ t('ingestDiagnostics.outcome') }}</dt><dd>{{ outcomes[detail.outcome] || detail.outcome }}</dd><dt>{{ t('ingestDiagnostics.packageStatus') }}</dt><dd>{{ statuses[packageStatus(detail)] || detail.package_status }}</dd>
            <template v-if="detail.entry_id"><dt>{{ t('ingestDiagnostics.entry') }}</dt><dd><a :href="entryURL(detail.entry_id)" :title="detail.entry_id">{{ shortID(detail.entry_id) }}</a> · <router-link :to="{ path: '/platform/model-calls', query: { entry_id: detail.entry_id } }">{{ t('ingestDiagnostics.linkedCalls') }}</router-link></dd></template>
            <template v-if="detail.recovery_entry_id"><dt>{{ t('ingestDiagnostics.recoveryEntry') }}</dt><dd><a :href="entryURL(detail.recovery_entry_id)" :title="detail.recovery_entry_id">{{ shortID(detail.recovery_entry_id) }}</a></dd></template>
            <template v-if="detail.recovery_request_id"><dt>{{ t('ingestDiagnostics.recoveryRequest') }}</dt><dd>{{ detail.recovery_request_id }}</dd></template>
          </dl>
          <section v-if="detail.missing?.length"><h3>{{ t('ingestDiagnostics.missing') }}</h3><ul><li v-for="reason in detail.missing" :key="reason">{{ reason }}</li></ul></section>
          <Tabs v-if="tabs.length" v-model="materialTab" :items="tabs" :label="t('ingestDiagnostics.materials')">
            <template v-for="tab in tabs" :key="tab.value" #[tab.value]>
              <template v-if="tab.value === 'conditions'"><dl v-if="detail.version" class="diagnostics-kv"><dt>{{ t('ingestDiagnostics.version') }}</dt><dd>{{ detail.version }}</dd></dl><pre v-if="Object.keys(detail.config || {}).length">{{ JSON.stringify(detail.config, null, 2) }}</pre></template>
              <ul v-else class="failure-materials"><li v-for="material in tabMaterials(tab.value)" :key="material.id"><span>{{ material.name }}<small>{{ material.type }} · {{ material.size.toLocaleString(locale) }} B</small></span><Button :disabled="!readable(detail) || materialLoading" @click="readMaterial(material)">{{ t('ingestDiagnostics.read') }}</Button></li></ul>
            </template>
          </Tabs>
          <p v-if="!detail.materials?.length" class="diagnostics-muted">{{ t('ingestDiagnostics.noMaterials') }}</p>
          <section v-if="selectedMaterial" class="failure-material-view" :aria-label="t('ingestDiagnostics.material')" :aria-busy="materialLoading">
            <div class="diagnostics-heading"><h3>{{ selectedMaterial.name }}</h3><Button variant="ghost" @click="releaseMaterial">{{ t('ingestDiagnostics.closeMaterial') }}</Button></div>
            <p v-if="materialLoading" role="status">{{ t('common.loading') }}</p>
            <ErrorState v-if="materialError" :title="materialError"><Button :disabled="materialLoading" @click="readMaterial(selectedMaterial!, readLength > 0)">{{ t('common.retry') }}</Button></ErrorState>
            <img v-if="imageURL" :src="imageURL" :alt="t('ingestDiagnostics.imageAlt', { name: selectedMaterial.name })" @error="materialError = t('ingestDiagnostics.materialFailed')" />
            <template v-else-if="!isImage(selectedMaterial)"><pre v-if="materialText">{{ materialText }}</pre><p class="diagnostics-muted">{{ t('ingestDiagnostics.range', { read: readLength.toLocaleString(locale), total: totalLength.toLocaleString(locale) }) }}</p><Button v-if="nextOffset !== null" :loading="materialLoading" @click="readMaterial(selectedMaterial!, true)">{{ t('ingestDiagnostics.more') }}</Button></template>
          </section>
        </template>
      </div>
    </Drawer>
  </main>
</template>

<style src="./diagnostics.css"></style>
