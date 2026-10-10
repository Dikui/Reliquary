<script setup lang="ts">
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
function syncURL() { void router.replace({ query: { ...(operationType.value ? { operation_type: operationType.value } : {}), ...(page.value > 1 ? { page: String(page.value) } : {}) } }) }
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
  <main class="operation-log-page">
    <header><h1>{{ t('operationLogs.title') }}</h1><p>{{ t('operationLogs.description') }}</p></header>
    <div class="log-toolbar"><label><span>{{ t('operationLogs.type') }}</span><t-select v-model="operationType" :options="options" :aria-label="t('operationLogs.type')" @change="filterChanged" /></label><t-button variant="outline" :loading="loading" @click="load">{{ t('operationLogs.refresh') }}</t-button><span>{{ t('operationLogs.timezone', { timezone }) }}</span></div>
    <div v-if="error" role="alert" class="log-error">{{ error }} <t-button variant="text" @click="load">{{ t('common.retry') }}</t-button></div>
    <t-loading :loading="loading">
      <ol v-if="items.length" class="log-list">
        <li v-for="item in items" :key="item.id" class="log-item">
          <div class="log-item-heading"><strong>{{ t(`operationLogs.types.${item.operation_type}`) }}</strong><time :datetime="item.created_at">{{ timestamp(item.created_at) }}</time></div>
          <p class="log-actor">{{ t('operationLogs.actor') }} {{ actorLabel(item) }}</p>
          <p class="log-object">{{ t('operationLogs.object') }} <code>{{ item.object_id }}</code></p>
          <div class="log-values"><div><span>{{ t('operationLogs.before') }}</span><pre>{{ formatValue(item.before) }}</pre></div><div><span>{{ t('operationLogs.after') }}</span><pre>{{ formatValue(item.after) }}</pre></div></div>
        </li>
      </ol>
      <t-empty v-else-if="!loading && !error" :description="t('operationLogs.empty')" />
    </t-loading>
    <t-pagination v-if="total > 0" :current="page" :page-size="pageSize" :total="total" :show-page-size="false" @current-change="pageChanged" />
  </main>
</template>

<style scoped lang="less">
.operation-log-page { height: 100%; box-sizing: border-box; overflow-y: auto; padding: 32px; color: var(--td-text-color-primary); }
header h1 { margin: 0; font-size: 24px; }
header p, .log-toolbar { color: var(--td-text-color-secondary); line-height: 1.7; }
.log-toolbar { display: flex; align-items: flex-end; flex-wrap: wrap; gap: 16px; font-size: 13px; }
.log-toolbar label { display: grid; gap: 6px; min-width: 220px; }
.log-list { display: grid; gap: 16px; padding: 0; list-style: none; margin: 20px 0; }
.log-item { padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); min-width: 0; overflow-wrap: anywhere; }
.log-item-heading { display: flex; flex-wrap: wrap; gap: 8px 16px; justify-content: space-between; }
.log-item-heading time, .log-actor, .log-object, .log-values span { font-size: 13px; color: var(--td-text-color-secondary); }
.log-values { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.log-values pre { margin: 6px 0 0; padding: 12px; border-radius: var(--td-radius-small); background: var(--td-bg-color-secondarycontainer); white-space: pre-wrap; overflow-wrap: anywhere; font-size: 12px; line-height: 1.7; }
.log-error { color: var(--td-error-color); }
@media (max-width: 640px) { .operation-log-page { padding: 16px; } .log-values { grid-template-columns: minmax(0, 1fr); } }
</style>
