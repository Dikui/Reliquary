<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getMaintenanceStatus, type MaintenanceStatus, type MaintenanceResult } from '@/api/maintenance'

const { t, locale } = useI18n()
const status = ref<MaintenanceStatus | null>(null)
const loading = ref(false)
const error = ref('')
let generation = 0

async function load() {
  if (loading.value) return
  const request = ++generation
  loading.value = true
  error.value = ''
  try {
    const result = await getMaintenanceStatus()
    if (request !== generation) return
    if (!result.success || !result.data) throw new Error(t('settings.maintenance.loadFailed'))
    status.value = result.data
  } catch (cause: any) {
    if (request === generation) error.value = cause?.message || t('settings.maintenance.loadFailed')
  } finally {
    if (request === generation) loading.value = false
  }
}

function formatTime(value?: string | null) {
  if (!value || Number.isNaN(new Date(value).getTime())) return t('settings.maintenance.never')
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(value))
}

function resultLabel(value: MaintenanceResult) {
  const labels: Record<MaintenanceResult, string> = {
    never: t('settings.maintenance.never'),
    running: t('settings.maintenance.running'),
    success: t('settings.maintenance.success'),
    failed: t('settings.maintenance.failed'),
    skipped: t('settings.maintenance.skipped'),
  }
  return labels[value] || t('settings.maintenance.failed')
}

function formatSize(bytes: number) {
  return `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }).format(bytes / 1048576)} MiB`
}

onMounted(() => { void load() })
onBeforeUnmount(() => { generation++ })
</script>

<template>
  <section class="maintenance-status" :aria-label="t('settings.maintenance.title')" :aria-busy="loading">
    <div class="maintenance-heading">
      <h3>{{ t('settings.maintenance.title') }}</h3>
      <t-button variant="text" size="small" :loading="loading" :disabled="loading" @click="load">{{ t(error ? 'common.retry' : 'common.refresh') }}</t-button>
    </div>
    <t-alert v-if="error" theme="error" :message="error" role="alert" />
    <p v-if="loading && !status" role="status">{{ t('common.loading') }}</p>
    <template v-if="status">
      <t-alert v-if="status.paused" theme="info" :message="t('settings.maintenance.paused')" />
      <dl class="maintenance-config">
        <dt>{{ t('settings.maintenance.directory') }}</dt>
        <dd><code>{{ status.backup_directory }}</code></dd>
        <dt>{{ t('settings.maintenance.schedule') }}</dt>
        <dd>{{ t(status.automatic_enabled ? 'settings.maintenance.enabled' : 'settings.maintenance.disabled') }} <span>{{ status.schedule_timezone }}</span></dd>
        <dt>{{ t('settings.maintenance.weeklySchedule') }}</dt>
        <dd><code>{{ status.backup_schedule }}</code></dd>
        <dt>{{ t('settings.maintenance.monthlySchedule') }}</dt>
        <dd><code>{{ status.drill_schedule }}</code></dd>
      </dl>
      <p class="maintenance-hint">{{ t('settings.maintenance.scheduleHint') }}</p>
      <div class="maintenance-results">
        <section class="maintenance-result">
          <h4>{{ t('settings.maintenance.backup') }}</h4>
          <dl>
            <dt>{{ t('settings.maintenance.latestTime') }}</dt><dd>{{ formatTime(status.backup_at) }}</dd>
            <dt>{{ t('settings.maintenance.result') }}</dt><dd>{{ resultLabel(status.backup_status) }}</dd>
            <dt>{{ t('settings.maintenance.size') }}</dt><dd>{{ formatSize(status.backup_size_bytes) }}</dd>
            <dt>{{ t('settings.maintenance.latestError') }}</dt><dd :class="{ 'maintenance-error': status.backup_error }" :role="status.backup_error ? 'alert' : undefined">{{ status.backup_error || t('settings.maintenance.noError') }}</dd>
          </dl>
        </section>
        <section class="maintenance-result">
          <h4>{{ t('settings.maintenance.drill') }}</h4>
          <dl>
            <dt>{{ t('settings.maintenance.latestTime') }}</dt><dd>{{ formatTime(status.drill_at) }}</dd>
            <dt>{{ t('settings.maintenance.result') }}</dt><dd>{{ resultLabel(status.drill_status) }}</dd>
            <dt>{{ t('settings.maintenance.latestError') }}</dt><dd :class="{ 'maintenance-error': status.drill_error }" :role="status.drill_error ? 'alert' : undefined">{{ status.drill_error || t('settings.maintenance.noError') }}</dd>
          </dl>
        </section>
      </div>
    </template>
  </section>
</template>

<style scoped lang="less">
.maintenance-status { margin-top: 28px; padding-top: 24px; border-top: 1px solid var(--td-component-stroke); color: var(--td-text-color-primary); }
.maintenance-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 32px; }
h3 { margin: 0; font-size: 18px; }
.maintenance-config { margin: 20px 0 0; }
dt { color: var(--td-text-color-secondary); font-size: 13px; }
dd { margin: 6px 0 16px; overflow-wrap: anywhere; }
dd span { margin-left: 8px; color: var(--td-text-color-secondary); }
code { font-size: 12px; }
.maintenance-hint { color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.6; }
.maintenance-results { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; }
.maintenance-result { min-width: 0; }
h4 { font-size: 14px; margin: 16px 0; }
.maintenance-error { color: var(--td-error-color); }
@container settings-content (max-width: 720px) { .maintenance-results { grid-template-columns: minmax(0, 1fr); gap: 0; } }
@media (max-width: 640px) { .maintenance-results { grid-template-columns: minmax(0, 1fr); gap: 0; } }
</style>
