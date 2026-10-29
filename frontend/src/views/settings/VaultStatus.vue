<script setup lang="ts">
import { Button, useConfirm } from '@/components/ui'
import '@/styles/settings-form.css'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getVaultStatus, rebuildVaultFiles, getVaultFileRebuildStatus, type VaultStatus, type VaultFileRebuildStatus } from '@/api/vault'
import MaintenanceStatus from './MaintenanceStatus.vue'

const confirm = useConfirm()
const { t, locale } = useI18n()
const status = ref<VaultStatus | null>(null)
const loading = ref(false)
const error = ref('')
let generation = 0
const rebuild = ref<VaultFileRebuildStatus>({ status: 'idle' })
const rebuilding = ref(false)
const rebuildError = ref('')
const confirming = ref(false)
let disposed = false
let rebuildGeneration = 0
let pollTimer: ReturnType<typeof setTimeout> | undefined
let confirmation: ReturnType<typeof confirm> | undefined

function applyRebuild(state: VaultFileRebuildStatus) {
  clearTimeout(pollTimer)
  rebuild.value = state
  rebuilding.value = state.status === 'running'
  if (state.status !== 'idle') rebuildError.value = state.status === 'failed' ? state.error || t('settings.vault.rebuildFailed') : ''
  if (rebuilding.value) pollTimer = setTimeout(() => { void pollRebuild() }, 1000)
}

async function pollRebuild(recoverOnly = false) {
  const request = ++rebuildGeneration
  try {
    const response = await getVaultFileRebuildStatus()
    if (disposed || request !== rebuildGeneration) return
    if (!response.success || !response.data) throw new Error(t('settings.vault.rebuildFailed'))
    if (recoverOnly && response.data.status !== 'running') return
    const wasRunning = rebuilding.value
    applyRebuild(response.data)
    if (wasRunning && !rebuilding.value) void load()
  } catch (cause: any) {
    if (disposed || request !== rebuildGeneration) return
    rebuildError.value = cause?.message || t('settings.vault.rebuildFailed')
    // Keep observing an accepted run even after a transient network failure.
    if (rebuilding.value) pollTimer = setTimeout(() => { void pollRebuild() }, 3000)
  }
}

async function startRebuild() {
  if (rebuilding.value || disposed) return
  const request = ++rebuildGeneration
  rebuilding.value = true
  rebuildError.value = ''
  rebuild.value = { status: 'running' }
  clearTimeout(pollTimer)
  try {
    const response = await rebuildVaultFiles()
    if (disposed || request !== rebuildGeneration) return
    if (!response.success || !response.data) throw new Error(t('settings.vault.rebuildFailed'))
    applyRebuild(response.data)
  } catch (cause: any) {
    if (disposed || request !== rebuildGeneration) return
    rebuilding.value = false
    rebuild.value = { status: 'idle' }
    rebuildError.value = cause?.message || t('settings.vault.rebuildFailed')
    // A lost response or conflict may mean a run is already active.
    void pollRebuild(true)
  }
}

function confirmRebuild() {
  if (rebuilding.value || confirming.value) return
  confirming.value = true
  const close = () => { confirmation?.destroy(); confirmation = undefined; confirming.value = false }
  confirmation = confirm({
    header: t('settings.vault.rebuild'),
    body: t('settings.vault.rebuildConfirm'),
    confirmBtn: t('settings.vault.rebuild'),
    cancelBtn: t('common.cancel'),
    onConfirm: () => { close(); void startRebuild() },
    onCancel: close,
    onClose: close,
  })
}

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

function refresh() {
  void load()
  if (rebuilding.value) return
  clearTimeout(pollTimer)
  void pollRebuild()
}

function formatScanTime(value?: string | null) {
  if (!value) return t('settings.vault.notScanned')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return t('settings.vault.notScanned')
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'medium' }).format(date)
}

watch(locale, () => { loading.value = false; void load() })
onMounted(() => { void load(); void pollRebuild() })
onBeforeUnmount(() => { generation++; disposed = true; clearTimeout(pollTimer); confirmation?.destroy() })
</script>

<template>
  <section class="vault-status settings-form" :aria-label="t('settings.vault.title')" :aria-busy="loading">
    <div class="vault-status-heading">
      <div class="vault-status-actions">
        <Button :loading="rebuilding" :disabled="rebuilding || confirming" @click="confirmRebuild">{{ t('settings.vault.rebuild') }}</Button>
        <Button variant="ghost" :loading="loading" :disabled="loading" @click="refresh">{{ t(error ? 'common.retry' : 'common.refresh') }}</Button>
      </div>
    </div>
    <p v-if="rebuilding" role="status">{{ t('settings.vault.rebuildRunning') }}</p>
    <p v-if="rebuildError" class="settings-error" role="alert">{{ rebuildError }}</p>
    <div v-if="rebuild.result" role="status">
      <p>{{ t(rebuild.status === 'success' ? 'settings.vault.rebuildComplete' : 'settings.vault.rebuildFailed') }}</p>
      <p>{{ t('settings.vault.rebuildCounts', { written: rebuild.result.written, skipped: rebuild.result.skipped, failed: rebuild.result.failed }) }}</p>
    </div>
    <p v-if="error" class="settings-error" role="alert">{{ error }}</p>
    <p v-if="loading && !status" role="status">{{ t('common.loading') }}</p>
    <template v-if="status">
      <dl class="vault-root">
        <dt>{{ t('settings.vault.root') }}</dt>
        <dd><code>{{ status.root }}</code></dd>
      </dl>
      <p class="vault-readonly-hint">{{ t('settings.vault.rootHint') }}</p>
      <p class="vault-readonly-hint">{{ t('settings.vault.archiveHint') }}</p>
      <p v-if="status.libraries.length === 0" role="status">{{ t('common.noData') }}</p>
      <div class="vault-directories">
        <section v-for="library in status.libraries" :key="library.library_id" class="vault-directory">
          <h4>{{ library.name }} <code>{{ library.directory }}/</code></h4>
          <dl>
            <dt>{{ t('settings.vault.archivedEntries') }}</dt>
            <dd>{{ new Intl.NumberFormat(locale).format(library.archived_entries) }}</dd>
            <template v-if="library.system_role === 'ai-output'">
              <dt>{{ t('settings.vault.lastScan') }}</dt>
              <dd>{{ formatScanTime(library.last_scan_at) }}</dd>
              <dt>{{ t('settings.vault.lastError') }}</dt>
              <dd :class="{ 'vault-scan-error': library.last_error }">{{ library.last_error || t('settings.vault.noError') }}</dd>
            </template>
          </dl>
        </section>
      </div>
    </template>
  </section>
  <MaintenanceStatus />
</template>

<style scoped>
.vault-status-heading, .vault-status-actions { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.vault-directories { display: grid; gap: var(--space-4); }
.vault-directory { display: grid; grid-template-columns: 224px minmax(0, 1fr); gap: var(--space-6); border-top: 2px solid var(--rule); min-width: 0; }
.vault-scan-error { color: var(--danger); }
@container settings-content (max-width: 720px) { .vault-directory { grid-template-columns: minmax(0, 1fr); gap: var(--space-2); } }
@media (max-width: 640px) { .vault-directory { grid-template-columns: minmax(0, 1fr); gap: var(--space-2); } }
</style>
