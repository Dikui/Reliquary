<template>
  <div class="api-settings settings-form">
    <section v-for="endpoint in endpoints" :key="endpoint.label" class="setting-row">
      <div class="setting-info"><label>{{ endpoint.label }}</label><p class="desc">{{ endpoint.desc }}</p></div>
      <div class="setting-control"><code :title="`${endpoint.method ? endpoint.method + ' ' : ''}${endpoint.url}`">{{ endpoint.method }} {{ endpoint.url }}</code><Button variant="ghost" :aria-label="endpoint.copy" @click="copy(endpoint.url)">{{ t('integrations.api.copy') }}</Button></div>
    </section>
    <section class="setting-row"><div class="setting-info"><label>{{ t('integrations.api.apiKeys') }}</label><p class="desc">{{ t('integrations.api.apiKeysDesc') }}</p></div><div class="setting-control"><Button variant="primary" @click="openCreate">{{ t('integrations.api.createApiKey') }}</Button></div></section>
    <p v-if="loading" role="status">{{ t('integrations.api.loading') }}</p>
    <p v-else-if="error" class="settings-error" role="alert">{{ error }} <Button @click="load">{{ t('integrations.api.retry') }}</Button></p>
    <p v-else-if="!keys.length" role="status">{{ t('integrations.api.noApiKeys') }}</p>
    <template v-else>
      <DataTable v-model:focused="focusedKey" :rows="pagedKeys" :columns="columns" :label="t('integrations.api.apiKeys')">
        <template #cell-api_key="{ row }"><span v-if="row.status === 'migration_invalidated'">{{ t('integrations.api.migrationInvalidated') }}</span><code v-else>{{ row.api_key }}</code></template>
        <template #cell-expires_at="{ row }">{{ row.status === 'migration_invalidated' ? t('integrations.api.migrationInvalidated') : row.expires_at ? new Date(row.expires_at).toLocaleString() : t('integrations.api.apiKeyNeverExpires') }}</template>
        <template #cell-actions="{ row }"><div class="settings-actions"><Button v-if="row.status === 'migration_invalidated'" variant="ghost" @click="openCreate">{{ t('integrations.api.regenerateMigratedKey') }}</Button><Button variant="ghost" @click="openEdit(row)">{{ t('integrations.api.editApiKey') }}</Button><Button variant="danger" @click="revoke(row)">{{ t('integrations.api.apiKeyRevoke') }}</Button></div></template>
      </DataTable>
      <nav class="settings-actions" :aria-label="t('entries.pagination')"><label class="page-size-control">{{ t('common.pageSize') }}<Select :model-value="pagination.pageSize" :aria-label="t('common.pageSize')" @update:model-value="onPageChange({ current: 1, pageSize: Number($event) })"><option v-for="size in [5, 10, 20, 50]" :key="size" :value="size">{{ size }}</option></Select></label><Button :disabled="pagination.current <= 1" @click="pagination.current--">{{ t('entries.previous') }}</Button><span>{{ pagination.current }} / {{ Math.ceil(pagination.total / pagination.pageSize) }}</span><Button :disabled="pagination.current * pagination.pageSize >= pagination.total" @click="pagination.current++">{{ t('entries.next') }}</Button></nav>
    </template>
    <section class="setting-row"><div class="setting-info"><label>{{ t('integrations.api.playgroundTitle') }}</label><p class="desc">{{ t('integrations.api.playgroundDesc') }}</p></div><div class="setting-control"><Button @click="openPlayground">{{ t('integrations.api.playgroundOpen') }}</Button></div></section>
    <Dialog v-model="editingVisible" :title="editingId ? t('integrations.api.editApiKey') : t('integrations.api.createApiKey')" :closable="!saving" :close-on-overlay-click="!saving" :close-on-esc-keydown="!saving">
      <form novalidate class="settings-form" @submit.prevent="save">
        <Field :label="t('integrations.api.apiKeyName')"><Input v-model="form.name" :disabled="saving" required /></Field>
        <Field :label="t('integrations.api.apiKeyExpiresAt')"><div class="api-key-expiry-field"><Input v-model="form.expiresAt" class="api-key-expiry" type="datetime-local" :disabled="saving" :aria-label="t('integrations.api.apiKeyExpiresAt')" /><p class="api-key-expiry-hint">{{ t('integrations.api.apiKeyExpiresAtHint') }}</p></div></Field>
        <p v-if="formError" class="settings-error" role="alert">{{ formError }}</p>
      </form>
      <template #footer><Button :disabled="saving" @click="editingVisible = false">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" @click="save">{{ t('common.save') }}</Button></template>
    </Dialog>
    <Dialog v-model="createdVisible" :title="t('integrations.api.apiKeyCreatedTitle')">
      <p>{{ t('integrations.api.apiKeyCreatedHint') }}</p><div class="token-value"><Input :model-value="createdToken" type="password" readonly :aria-label="t('integrations.api.apiKeyValue')" /><Button @click="copy(createdToken)">{{ t('integrations.api.copy') }}</Button></div>
    </Dialog>
    <Dialog v-model="playgroundVisible" :title="t('integrations.api.playgroundTitle')">
      <form novalidate class="settings-form" @submit.prevent="runQuery">
        <Field :label="t('integrations.api.apiKeyValue')"><Input v-model="playground.apiKey" type="password" autocomplete="off" :disabled="running" /></Field>
        <Field :label="t('integrations.api.playgroundQuestion')"><Textarea v-model="playground.query" rows="4" :disabled="running" /></Field>
        <SearchFilters v-model="playgroundFilters" :options="searchOptions" :loading="searchOptionsLoading" :disabled="running" />
      </form>
      <p v-if="searchOptionsError" class="settings-error" role="alert">{{ searchOptionsError }} <Button @click="loadSearchOptions">{{ t('common.retry') }}</Button></p>
      <strong class="request-preview-label">{{ t('knowledgeSearch.requestPreview') }}</strong><pre class="query-result">{{ playgroundRequestPreview }}</pre>
      <p v-if="playgroundError" class="settings-error" role="alert">{{ playgroundError }}</p><pre v-if="playgroundResult" class="query-result">{{ playgroundResult }}</pre>
      <template #footer><Button variant="primary" :loading="running" @click="runQuery">{{ t('integrations.api.playgroundRun') }}</Button></template>
    </Dialog>
  </div>
</template>
<script setup lang="ts">
import { Button, Input, Select, Textarea, Dialog, DataTable, Field, toast, useConfirm } from '@/components/ui'
import '@/styles/settings-form.css'
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import SearchFilters from '@/components/SearchFilters.vue'
import { getSearchOptions } from '@/api/search'
import type { SearchOptions } from '@/types/search'
import { buildSearchRequest, searchErrorMessage, searchErrorCode, defaultSearchFilters } from '@/utils/search'
import { apiKeyErrorMessage } from '@/utils/personalServerError'
import { getApiBaseUrl } from '@/utils/api-base'
import { copyToClipboard } from '@/utils/clipboard'
import { listAPIKeys, createAPIKey, updateAPIKey, deleteAPIKey, type APIKey } from '@/api/space'

const confirm = useConfirm()
const { t } = useI18n()
const apiBaseUrl = computed(() => getApiBaseUrl().replace(/\/$/, '') || window.location.origin)
const externalAgentBaseUrl = computed(() => apiBaseUrl.value.startsWith('/') ? `${window.location.origin}${apiBaseUrl.value}` : apiBaseUrl.value)
const ingestUrl = computed(() => `${externalAgentBaseUrl.value}/api/v1/ingest`)
const mcpUrl = computed(() => `${externalAgentBaseUrl.value}/mcp`)
const endpoints = computed(() => [
  { label: t('integrations.api.baseUrl'), desc: t('integrations.api.baseUrlDesc'), url: apiBaseUrl.value, copy: t('integrations.api.copy'), method: '' },
  { label: t('integrations.api.ingestUrl'), desc: t('integrations.api.ingestUrlDesc'), url: ingestUrl.value, copy: t('integrations.api.copyIngestUrl'), method: 'POST' },
  { label: t('integrations.api.mcpUrl'), desc: t('integrations.api.mcpUrlDesc'), url: mcpUrl.value, copy: t('integrations.api.copyMcpUrl'), method: '' },
])
const focusedKey = ref('')
const keys = ref<APIKey[]>([])
const loading = ref(false)
const error = ref('')
const pagination = reactive({ current: 1, pageSize: 10, total: 0 })
const columns = computed(() => [
  { key: 'name', label: t('integrations.api.apiKeyName') },
  { key: 'api_key', label: t('integrations.api.apiKeyValue') },
  { key: 'expires_at', label: t('integrations.api.apiKeyExpiresAt') },
  { key: 'actions', label: t('common.operation') },
])
const pagedKeys = computed(() => keys.value.slice((pagination.current - 1) * pagination.pageSize, pagination.current * pagination.pageSize))
const editingVisible = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formError = ref('')
const form = reactive({ name: '', expiresAt: '' })
const createdVisible = ref(false)
const createdToken = ref('')
const playgroundVisible = ref(false)
const playground = reactive({ apiKey: '', query: '' })
const playgroundFilters = ref(defaultSearchFilters())
const playgroundLibraries = ref<string[]>([])
const searchOptions = ref<SearchOptions>({ entities: [], topics: [] })
const searchOptionsLoading = ref(false), searchOptionsError = ref('')
const playgroundRequestPreview = computed(() => JSON.stringify({ ...buildSearchRequest(playground.query, playgroundFilters.value), libraries: playgroundLibraries.value }, null, 2))
const running = ref(false)
const playgroundError = ref('')
const playgroundResult = ref('')

const copy = async (value: string) => { const copied = await copyToClipboard(value); if (copied) toast.success(t('common.copySuccess')); else toast.error(t('common.copyFailed')) }
function onPageChange(page: { current: number; pageSize: number }) {
  const pageSize = [5, 10, 20, 50].includes(page.pageSize) ? page.pageSize : pagination.pageSize
  Object.assign(pagination, { pageSize, current: Math.max(1, Math.min(page.current, Math.ceil(pagination.total / pageSize) || 1)) })
}
async function load() {
  loading.value = true
  error.value = ''
  try {
    const response = await listAPIKeys()
    if (!response.success) throw new Error(response.message || t('integrations.api.loadApiKeysFailed'))
    keys.value = response.data || []
    pagination.total = keys.value.length
    pagination.current = Math.min(pagination.current, Math.max(1, Math.ceil(keys.value.length / pagination.pageSize)))
  } catch (e: any) { error.value = apiKeyErrorMessage(e, t, 'integrations.api.loadApiKeysFailed') }
  finally { loading.value = false }
}
function openCreate() { editingId.value = null; form.name = ''; form.expiresAt = ''; formError.value = ''; editingVisible.value = true }
function openEdit(key: APIKey) {
  editingId.value = key.id
  form.name = key.name
  form.expiresAt = key.expires_at ? new Date(new Date(key.expires_at).getTime() - new Date(key.expires_at).getTimezoneOffset() * 60000).toISOString().slice(0, 16) : ''
  formError.value = ''
  editingVisible.value = true
}
async function save() {
  if (saving.value) return
  if (!form.name.trim()) { formError.value = t('integrations.api.apiKeyNameRequired'); return }
  const expires = form.expiresAt ? Math.floor(new Date(form.expiresAt).getTime() / 1000) : 0
  if (form.expiresAt && (!Number.isFinite(expires) || expires <= Date.now() / 1000)) { formError.value = t('integrations.api.apiKeyExpiresAtInvalid'); return }
  saving.value = true
  formError.value = ''
  try {
    const payload = { name: form.name.trim(), ...(expires ? { expires_at_unix: expires } : {}) }
    const response = editingId.value ? await updateAPIKey(editingId.value, payload) : await createAPIKey(payload)
    if (!response.success) throw new Error(response.message || t('integrations.api.createApiKeyFailed'))
    if (!editingId.value) { createdToken.value = (response.data as any)?.token || ''; createdVisible.value = true }
    editingVisible.value = false
    toast.success(t('integrations.api.updateApiKeySuccess'))
    await load()
  } catch (e: any) { formError.value = apiKeyErrorMessage(e, t, editingId.value ? 'integrations.api.updateApiKeyFailed' : 'integrations.api.createApiKeyFailed') }
  finally { saving.value = false }
}
function revoke(key: APIKey) {
  const dialog = confirm({
    header: t('integrations.api.apiKeyRevoke'), body: t('integrations.api.apiKeyRevokeConfirm', { name: key.name }),
    confirmBtn: t('integrations.api.apiKeyRevoke'), theme: 'danger',
    onConfirm: async () => {
      try { const response = await deleteAPIKey(key.id); if (!response.success) throw new Error(response.message); dialog.destroy(); await load(); toast.success(t('integrations.api.apiKeyRevoked')) }
      catch (e: any) { toast.error(apiKeyErrorMessage(e, t, 'integrations.api.deleteApiKeyFailed')) }
    },
  })
}
function openPlayground() { playgroundVisible.value = true; playgroundError.value = ''; void loadSearchOptions() }
async function loadSearchOptions() {
  searchOptionsLoading.value = true
  searchOptionsError.value = ''
  try {
    const response = await getSearchOptions(playgroundLibraries.value)
    if (!response.success) throw new Error(response.message)
    searchOptions.value = response.data
  } catch { searchOptionsError.value = t('knowledgeSearch.optionsFailed') }
  finally { searchOptionsLoading.value = false }
}
async function runQuery() {
  if (running.value) return
  if (!playground.apiKey.trim() || !playground.query.trim()) { playgroundError.value = t('integrations.api.playgroundNeedQuestionAndKey'); return }
  if (playgroundFilters.value.published_from && playgroundFilters.value.published_to && playgroundFilters.value.published_from > playgroundFilters.value.published_to) { playgroundError.value = t('knowledgeSearch.invalidDateRange'); return }
  if (!Number.isInteger(playgroundFilters.value.limit) || playgroundFilters.value.limit < 1 || playgroundFilters.value.limit > 100) { playgroundError.value = t('knowledgeSearch.invalidLimit'); return }
  running.value = true
  playgroundError.value = ''
  try {
    playgroundResult.value = ''
    playgroundLibraries.value = []
    const headers = { 'Content-Type': 'application/json', 'X-API-Key': playground.apiKey.trim() }
    const catalogResponse = await fetch(`${apiBaseUrl.value}/api/v1/libraries`, { headers })
    const catalog = await catalogResponse.json()
    if (!catalogResponse.ok || catalog.success === false) throw catalog
    playgroundLibraries.value = (catalog.data as Array<{ id: string }>).map(library => library.id)
    const response = await fetch(`${apiBaseUrl.value}/api/v1/search`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-API-Key': playground.apiKey.trim() }, body: playgroundRequestPreview.value })
    const body = await response.json()
    if (!response.ok || body.success === false) throw body
    playgroundResult.value = JSON.stringify(body, null, 2)
  } catch (e: any) { const code = searchErrorCode(e); playgroundError.value = `${code === undefined ? '' : `${code}: `}${searchErrorMessage(e, t)}` }
  finally { running.value = false }
}
onMounted(load)
</script>
<style scoped>
 .setting-control code { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.setting-control .rq-button { flex-shrink: 0; }
.api-key-expiry-field { display: flex; flex-direction: column; gap: var(--space-2); width: 100%; min-width: 0; }
.api-key-expiry { width: 100%; }
.api-key-expiry-hint { margin: 0; color: var(--muted); font-size: var(--text-sm); }
.token-value { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.token-value .rq-control { flex: 1; min-width: 160px; }
.query-result { white-space: pre-wrap; overflow-wrap: anywhere; max-height: 360px; overflow: auto; padding: var(--space-3); background: var(--surface-soft); color: var(--text); font-family: var(--mono); }
.page-size-control { display: flex; gap: var(--space-2); align-items: center; }
.page-size-control .rq-control { width: auto; }
.api-settings :deep(.rq-table) { min-width: 640px; }
.api-settings :deep(.rq-table th), .api-settings :deep(.rq-table td) { min-width: 120px; }

</style>
