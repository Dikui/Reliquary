<template>
  <div class="api-settings">
    <div class="setting-row">
      <div class="setting-info"><label>{{ t('integrations.api.baseUrl') }}</label><p>{{ t('integrations.api.baseUrlDesc') }}</p></div>
      <div class="setting-control"><code>{{ apiBaseUrl }}</code><t-button variant="text" :aria-label="t('integrations.api.copy')" @click="copy(apiBaseUrl)"><t-icon name="copy" /></t-button></div>
    </div>
    <div class="setting-row">
      <div class="setting-info"><label>{{ t('integrations.api.apiKeys') }}</label><p>{{ t('integrations.api.apiKeysDesc') }}</p></div>
      <t-button theme="primary" @click="openCreate">{{ t('integrations.api.createApiKey') }}</t-button>
    </div>
    <t-loading v-if="loading" :text="t('integrations.api.loading')" />
    <t-alert v-else-if="error" theme="error" :message="error"><template #operation><t-button variant="text" @click="load">{{ t('integrations.api.retry') }}</t-button></template></t-alert>
    <t-empty v-else-if="!keys.length" :description="t('integrations.api.noApiKeys')" />
    <t-table v-else row-key="id" :data="keys" :columns="columns" :pagination="pagination" @page-change="onPageChange">
      <template #api_key="{ row }"><code>{{ row.api_key }}</code></template>
      <template #expires_at="{ row }">{{ row.expires_at ? new Date(row.expires_at).toLocaleString() : t('integrations.api.apiKeyNeverExpires') }}</template>
      <template #actions="{ row }"><t-button variant="text" @click="openEdit(row)">{{ t('integrations.api.editApiKey') }}</t-button><t-button theme="danger" variant="text" @click="revoke(row)">{{ t('integrations.api.apiKeyRevoke') }}</t-button></template>
    </t-table>
    <div class="setting-row">
      <div class="setting-info"><label>{{ t('integrations.api.playgroundTitle') }}</label><p>{{ t('integrations.api.playgroundDesc') }}</p></div>
      <t-button variant="outline" @click="playgroundVisible = true">{{ t('integrations.api.playgroundOpen') }}</t-button>
    </div>
    <SettingDrawer v-model:visible="editingVisible" :title="editingId ? t('integrations.api.editApiKey') : t('integrations.api.createApiKey')" :confirm-text="t('common.save')" :confirm-loading="saving" storage-key="setting-drawer:width:api-key" width="560px" @confirm="save">
      <t-form ref="formRef" :data="form" :rules="rules" layout="vertical" label-align="top" @submit.prevent>
        <t-form-item :label="t('integrations.api.apiKeyName')" name="name"><t-input v-model="form.name" :disabled="saving" /></t-form-item>
        <t-form-item :label="t('integrations.api.apiKeyExpiresAt')" name="expiresAt"><t-input v-model="form.expiresAt" type="datetime-local" :disabled="saving" /><p>{{ t('integrations.api.apiKeyExpiresAtHint') }}</p></t-form-item>
        <t-alert v-if="formError" theme="error" :message="formError" />
      </t-form>
    </SettingDrawer>
    <SettingDrawer v-model:visible="createdVisible" :title="t('integrations.api.apiKeyCreatedTitle')" :hide-footer="true" storage-key="setting-drawer:width:api-key-created" width="560px">
      <t-alert theme="warning" :message="t('integrations.api.apiKeyCreatedHint')" />
      <div class="token-value"><t-input :value="createdToken" type="password" readonly /><t-button variant="outline" @click="copy(createdToken)">{{ t('integrations.api.copy') }}</t-button></div>
    </SettingDrawer>
    <SettingDrawer v-model:visible="playgroundVisible" :title="t('integrations.api.playgroundTitle')" :confirm-text="t('integrations.api.playgroundRun')" :confirm-loading="running" storage-key="setting-drawer:width:api-playground" width="680px" @confirm="runQuery">
      <t-form :data="playground" layout="vertical" label-align="top" @submit.prevent>
        <t-form-item :label="t('integrations.api.apiKeyValue')"><t-input v-model="playground.apiKey" type="password" autocomplete="off" :disabled="running" /></t-form-item>
        <t-form-item :label="t('integrations.api.playgroundQuestion')"><t-textarea v-model="playground.query" :autosize="{ minRows: 3, maxRows: 8 }" :disabled="running" /></t-form-item>
      </t-form>
      <t-alert v-if="playgroundError" theme="error" :message="playgroundError" />
      <pre v-if="playgroundResult" class="query-result">{{ playgroundResult }}</pre>
    </SettingDrawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import SettingDrawer from '@/components/settings/SettingDrawer.vue'
import { apiKeyErrorMessage } from '@/utils/personalServerError'
import { getApiBaseUrl } from '@/utils/api-base'
import { copyWithToast } from '@/utils/clipboard'
import { listTenantAPIKeys, createTenantAPIKey, updateTenantAPIKey, deleteTenantAPIKey, type TenantAPIKey } from '@/api/tenant'

const { t } = useI18n()
const apiBaseUrl = computed(() => getApiBaseUrl().replace(/\/$/, '') || window.location.origin)
const keys = ref<TenantAPIKey[]>([])
const loading = ref(false)
const error = ref('')
const pagination = reactive({ current: 1, pageSize: 10, total: 0 })
const columns = computed(() => [
  { colKey: 'name', title: t('integrations.api.apiKeyName') },
  { colKey: 'api_key', title: t('integrations.api.apiKeyValue') },
  { colKey: 'expires_at', title: t('integrations.api.apiKeyExpiresAt') },
  { colKey: 'actions', title: t('common.operation') },
])
const editingVisible = ref(false)
const editingId = ref<number | null>(null)
const formRef = ref()
const saving = ref(false)
const formError = ref('')
const form = reactive({ name: '', expiresAt: '' })
const rules = computed(() => ({ name: [{ required: true, message: t('integrations.api.apiKeyNameRequired'), type: 'error' as const }] }))
const createdVisible = ref(false)
const createdToken = ref('')
const playgroundVisible = ref(false)
const playground = reactive({ apiKey: '', query: '' })
const running = ref(false)
const playgroundError = ref('')
const playgroundResult = ref('')

const copy = (value: string) => copyWithToast(value, 'common.copySuccess')
function onPageChange(page: { current: number; pageSize: number }) { Object.assign(pagination, page) }
async function load() {
  loading.value = true
  error.value = ''
  try {
    const response = await listTenantAPIKeys()
    if (!response.success) throw new Error(response.message || t('integrations.api.loadApiKeysFailed'))
    keys.value = response.data || []
    pagination.total = keys.value.length
    pagination.current = Math.min(pagination.current, Math.max(1, Math.ceil(keys.value.length / pagination.pageSize)))
  } catch (e: any) { error.value = apiKeyErrorMessage(e, t, 'integrations.api.loadApiKeysFailed') }
  finally { loading.value = false }
}
function openCreate() { editingId.value = null; form.name = ''; form.expiresAt = ''; formError.value = ''; editingVisible.value = true }
function openEdit(key: TenantAPIKey) {
  editingId.value = key.id
  form.name = key.name
  form.expiresAt = key.expires_at ? new Date(new Date(key.expires_at).getTime() - new Date(key.expires_at).getTimezoneOffset() * 60000).toISOString().slice(0, 16) : ''
  formError.value = ''
  editingVisible.value = true
}
async function save() {
  if (saving.value || await formRef.value?.validate() !== true) return
  const expires = form.expiresAt ? Math.floor(new Date(form.expiresAt).getTime() / 1000) : 0
  if (form.expiresAt && (!Number.isFinite(expires) || expires <= Date.now() / 1000)) { formError.value = t('integrations.api.apiKeyExpiresAtInvalid'); return }
  saving.value = true
  formError.value = ''
  try {
    const payload = { name: form.name.trim(), ...(expires ? { expires_at_unix: expires } : {}) }
    const response = editingId.value ? await updateTenantAPIKey(editingId.value, payload) : await createTenantAPIKey(payload)
    if (!response.success) throw new Error(response.message || t('integrations.api.createApiKeyFailed'))
    if (!editingId.value) { createdToken.value = (response.data as any)?.token || ''; createdVisible.value = true }
    editingVisible.value = false
    MessagePlugin.success(t('integrations.api.updateApiKeySuccess'))
    await load()
  } catch (e: any) { formError.value = apiKeyErrorMessage(e, t, editingId.value ? 'integrations.api.updateApiKeyFailed' : 'integrations.api.createApiKeyFailed') }
  finally { saving.value = false }
}
function revoke(key: TenantAPIKey) {
  const dialog = DialogPlugin.confirm({
    header: t('integrations.api.apiKeyRevoke'), body: t('integrations.api.apiKeyRevokeConfirm', { name: key.name }),
    confirmBtn: { content: t('integrations.api.apiKeyRevoke'), theme: 'danger' },
    onConfirm: async () => {
      try { const response = await deleteTenantAPIKey(key.id); if (!response.success) throw new Error(response.message); dialog.hide(); await load(); MessagePlugin.success(t('integrations.api.apiKeyRevoked')) }
      catch (e: any) { MessagePlugin.error(apiKeyErrorMessage(e, t, 'integrations.api.deleteApiKeyFailed')) }
    },
  })
}
async function runQuery() {
  if (running.value) return
  if (!playground.apiKey.trim() || !playground.query.trim()) { playgroundError.value = t('integrations.api.playgroundNeedQuestionAndKey'); return }
  running.value = true
  playgroundError.value = ''
  try {
    const response = await fetch(`${apiBaseUrl.value}/api/v1/knowledge-search`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-API-Key': playground.apiKey.trim() }, body: JSON.stringify({ query: playground.query.trim() }) })
    const body = await response.json()
    if (!response.ok) throw new Error(body.message || body.error?.message || t('integrations.api.playgroundRunFailed'))
    playgroundResult.value = JSON.stringify(body, null, 2)
  } catch (e: any) { playgroundError.value = apiKeyErrorMessage(e, t, 'integrations.api.playgroundRunFailed') }
  finally { running.value = false }
}
onMounted(load)
</script>

<style scoped lang="less">
.setting-row { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
.setting-info label { color: var(--td-text-color-primary); font-weight: 500; }
.setting-info p { color: var(--td-text-color-secondary); margin: 6px 0 0; }
.setting-control, .token-value { display: flex; align-items: center; gap: 8px; min-width: 0; }
.token-value { margin-top: 16px; }
code { overflow-wrap: anywhere; }
.query-result { overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; }
@media (max-width: 600px) { .setting-row { align-items: flex-start; flex-direction: column; } }
</style>
