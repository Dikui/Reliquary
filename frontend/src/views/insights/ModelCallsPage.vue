<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { read, callDetails, type CallSummary, type ModelCall } from '@/api/insights'
import TrendChart from './TrendChart.vue'
import ModelCallContentViewer from '@/components/ModelCallContentViewer.vue'
const viewingCall = ref('')
const viewingDetail = computed(() => calls.value.find(call => call.id === viewingCall.value))
import type { TrendMetric } from './trend'
import { Button, DataTable, Drawer, EmptyState, Tag } from '@/components/ui'
import './diagnostics.css'
import './diagnostics-b.css'
const { t } = useI18n(), route = useRoute(), router = useRouter()
const models = ref<{ id: string; name: string }[]>([]), purposes = ref<string[]>([]), summaries = ref<CallSummary[]>([]), trend = ref<CallSummary[]>([]), calls = ref<ModelCall[]>([])
const initialFrom = new Date(); initialFrom.setDate(initialFrom.getDate() - 30)
const filters = ref({ from: initialFrom.toISOString().slice(0, 10), to: '', model_id: '', purpose: '' })
const applied = ref({ ...filters.value }), period = ref('day'), group = ref('model,purpose'), metric = ref<TrendMetric>('calls')
const entry = computed(() => typeof route.query.entry_id === 'string' ? route.query.entry_id : '')
const entryFilter = ref(entry.value)
const focusedCall = ref('')
const offset = ref(0), total = ref(0), loading = ref(true), error = ref(''), entryTitle = ref('')
let version = 0
const purposeLabels = computed<Record<string, string>>(() => ({ content_check: t('ikb19.purposes.content_check'), intake_review: t('ikb19.purposes.intakeReview'), article_processing: t('ikb19.purposes.processing'), segment_merge: t('ikb19.purposes.segmentMerge'), image_description: t('ikb19.purposes.imageDescription'), entity_resolution: t('ikb19.purposes.entityResolution'), entity_name_embedding: t('ikb19.purposes.entityEmbedding'), body_embedding: t('ikb19.purposes.bodyEmbedding'), rerank: t('ikb19.purposes.rerank'), evaluation_generation: t('ikb19.purposes.evaluation'), other: t('ikb19.purposes.other') }))
const modelOptions = computed(() => [...models.value, ...[...new Set([...summaries.value.map(s => s.model_id), ...calls.value.map(c => c.model_id)])].filter(id => id && !models.value.some(m => m.id === id)).map(id => ({ id, name: calls.value.find(c => c.model_id === id)?.model_name || id }))])
function modelName(id: string) { return modelOptions.value.find(m => m.id === id)?.name || id }
function purposeName(value: string) { return purposeLabels.value[value] || value }
function value(value: number | null | undefined, unknown = 0, currency = '', notApplicable = 0) {
  const known = value == null ? '' : currency ? `${currency} ${value.toFixed(6)}` : value.toLocaleString()
  const notes = [unknown ? `${t('ikb19.unknown')} (${unknown})` : '', notApplicable ? `${t('ikb19.notApplicable')} (${notApplicable})` : ''].filter(Boolean)
  return [known ? known + (unknown ? ` ${t('ikb19.partial')}` : '') : notes.length ? '' : t('ikb19.unknown'), ...notes].filter(Boolean).join('; ')
}
function params() { return { ...applied.value, from: applied.value.from ? new Date(applied.value.from).toISOString() : undefined, to: applied.value.to ? new Date(applied.value.to).toISOString() : undefined, entry_id: entry.value || undefined } }
async function load(all = true) {
  const ticket = ++version
  loading.value = true; error.value = ''
  calls.value = []; focusedCall.value = ''; viewingCall.value = ''
  if (all) { summaries.value = []; trend.value = []; total.value = 0 }
  try {
    const filter = params()
    if (filter.from && filter.to && filter.from >= filter.to) throw new Error(t('ikb19.invalidRange'))
    const detailRequest = callDetails({ ...filter, limit: 25, offset: offset.value })
    if (all) {
      const [summaryData, trendData, details] = await Promise.all([read<CallSummary[]>('model-calls/summary', { ...filter, period: period.value, group_by: group.value }), read<CallSummary[]>('model-calls/trend', filter), detailRequest])
      if (ticket !== version) return
      summaries.value = summaryData; trend.value = trendData; calls.value = details.data; total.value = details.total || 0
    } else { const details = await detailRequest; if (ticket !== version) return; calls.value = details.data; total.value = details.total || 0 }
  } catch (e: any) { if (ticket === version) error.value = e?.message || t('ikb19.loadFailed') }
  finally { if (ticket === version) loading.value = false }
}
async function initialize() {
  loading.value = true; error.value = ''
  try { const [m, p] = await Promise.all([read<{ id: string; name: string }[]>('models'), read<string[]>('model-calls/purposes')]); models.value = m; purposes.value = p; await load() }
  catch (e: any) { error.value = e?.message || t('ikb19.loadFailed'); loading.value = false }
}
function apply() {
  applied.value = { ...filters.value }; offset.value = 0
  if (entryFilter.value.trim() !== entry.value) void router.replace({ query: { ...route.query, entry_id: entryFilter.value.trim() || undefined } })
  else void load()
}
function page(direction: number) { offset.value = Math.max(0, offset.value + direction * 25); void load(false) }
async function entryChanged() {
  entryFilter.value = entry.value; entryTitle.value = ''; offset.value = 0
  const id = entry.value
  await load()
  if (id) { try { const e = await read<{ title: string }>(`entries/${encodeURIComponent(id)}`); if (entry.value === id) entryTitle.value = e.title } catch { /* Deleted materials retain their call history. */ } }
}
watch([period, group], () => { offset.value = 0; void load() })
watch(entry, entryChanged)
onMounted(async () => { await initialize(); const id = entry.value; if (id) { try { const result = await read<{ title: string }>(`entries/${encodeURIComponent(id)}`); if (entry.value === id) entryTitle.value = result.title } catch { /* Call history survives deleted materials. */ } } })
onBeforeUnmount(() => { version++ })
const shortId = (id: string) => id.length > 12 ? id.slice(0, 8) : id
const totals = computed(() => {
  const sumKnown = (key: 'input_tokens' | 'output_tokens') => {
    const known = trend.value.filter(row => row[key] !== null)
    return known.length ? known.reduce((sum, row) => sum + row[key]!, 0) : null
  }
  const sum = (key: 'calls' | 'failures' | 'unknown_input_calls' | 'unknown_output_calls') => trend.value.reduce((n, row) => n + row[key], 0)
  const currencies = [...new Set(trend.value.map(row => row.currency))].map(currency => {
    const rows = trend.value.filter(row => row.currency === currency), known = rows.filter(row => row.cost !== null)
    return { currency, cost: known.length ? known.reduce((n, row) => n + row.cost!, 0) : null, unknown: rows.reduce((n, row) => n + row.unknown_cost_calls, 0), notApplicable: rows.reduce((n, row) => n + row.not_applicable_cost_calls, 0) }
  })
  return { calls: sum('calls'), failures: sum('failures'), input: sumKnown('input_tokens'), output: sumKnown('output_tokens'), unknownInput: sum('unknown_input_calls'), unknownOutput: sum('unknown_output_calls'), currencies }
})
const columns = computed(() => [
  { key: 'time', label: t('ikb19.time') }, { key: 'model', label: t('ikb19.model') }, { key: 'purpose', label: t('ikb19.purpose') },
  { key: 'entry', label: t('ikb19.entry') }, { key: 'result', label: t('ikb19.result') }, { key: 'duration', label: t('ikb19.elapsed') },
  { key: 'input', label: t('ikb19.inputTokens') }, { key: 'output', label: t('ikb19.outputTokens') }, { key: 'cost', label: t('ikb19.cost') }, { key: 'attempt', label: t('ikb19.attempt') },
])
function openClickedRow(event: MouseEvent) {
  const target = event.target as HTMLElement
  if (target.closest('a, button')) return
  const id = target.closest<HTMLElement>('[data-row-key]')?.dataset.rowKey
  if (id) viewingCall.value = id
}
</script>
<template>
  <main class="diagnostics-page analytics-page model-calls-page" :aria-busy="loading">
    <div class="diagnostics-wrap analytics-wrap">
      <div class="diagnostics-heading"><h1>{{ t('ikb19.callsTitle') }}</h1></div><p class="muted">{{ t('ikb19.callsHint') }}</p>
      <div v-if="entry" class="notice entry-scope">{{ t('ikb19.entryCalls') }}: <a :title="entry" :href="`/platform/entries/${encodeURIComponent(entry)}`">{{ entryTitle || shortId(entry) }}</a> <Button @click="router.replace({ query: { ...route.query, entry_id: undefined } })">{{ t('ikb19.clearEntry') }}</Button></div>
      <form class="calls-filters" @submit.prevent="apply">
        <div class="fields">
          <label class="field">{{ t('ikb19.from') }}<input class="rq-control" type="date" :value="filters.from" @input="filters.from = ($event.target as HTMLInputElement).value" /></label>
          <label class="field">{{ t('ikb19.toExclusive') }}<input class="rq-control" type="date" :value="filters.to" @input="filters.to = ($event.target as HTMLInputElement).value" /></label>
          <label class="field">{{ t('ikb19.model') }}<select class="rq-control" :value="filters.model_id" @change="filters.model_id = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.all') }}</option><option v-for="m in modelOptions" :key="m.id" :value="m.id">{{ m.name }}</option><option v-if="filters.model_id && !modelOptions.some(m => m.id === filters.model_id)" :value="filters.model_id">{{ filters.model_id }}</option></select></label>
          <label class="field">{{ t('ikb19.purpose') }}<select class="rq-control" :value="filters.purpose" @change="filters.purpose = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.all') }}</option><option v-for="p in purposes" :key="p" :value="p">{{ purposeName(p) }}</option></select></label>
          <label class="field">{{ t('ikb19.entry') }}<input class="rq-control" type="search" :value="entryFilter" @input="entryFilter = ($event.target as HTMLInputElement).value" /></label>
        </div>
        <p class="muted">{{ t('ikb19.utcHint') }}</p><div class="toolbar"><Button type="submit" variant="primary" :disabled="loading">{{ t('ikb19.apply') }}</Button><Button type="button" :disabled="loading" @click="filters = { from: '', to: '', model_id: '', purpose: '' }; apply()">{{ t('ikb19.reset') }}</Button></div>
      </form>
      <div v-if="error" role="alert" class="notice error">{{ error }} <Button :disabled="loading" @click="initialize">{{ t('ikb19.retry') }}</Button></div>
      <p v-if="loading" role="status">{{ t('ikb19.loading') }}</p>
      <EmptyState v-if="!loading && !total && !error" :title="t('ikb19.noCalls')" />
      <template v-if="trend.length || summaries.length || calls.length">
        <div v-if="trend.length" class="analytics-metrics call-kpis" :aria-label="t('ikb19.summary')">
          <div class="analytics-metric"><span class="metric-label">{{ t('ikb19.calls') }}</span><strong class="metric-value">{{ totals.calls.toLocaleString() }}</strong></div>
          <div class="analytics-metric"><span class="metric-label">{{ t('ikb19.failureRate') }}</span><strong class="metric-value" :class="{ 'metric-down': totals.failures > 0 }">{{ (totals.calls ? totals.failures / totals.calls * 100 : 0).toFixed(2) }}%</strong></div>
          <div class="analytics-metric"><span class="metric-label">{{ t('ikb19.inputTokens') }}</span><strong class="metric-value">{{ totals.input === null ? t('ikb19.unknown') : totals.input.toLocaleString() }}</strong><span v-if="totals.unknownInput" class="muted">{{ totals.input !== null ? t('ikb19.partial') + ' · ' : '' }}{{ t('ikb19.unknown') }} {{ totals.unknownInput }}</span></div>
          <div class="analytics-metric"><span class="metric-label">{{ t('ikb19.outputTokens') }}</span><strong class="metric-value">{{ totals.output === null ? t('ikb19.unknown') : totals.output.toLocaleString() }}</strong><span v-if="totals.unknownOutput" class="muted">{{ totals.output !== null ? t('ikb19.partial') + ' · ' : '' }}{{ t('ikb19.unknown') }} {{ totals.unknownOutput }}</span></div>
          <div class="analytics-metric"><span class="metric-label">{{ t('ikb19.cost') }}</span><span v-for="currency in totals.currencies" :key="currency.currency" class="metric-cost">{{ currency.currency || t('ikb19.noCurrency') }} {{ value(currency.cost, currency.unknown, '', currency.notApplicable) }}</span></div>
        </div>
        <section v-if="trend.length" class="analytics-stack"><div class="chart-heading"><h2>{{ t('ikb19.dailyTrend') }}</h2><label class="field">{{ t('ikb19.trendMetric') }}<select class="rq-control" :value="metric" @change="metric = ($event.target as HTMLSelectElement).value as TrendMetric"><option value="calls">{{ t('ikb19.calls') }}</option><option value="tokens">{{ t('ikb19.tokens') }}</option><option value="cost">{{ t('ikb19.cost') }}</option></select></label></div><TrendChart :rows="trend" :metric="metric" /></section>
        <section v-if="summaries.length" class="analytics-stack">
          <div class="chart-heading"><h2>{{ t('ikb19.summary') }}</h2><div class="toolbar"><label class="field">{{ t('ikb19.period') }}<select class="rq-control" :value="period" :disabled="loading" @change="period = ($event.target as HTMLSelectElement).value"><option value="day">{{ t('ikb19.day') }}</option><option value="week">{{ t('ikb19.week') }}</option><option value="month">{{ t('ikb19.month') }}</option></select></label><label class="field">{{ t('ikb19.groupBy') }}<select class="rq-control" :value="group" :disabled="loading" @change="group = ($event.target as HTMLSelectElement).value"><option value="model,purpose">{{ t('ikb19.modelAndPurpose') }}</option><option value="model">{{ t('ikb19.model') }}</option><option value="purpose">{{ t('ikb19.purpose') }}</option></select></label></div></div>
          <p class="muted">{{ t('ikb19.partialHint') }}</p>
          <div class="table-scroll summary-table" tabindex="0" role="region" :aria-label="t('ikb19.summary')"><table class="rq-table"><thead><tr><th>{{ t('ikb19.time') }} UTC</th><th>{{ t('ikb19.model') }}</th><th>{{ t('ikb19.purpose') }}</th><th>{{ t('ikb19.currency') }}</th><th>{{ t('ikb19.calls') }}</th><th>{{ t('ikb19.failureRate') }}</th><th>{{ t('ikb19.duration') }}</th><th>{{ t('ikb19.inputTokens') }}</th><th>{{ t('ikb19.outputTokens') }}</th><th>{{ t('ikb19.cost') }}</th></tr></thead><tbody><tr v-for="(row, i) in summaries" :key="i"><td>{{ row.period.slice(0, 10) }}</td><td :title="row.model_id">{{ row.model_id ? modelName(row.model_id) : t('ikb19.all') }}</td><td>{{ row.purpose ? purposeName(row.purpose) : t('ikb19.all') }}</td><td>{{ row.currency || t('ikb19.noCurrency') }}</td><td class="num">{{ row.calls }}</td><td class="num">{{ (row.failure_rate * 100).toFixed(2) }}% ({{ row.failures }})</td><td class="num">{{ row.average_duration_ms.toFixed(1) }} ms</td><td class="num">{{ value(row.input_tokens, row.unknown_input_calls) }}</td><td class="num">{{ value(row.output_tokens, row.unknown_output_calls) }}</td><td class="num">{{ value(row.cost, row.unknown_cost_calls, row.currency, row.not_applicable_cost_calls) }}</td></tr></tbody></table></div>
        </section>
        <section v-if="total || calls.length" class="analytics-stack"><h2>{{ t('ikb19.details') }}</h2><p class="muted">{{ t('ikb19.pagination', { start: calls.length ? offset + 1 : 0, end: Math.min(offset + calls.length, total), total }) }}</p>
          <DataTable v-if="calls.length" v-model:focused="focusedCall" :rows="calls" :columns="columns" :label="t('ikb19.details')" class="calls-table" @activate="viewingCall = $event.id" @click="openClickedRow">
            <template #cell-time="{ row: call }"><span class="diagnostics-nowrap">{{ new Date(call.requested_at).toLocaleString() }}</span><Button variant="ghost" @click="viewingCall = call.id">{{ t('ikb23.view') }}</Button></template>
            <template #cell-model="{ row: call }">{{ call.model_name }}<p class="muted diagnostics-mono" :title="call.model_id">{{ shortId(call.model_id) }}</p></template>
            <template #cell-purpose="{ row: call }">{{ purposeName(call.purpose) }}</template>
            <template #cell-entry="{ row: call }"><a v-if="call.entry_id" class="diagnostics-mono" :title="call.entry_id" :href="`/platform/entries/${encodeURIComponent(call.entry_id)}`">{{ shortId(call.entry_id) }}</a><span v-else>{{ t('ikb19.notApplicable') }}</span></template>
            <template #cell-result="{ row: call }"><Tag :variant="call.success ? 'plain' : 'danger'">{{ call.success ? t('ikb19.success') : t('ikb19.failed') }}</Tag><p v-if="call.error_category">{{ call.error_category }}</p></template>
            <template #cell-duration="{ row: call }">{{ call.duration_ms }} ms</template>
            <template #cell-input="{ row: call }">{{ value(call.input_tokens) }}<p v-if="call.text_input_tokens != null" class="muted">{{ t('ikb18.textTokens') }}: {{ value(call.text_input_tokens) }}</p><p v-if="call.image_input_tokens != null" class="muted">{{ t('ikb18.imageTokens') }}: {{ value(call.image_input_tokens) }}</p></template>
            <template #cell-output="{ row: call }">{{ value(call.output_tokens) }}</template>
            <template #cell-cost="{ row: call }">{{ call.cost_status === 'not_applicable' ? t('ikb19.notApplicable') : call.cost_status === 'unknown' ? t('ikb19.unknown') : value(call.cost, 0, call.currency) }}</template>
            <template #cell-attempt="{ row: call }">{{ call.attempt }}<p class="muted">{{ t('ikb19.retryNumber') }}: {{ call.retry_number }}</p></template>
          </DataTable>
          <div v-if="offset || total > 25" class="toolbar pagination"><Button :disabled="loading || offset === 0" @click="page(-1)">{{ t('ikb19.previous') }}</Button><Button :disabled="loading || offset + calls.length >= total" @click="page(1)">{{ t('ikb19.next') }}</Button></div>
        </section>
      </template>
      <Drawer :model-value="!!viewingCall" :title="t('ikb23.view')" content-class="analytics-dialog call-drawer" @update:model-value="!$event && (viewingCall = '')">
        <div v-if="viewingCall" class="call-detail">
          <dl v-if="viewingDetail" class="diagnostics-kv"><dt>{{ t('ikb19.model') }}</dt><dd>{{ viewingDetail.model_name }}</dd><dt>{{ t('ikb19.purpose') }}</dt><dd>{{ purposeName(viewingDetail.purpose) }}</dd><dt>{{ t('ikb19.time') }}</dt><dd>{{ new Date(viewingDetail.requested_at).toLocaleString() }}</dd><dt>{{ t('ikb19.entry') }}</dt><dd><a v-if="viewingDetail.entry_id" :href="`/platform/entries/${encodeURIComponent(viewingDetail.entry_id)}`">{{ viewingDetail.entry_id }}</a><span v-else>{{ t('ikb19.notApplicable') }}</span></dd><dt>{{ t('ikb19.result') }}</dt><dd>{{ viewingDetail.success ? t('ikb19.success') : t('ikb19.failed') }} {{ viewingDetail.error_category }}</dd><dt>{{ t('ikb19.elapsed') }}</dt><dd>{{ viewingDetail.duration_ms }} ms</dd><dt>{{ t('ikb19.attempt') }}</dt><dd>{{ viewingDetail.attempt }} · {{ t('ikb19.retryNumber') }} {{ viewingDetail.retry_number }}</dd></dl>
          <ModelCallContentViewer :call-id="viewingCall" />
        </div>
      </Drawer>
    </div>
  </main>
</template>
