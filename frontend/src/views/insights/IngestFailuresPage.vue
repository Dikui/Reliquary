<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { listIngestFailures, getIngestFailure, readFailureText, readFailureImage, type IngestFailure, type FailureMaterial, type FailureQuery } from '@/api/ingest-diagnostics'

const { t, locale } = useI18n(), route = useRoute(), router = useRouter()
const pageSize = 20
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
const entryURL = (id: string) => `/platform/entries?entry_id=${encodeURIComponent(id)}`
const isImage = (material: FailureMaterial) => material.type.startsWith('image/')
function packageStatus(failure: IngestFailure) { return ['available', 'partial'].includes(failure.package_status) && new Date(failure.expires_at).getTime() <= Date.now() ? 'expired' : failure.package_status }
function readable(failure: IngestFailure) { return ['available', 'partial'].includes(packageStatus(failure)) }
function errorText(value: any, fallback: string) { return value?.status === 410 ? t('ingestDiagnostics.expiredMaterial') : value?.status === 404 ? t('ingestDiagnostics.notFound') : value?.message || fallback }
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
    for (const [key, value] of Object.entries(applied.value)) if (value) (query as any)[key] = key === 'from' || key === 'to' ? new Date(`${value}T00:00:00`).toISOString() : value
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
    if (ticket === detailVersion && !disposed) detail.value = response.data
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
  if (changed || listVersion === 0) void load()
}, { immediate: true })
watch(selectedID, () => { void loadDetail() }, { immediate: true })
watch(locale, () => { if (typeof document !== 'undefined') document.title = `${t('ingestDiagnostics.title')} · Reliquary` }, { immediate: true })
onUnmounted(() => { disposed = true; listVersion++; detailVersion++; releaseMaterial() })
</script>

<template>
  <main class="diagnostic-page">
    <header><h1>{{ t('ingestDiagnostics.diagnostics') }}</h1><nav class="diagnostic-shortcuts" :aria-label="t('ingestDiagnostics.diagnostics')"><router-link to="/platform/entries?parse_status=failed">{{ t('ingestDiagnostics.failedEntries') }}</router-link><router-link to="/platform/model-calls">{{ t('ikb19.callsTitle') }}</router-link><router-link to="/platform/settings?section=runtime-queues">{{ t('ingestDiagnostics.queues') }}</router-link><router-link to="/platform/settings?section=operation-logs">{{ t('ingestDiagnostics.logs') }}</router-link><router-link to="/platform/settings?section=vault">{{ t('ingestDiagnostics.vault') }}</router-link><router-link to="/platform/evaluation">{{ t('ikb19.evaluationTitle') }}</router-link></nav><p class="muted">{{ t('ingestDiagnostics.processingHint') }}</p></header>
    <section v-if="!selectedID" :aria-busy="loading">
      <h2>{{ t('ingestDiagnostics.title') }}</h2><p>{{ t('ingestDiagnostics.description') }}</p><p class="muted">{{ t('ingestDiagnostics.retention') }}</p>
      <form novalidate class="filter-panel" :aria-label="t('ingestDiagnostics.parameters')" @submit.prevent="apply()">
        <div class="filter-fields"><label><span>{{ t('ingestDiagnostics.from') }}</span><t-date-picker v-model="filters.from" clearable allow-input value-type="YYYY-MM-DD" @clear="apply()" /></label><label><span>{{ t('ingestDiagnostics.to') }}</span><t-date-picker v-model="filters.to" clearable allow-input value-type="YYYY-MM-DD" @clear="apply()" /></label><label v-for="field in fields" :key="field.key"><span>{{ field.label }}</span><t-input v-model="filters[field.key]" clearable @clear="apply()" /></label></div>
        <div class="toolbar"><t-button type="submit" :disabled="loading">{{ t('ingestDiagnostics.apply') }}</t-button><t-button variant="outline" :disabled="loading" @click="apply(true)">{{ t('ingestDiagnostics.reset') }}</t-button><t-button variant="outline" :loading="loading" @click="load">{{ t('ingestDiagnostics.refresh') }}</t-button></div>
      </form>
      <p class="muted">{{ t('ingestDiagnostics.timezone', { timezone }) }}</p>
      <div v-if="error" class="error" role="alert">{{ error }} <t-button variant="text" :disabled="loading" @click="load">{{ t('common.retry') }}</t-button></div>
      <t-loading :loading="loading"><ol v-if="items.length" class="failure-list"><li v-for="item in items" :key="item.id" class="failure-card"><div class="heading"><strong>{{ item.error_code }}</strong><time :datetime="item.created_at">{{ timestamp(item.created_at) }}</time></div><p>{{ item.reason }}</p><dl><dt>{{ t('ingestDiagnostics.failureId') }}</dt><dd><code>{{ item.id }}</code></dd><dt>{{ t('ingestDiagnostics.requestId') }}</dt><dd><code>{{ item.request_id }}</code></dd><dt>{{ t('ingestDiagnostics.apiKey') }}</dt><dd>{{ namedID(item.api_key_name, item.api_key_id) }}</dd><dt>{{ t('ingestDiagnostics.library') }}</dt><dd>{{ namedID(item.library, item.library_id) }}</dd><dt>{{ t('ingestDiagnostics.packageStatus') }}</dt><dd>{{ statuses[packageStatus(item)] || item.package_status }}</dd></dl><t-button variant="outline" @click="selectFailure(item.id)">{{ t('ingestDiagnostics.view') }}</t-button></li></ol><t-empty v-else-if="!loading && !error" :description="hasFilters ? t('ingestDiagnostics.noResults') : t('ingestDiagnostics.empty')" /></t-loading>
      <t-pagination v-if="total > 0" :current="page" :page-size="pageSize" :total="total" :show-page-size="false" @current-change="changePage" />
    </section>
    <section v-else :aria-busy="detailLoading"><div class="toolbar"><t-button variant="outline" @click="selectFailure()">{{ t('ingestDiagnostics.back') }}</t-button><t-button variant="outline" :loading="detailLoading" @click="loadDetail">{{ t('ingestDiagnostics.refresh') }}</t-button></div><h2>{{ t('ingestDiagnostics.detail') }}</h2>
      <div v-if="detailError" class="error" role="alert">{{ detailError }} <t-button variant="text" :disabled="detailLoading" @click="loadDetail">{{ t('common.retry') }}</t-button></div><t-loading :loading="detailLoading"><template v-if="detail"><p v-if="detail.outcome === 'unknown'" class="warning" role="status">{{ t('ingestDiagnostics.unknownOutcome') }}</p><p v-if="packageStatus(detail) === 'partial'" class="warning" role="status">{{ t('ingestDiagnostics.partialHint') }}</p><p v-else-if="packageStatus(detail) === 'expired'" class="warning" role="status">{{ t('ingestDiagnostics.expiredMaterial') }}</p>
        <dl class="detail-values"><dt>{{ t('ingestDiagnostics.failureId') }}</dt><dd>{{ detail.id }}</dd><dt>{{ t('ingestDiagnostics.requestId') }}</dt><dd>{{ detail.request_id }}</dd><dt>{{ t('ingestDiagnostics.createdAt') }}</dt><dd>{{ timestamp(detail.created_at) }}</dd><dt>{{ t('ingestDiagnostics.expiresAt') }}</dt><dd>{{ timestamp(detail.expires_at) }}</dd><dt>{{ t('ingestDiagnostics.transport') }}</dt><dd>{{ detail.transport }}</dd><dt>{{ t('ingestDiagnostics.apiKey') }}</dt><dd>{{ namedID(detail.api_key_name, detail.api_key_id) }}</dd><dt>{{ t('ingestDiagnostics.library') }}</dt><dd>{{ namedID(detail.library, detail.library_id) }}</dd><dt>{{ t('ingestDiagnostics.itemIndex') }}</dt><dd>{{ detail.item_index }}</dd><dt>{{ t('ingestDiagnostics.stage') }}</dt><dd>{{ detail.stage }}</dd><dt>{{ t('ingestDiagnostics.errorCode') }}</dt><dd>{{ detail.error_code }}</dd><dt>{{ t('ingestDiagnostics.reason') }}</dt><dd>{{ detail.reason }}</dd><dt>{{ t('ingestDiagnostics.retryable') }}</dt><dd>{{ detail.retryable ? t('ingestDiagnostics.yes') : t('ingestDiagnostics.no') }}</dd><dt>{{ t('ingestDiagnostics.outcome') }}</dt><dd>{{ outcomes[detail.outcome] || detail.outcome }}</dd><dt>{{ t('ingestDiagnostics.packageStatus') }}</dt><dd>{{ statuses[packageStatus(detail)] || detail.package_status }}</dd><dt v-if="detail.version">{{ t('ingestDiagnostics.version') }}</dt><dd v-if="detail.version">{{ detail.version }}</dd><dt v-if="detail.entry_id">{{ t('ingestDiagnostics.entry') }}</dt><dd v-if="detail.entry_id"><a :href="entryURL(detail.entry_id)">{{ detail.entry_id }}</a> · <router-link :to="{ path: '/platform/model-calls', query: { entry_id: detail.entry_id } }">{{ t('ingestDiagnostics.linkedCalls') }}</router-link></dd><dt v-if="detail.recovery_entry_id">{{ t('ingestDiagnostics.recoveryEntry') }}</dt><dd v-if="detail.recovery_entry_id"><a :href="entryURL(detail.recovery_entry_id)">{{ detail.recovery_entry_id }}</a></dd><dt v-if="detail.recovery_request_id">{{ t('ingestDiagnostics.recoveryRequest') }}</dt><dd v-if="detail.recovery_request_id">{{ detail.recovery_request_id }}</dd></dl>
        <section v-if="detail.config && Object.keys(detail.config).length"><h3>{{ t('ingestDiagnostics.conditions') }}</h3><pre>{{ JSON.stringify(detail.config, null, 2) }}</pre></section><section v-if="detail.missing?.length"><h3>{{ t('ingestDiagnostics.missing') }}</h3><ul><li v-for="reason in detail.missing" :key="reason">{{ reason }}</li></ul></section>
        <section><h3>{{ t('ingestDiagnostics.materials') }}</h3><ul class="material-list"><li v-for="material in detail.materials" :key="material.id"><span>{{ material.name }} · {{ material.type }} · {{ material.size.toLocaleString(locale) }} B</span><t-button variant="outline" :disabled="!readable(detail) || materialLoading" @click="readMaterial(material)">{{ t('ingestDiagnostics.read') }}</t-button></li></ul><p v-if="!detail.materials?.length" class="muted">{{ t('ingestDiagnostics.noMaterials') }}</p></section>
        <section v-if="selectedMaterial" class="material-view" :aria-label="t('ingestDiagnostics.material')" :aria-busy="materialLoading"><div class="heading"><h3>{{ selectedMaterial.name }}</h3><t-button variant="text" @click="releaseMaterial">{{ t('ingestDiagnostics.closeMaterial') }}</t-button></div><div v-if="materialError" class="error" role="alert">{{ materialError }} <t-button variant="text" :disabled="materialLoading" @click="readMaterial(selectedMaterial!, readLength > 0)">{{ t('common.retry') }}</t-button></div><t-loading :loading="materialLoading"><img v-if="imageURL" :src="imageURL" :alt="t('ingestDiagnostics.imageAlt', { name: selectedMaterial.name })" @error="materialError = t('ingestDiagnostics.materialFailed')" /><template v-else-if="!isImage(selectedMaterial)"><pre>{{ materialText }}</pre><p class="muted">{{ t('ingestDiagnostics.range', { read: readLength.toLocaleString(locale), total: totalLength.toLocaleString(locale) }) }}</p><t-button v-if="nextOffset !== null" variant="outline" :loading="materialLoading" @click="readMaterial(selectedMaterial!, true)">{{ t('ingestDiagnostics.more') }}</t-button></template></t-loading></section>
      </template></t-loading>
    </section>
  </main>
</template>

<style scoped lang="less">
.diagnostic-page { flex: 1; min-width: 0; min-height: 0; overflow: auto; padding: 28px; box-sizing: border-box; color: var(--td-text-color-primary); }
h1 { font-size: 24px; margin: 0 0 16px; } h2 { font-size: 20px; } h3 { font-size: 16px; }
p { line-height: 1.7; overflow-wrap: anywhere; } .muted, time, dt { color: var(--td-text-color-secondary); font-size: 13px; }
.diagnostic-shortcuts, .toolbar, .heading { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; }.heading { justify-content: space-between; }.toolbar { margin-top: 16px; }
a { color: var(--td-text-color-link); } a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.filter-panel, .failure-card, .material-view { min-width: 0; padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); }
.filter-fields { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(220px, 100%), 1fr)); gap: 14px; }.filter-fields label { display: grid; gap: 6px; min-width: 0; font-size: 13px; }.filter-fields :deep(.t-date-picker) { width: 100%; }
.failure-list { display: grid; gap: 16px; padding: 0; margin: 20px 0; list-style: none; }dl { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: 8px 16px; line-height: 1.7; }dd { margin: 0; overflow-wrap: anywhere; }.detail-values { padding-block: 8px; }
.error, .warning { padding: 12px; border-radius: var(--td-radius-small); overflow-wrap: anywhere; }.error { color: var(--td-error-color); background: var(--td-error-color-light); }.warning { color: var(--td-warning-color); background: var(--td-warning-color-light); }
.material-list { list-style: none; padding: 0; }.material-list li { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin-bottom: 12px; overflow-wrap: anywhere; }.material-list span { min-width: 0; }
pre { max-height: 420px; overflow: auto; padding: 12px; background: var(--td-bg-color-secondarycontainer); border-radius: var(--td-radius-small); font-family: var(--app-font-family-mono); font-size: 12px; line-height: 1.7; white-space: pre-wrap; overflow-wrap: anywhere; }.material-view img { display: block; max-width: 100%; max-height: 600px; object-fit: contain; }
@media (max-width: 640px) { .diagnostic-page { padding: 16px; } .filter-panel, .failure-card, .material-view { padding: 14px; } dl { grid-template-columns: minmax(0, 1fr); gap: 4px; } dd { margin-bottom: 8px; } }
</style>
