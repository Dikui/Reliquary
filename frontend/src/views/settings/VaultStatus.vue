<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getVaultStatus, type VaultStatus } from '@/api/vault'
import MaintenanceStatus from './MaintenanceStatus.vue'

const { t, locale } = useI18n()
const status = ref<VaultStatus | null>(null)
const loading = ref(false)
const error = ref('')
let generation = 0

async function load() {
  if (loading.value) return
  const request = ++generation
  loading.value = true
  error.value = ''
  try {
    const result = await getVaultStatus()
    if (request !== generation) return
    if (!result.success || !result.data) throw new Error(t('settings.vault.loadFailed'))
    status.value = result.data
  } catch (cause: any) {
    if (request === generation) error.value = cause?.message || t('settings.vault.loadFailed')
  } finally {
    if (request === generation) loading.value = false
  }
}

function formatScanTime(value?: string | null) {
  if (!value) return t('settings.vault.notScanned')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return t('settings.vault.notScanned')
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'medium' }).format(date)
}

onMounted(() => { void load() })
onBeforeUnmount(() => { generation++ })
</script>

<template>
  <section class="vault-status" :aria-label="t('settings.vault.title')" :aria-busy="loading">
    <div class="vault-status-heading">
      <h3>{{ t('settings.vault.title') }}</h3>
      <t-button variant="text" size="small" :loading="loading" :disabled="loading" @click="load">{{ t(error ? 'common.retry' : 'common.refresh') }}</t-button>
    </div>
    <t-alert v-if="error" theme="error" :message="error" role="alert" />
    <p v-if="loading && !status" role="status">{{ t('common.loading') }}</p>
    <template v-if="status">
      <dl class="vault-root">
        <dt>{{ t('settings.vault.root') }}</dt>
        <dd><code>{{ status.root }}</code></dd>
      </dl>
      <p class="vault-readonly-hint">{{ t('settings.vault.rootHint') }}</p>
      <div class="vault-directories">
        <section v-for="kind in (['mine', 'reports'] as const)" :key="kind" class="vault-directory">
          <h4>{{ t(kind === 'mine' ? 'settings.vault.mine' : 'settings.vault.reports') }} <code>{{ status[kind].directory }}/</code></h4>
          <p v-if="kind === 'mine'" class="vault-readonly-hint">{{ t('settings.vault.mineImportedHint') }}</p>
          <dl>
            <template v-if="kind === 'reports'">
              <dt>{{ t('settings.vault.lastScan') }}</dt>
              <dd>{{ formatScanTime(status[kind].last_scan_at) }}</dd>
            </template>
            <dt>{{ t('settings.vault.indexedFiles') }}</dt>
            <dd>{{ new Intl.NumberFormat(locale).format(status[kind].indexed_files) }}</dd>
            <dt>{{ t('settings.vault.lastError') }}</dt>
            <dd :class="{ 'vault-scan-error': status[kind].last_error }">{{ status[kind].last_error || t('settings.vault.noError') }}</dd>
          </dl>
        </section>
      </div>
    </template>
  </section>
  <MaintenanceStatus />
</template>

<style scoped lang="less">
.vault-status { color: var(--td-text-color-primary); }
.vault-status-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
h3 { margin: 0; font-size: 18px; }
.vault-root { margin: 20px 0 0; }
dt { color: var(--td-text-color-secondary); font-size: 13px; }
dd { margin: 6px 0 16px; overflow-wrap: anywhere; }
code { font-size: 12px; }
.vault-readonly-hint { margin-top: -6px; color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.6; }
.vault-directories { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; }
.vault-directory { min-width: 0; }
h4 { font-size: 14px; margin: 16px 0; }
h4 code { margin-left: 6px; color: var(--td-text-color-secondary); font-weight: 400; }
.vault-scan-error { color: var(--td-error-color); }
@container settings-content (max-width: 720px) { .vault-directories { grid-template-columns: minmax(0, 1fr); gap: 0; } }
@media (max-width: 640px) { .vault-directories { grid-template-columns: minmax(0, 1fr); gap: 0; } }
</style>
