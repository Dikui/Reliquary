<script setup lang="ts">
import { Button, Select } from '@/components/ui'
import '@/styles/settings-form.css'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { listOperationLogs, OPERATION_TYPES, type OperationLog } from '@/api/operation-logs'

const { t, locale } = useI18n()
const route = useRoute(), router = useRouter()
const operationType = ref(''), page = ref(1), pageSize = 30
const items = ref<OperationLog[]>([]), total = ref(0), loading = ref(false), error = ref('')
const options = computed(() => [{ value: '', label: t('operationLogs.allTypes') }, ...OPERATION_TYPES.map(value => ({ value, label: t(`operationLogs.types.${value}`) }))])
const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
let generation = 0, disposed = false
const timestamp = (value: string) => new Date(value).toLocaleString(locale.value, { timeZoneName: 'short' })
const formatValue = (value: unknown) => value == null ? t('operationLogs.noValue') : JSON.stringify(value, null, 2)
function actorLabel(item: OperationLog) {
  if (item.actor?.startsWith('web_user:')) return t('operationLogs.webActor')
  if (item.actor?.startsWith('api_key:')) return `API Key ${item.actor_name?.trim() || '#' + item.actor.slice('api_key:'.length)}`
  return item.actor || t('operationLogs.noValue')
}
async function load() {
  const current = ++generation
  loading.value = true
  error.value = ''
  try {
    const response = await listOperationLogs({ operation_type: operationType.value || undefined, page: page.value, page_size: pageSize })
    if (!response.success) throw new Error(response.message)
    if (current !== generation || disposed) return
    items.value = response.data.items || []
    total.value = response.data.total || 0
  } catch { if (current === generation && !disposed) error.value = t('operationLogs.loadFailed') }
  finally { if (current === generation && !disposed) loading.value = false }
}
function syncURL() { void router.replace({ query: { ...(route.path === '/platform/settings' ? { section: 'operation-logs' } : {}), ...(operationType.value ? { operation_type: operationType.value } : {}), ...(page.value > 1 ? { page: String(page.value) } : {}) } }) }
function readURL() {
  const value = typeof route.query.operation_type === 'string' ? route.query.operation_type : ''
  operationType.value = OPERATION_TYPES.includes(value as typeof OPERATION_TYPES[number]) ? value : ''
  const valuePage = Number(route.query.page)
  page.value = Number.isInteger(valuePage) && valuePage > 0 ? valuePage : 1
}
function filterChanged() { page.value = 1; syncURL(); void load() }
function pageChanged(value: number) { page.value = value; syncURL(); void load() }
watch(() => route.query, () => {
  const previous = `${operationType.value}:${page.value}`
  readURL()
  if (previous !== `${operationType.value}:${page.value}`) void load()
})
onMounted(() => { readURL(); void load() })
onUnmounted(() => { disposed = true; generation++ })
</script>

<template>
  <section class="operation-log-page settings-form">
    <header><p>{{ t('operationLogs.description') }}</p></header>
    <div class="log-toolbar"><label><span>{{ t('operationLogs.type') }}</span><Select v-model="operationType" :aria-label="t('operationLogs.type')" @update:model-value="filterChanged"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></Select></label><Button variant="default" :loading="loading" @click="load">{{ t('operationLogs.refresh') }}</Button><span>{{ t('operationLogs.timezone', { timezone }) }}</span></div>
    <div v-if="error" role="alert" class="log-error">{{ error }} <Button variant="ghost" @click="load">{{ t('common.retry') }}</Button></div>
    <div :loading="loading">
      <ol v-if="items.length" class="log-list">
        <li v-for="item in items" :key="item.id" class="log-item">
          <div class="log-item-heading"><strong>{{ t(`operationLogs.types.${item.operation_type}`) }}</strong><time :datetime="item.created_at">{{ timestamp(item.created_at) }}</time></div>
          <p class="log-actor">{{ t('operationLogs.actor') }} {{ actorLabel(item) }}</p>
          <p class="log-object">{{ t('operationLogs.object') }} <code :title="item.object_id">{{ item.object_id.length > 36 ? item.object_id.slice(0, 12) + '…' : item.object_id }}</code></p>
          <div class="log-values"><div><span>{{ t('operationLogs.before') }}</span><pre>{{ formatValue(item.before) }}</pre></div><div><span>{{ t('operationLogs.after') }}</span><pre>{{ formatValue(item.after) }}</pre></div></div>
        </li>
      </ol>
      <p v-else-if="!loading && !error" role="status">{{ t('operationLogs.empty') }}</p>
    </div>
    <nav v-if="total > 0" class="settings-actions" :aria-label="t('entries.pagination')"><Button :disabled="loading || page <= 1" @click="pageChanged(page - 1)">{{ t('entries.previous') }}</Button><span>{{ page }} / {{ Math.ceil(total / pageSize) }}</span><Button :disabled="loading || page * pageSize >= total" @click="pageChanged(page + 1)">{{ t('entries.next') }}</Button></nav>
  </section>
</template>

<style scoped>
 header p { color: var(--muted); }
.log-toolbar { display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--space-3); border-top: 2px solid var(--rule); padding-top: var(--space-4); }
.log-toolbar label { display: grid; gap: var(--space-2); min-width: 180px; }
.log-list { list-style: none; padding: 0; }
.log-item { border-top: 2px solid var(--rule); padding: var(--space-4) 0; overflow-wrap: anywhere; }
.log-item-heading { display: flex; flex-wrap: wrap; justify-content: space-between; gap: var(--space-2); }
.log-item-heading time, .log-actor, .log-object, .log-values span { font-size: var(--text-sm); color: var(--muted); }
.log-values { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-4); }
.log-values pre { padding: var(--space-3); background: var(--surface-soft); }
.log-error { color: var(--danger); }
@container settings-content (max-width: 720px) { .log-values { grid-template-columns: minmax(0, 1fr); } }
@media (max-width: 640px) { .log-values { grid-template-columns: minmax(0, 1fr); } }
</style>
