<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { read, callDetails, type CallSummary, type ModelCall } from '@/api/insights'
import TrendChart from './TrendChart.vue'
import ModelCallContentViewer from '@/components/ModelCallContentViewer.vue'
const viewingCall = ref('')
import type { TrendMetric } from './trend'
import './insights.css'
const { t } = useI18n(), route = useRoute(), router = useRouter()
const models = ref<{ id: string; name: string }[]>([]), purposes = ref<string[]>([]), summaries = ref<CallSummary[]>([]), trend = ref<CallSummary[]>([]), calls = ref<ModelCall[]>([])
const initialFrom = new Date(); initialFrom.setDate(initialFrom.getDate() - 30)
const filters = ref({ from: initialFrom.toISOString().slice(0, 10), to: '', model_id: '', purpose: '' })
const applied = ref({ ...filters.value }), period = ref('day'), group = ref('model,purpose'), metric = ref<TrendMetric>('calls')
const entry = computed(() => typeof route.query.entry_id === 'string' ? route.query.entry_id : '')
const offset = ref(0), total = ref(0), loading = ref(true), error = ref(''), entryTitle = ref('')
let version = 0
const purposeLabels = computed<Record<string, string>>(() => ({ intake_review: t('ikb19.purposes.intakeReview'), article_processing: t('ikb19.purposes.processing'), segment_merge: t('ikb19.purposes.segmentMerge'), image_description: t('ikb19.purposes.imageDescription'), entity_resolution: t('ikb19.purposes.entityResolution'), entity_name_embedding: t('ikb19.purposes.entityEmbedding'), body_embedding: t('ikb19.purposes.bodyEmbedding'), rerank: t('ikb19.purposes.rerank'), evaluation_generation: t('ikb19.purposes.evaluation'), other: t('ikb19.purposes.other') }))
const modelOptions = computed(() => [...models.value, ...[...new Set([...summaries.value.map(s => s.model_id), ...calls.value.map(c => c.model_id)])].filter(id => id && !models.value.some(m => m.id === id)).map(id => ({ id, name: calls.value.find(c => c.model_id === id)?.model_name || id }))])
function modelName(id: string) { return models.value.find(m => m.id === id)?.name || id }
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
  if (all) { summaries.value = []; trend.value = []; calls.value = []; total.value = 0 }
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
function apply() { applied.value = { ...filters.value }; offset.value = 0; void load() }
function page(direction: number) { offset.value = Math.max(0, offset.value + direction * 25); void load(false) }
async function entryChanged() {
  entryTitle.value = ''; offset.value = 0; await load()
  const id = entry.value
  if (id) { try { const e = await read<{ title: string }>(`entries/${encodeURIComponent(id)}`); if (entry.value === id) entryTitle.value = e.title } catch { /* Deleted materials retain their call history. */ } }
}
watch([period, group], () => { offset.value = 0; void load() })
watch(entry, entryChanged)
onMounted(async () => { await initialize(); if (entry.value) await entryChanged() })
</script>
<template>
  <main class="insights" :aria-busy="loading">
    <h1>{{ t('ikb19.callsTitle') }}</h1><p class="muted">{{ t('ikb19.callsHint') }}</p>
    <div v-if="entry" class="notice">{{ t('ikb19.entryCalls') }}: <a :href="`/platform/entries?entry_id=${encodeURIComponent(entry)}`">{{ entryTitle || entry }}</a> <button @click="router.replace({ query: { ...route.query, entry_id: undefined } })">{{ t('ikb19.clearEntry') }}</button></div>
    <form class="panel" @submit.prevent="apply"><div class="fields"><label class="field">{{ t('ikb19.from') }}<input type="date" :value="filters.from" @input="filters.from = ($event.target as HTMLInputElement).value" /></label><label class="field">{{ t('ikb19.toExclusive') }}<input type="date" :value="filters.to" @input="filters.to = ($event.target as HTMLInputElement).value" /></label><label class="field">{{ t('ikb19.model') }}<select :value="filters.model_id" @change="filters.model_id = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.all') }}</option><option v-for="m in modelOptions" :key="m.id" :value="m.id">{{ m.name }}</option><option v-if="filters.model_id && !modelOptions.some(m => m.id === filters.model_id)" :value="filters.model_id">{{ filters.model_id }}</option></select></label><label class="field">{{ t('ikb19.purpose') }}<select :value="filters.purpose" @change="filters.purpose = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.all') }}</option><option v-for="p in purposes" :key="p" :value="p">{{ purposeName(p) }}</option></select></label></div><p class="muted">{{ t('ikb19.utcHint') }}</p><div class="toolbar"><button type="submit" class="primary" :disabled="loading">{{ t('ikb19.apply') }}</button><button type="button" :disabled="loading" @click="filters = { from: '', to: '', model_id: '', purpose: '' }; apply()">{{ t('ikb19.reset') }}</button></div></form>
    <div v-if="error" role="alert" class="notice error">{{ error }} <button :disabled="loading" @click="initialize">{{ t('ikb19.retry') }}</button></div>
    <p v-if="loading" role="status">{{ t('ikb19.loading') }}</p>
    <p v-if="!loading && !total && !error" class="empty">{{ t('ikb19.noCalls') }}</p>
    <section class="panel"><h2>{{ t('ikb19.summary') }}</h2><div class="toolbar"><label class="field">{{ t('ikb19.period') }}<select :value="period" :disabled="loading" @change="period = ($event.target as HTMLSelectElement).value"><option value="day">{{ t('ikb19.day') }}</option><option value="week">{{ t('ikb19.week') }}</option><option value="month">{{ t('ikb19.month') }}</option></select></label><label class="field">{{ t('ikb19.groupBy') }}<select :value="group" :disabled="loading" @change="group = ($event.target as HTMLSelectElement).value"><option value="model,purpose">{{ t('ikb19.modelAndPurpose') }}</option><option value="model">{{ t('ikb19.model') }}</option><option value="purpose">{{ t('ikb19.purpose') }}</option></select></label></div>
      <p class="muted">{{ t('ikb19.partialHint') }}</p>
      <div class="table-scroll"><table><thead><tr><th>{{ t('ikb19.time') }} UTC</th><th>{{ t('ikb19.model') }}</th><th>{{ t('ikb19.purpose') }}</th><th>{{ t('ikb19.currency') }}</th><th>{{ t('ikb19.calls') }}</th><th>{{ t('ikb19.failureRate') }}</th><th>{{ t('ikb19.duration') }}</th><th>{{ t('ikb19.inputTokens') }}</th><th>{{ t('ikb19.outputTokens') }}</th><th>{{ t('ikb19.cost') }}</th></tr></thead><tbody><tr v-for="(row, i) in summaries" :key="i"><td>{{ row.period.slice(0, 10) }}</td><td>{{ row.model_id ? modelName(row.model_id) : t('ikb19.all') }}</td><td>{{ row.purpose ? purposeName(row.purpose) : t('ikb19.all') }}</td><td>{{ row.currency || t('ikb19.noCurrency') }}</td><td>{{ row.calls }}</td><td>{{ (row.failure_rate * 100).toFixed(2) }}% ({{ row.failures }})</td><td>{{ row.average_duration_ms.toFixed(1) }} ms</td><td>{{ value(row.input_tokens, row.unknown_input_calls) }}</td><td>{{ value(row.output_tokens, row.unknown_output_calls) }}</td><td>{{ value(row.cost, row.unknown_cost_calls, row.currency, row.not_applicable_cost_calls) }}</td></tr></tbody></table></div>
    </section>
    <section class="panel"><h2>{{ t('ikb19.dailyTrend') }}</h2><label class="field">{{ t('ikb19.trendMetric') }}<select :value="metric" @change="metric = ($event.target as HTMLSelectElement).value as TrendMetric"><option value="calls">{{ t('ikb19.calls') }}</option><option value="tokens">{{ t('ikb19.tokens') }}</option><option value="cost">{{ t('ikb19.cost') }}</option></select></label><TrendChart :rows="trend" :metric="metric" /></section>
    <section class="panel"><h2>{{ t('ikb19.details') }}</h2><p class="muted">{{ t('ikb19.pagination', { start: total ? offset + 1 : 0, end: Math.min(offset + calls.length, total), total }) }}</p><div class="table-scroll"><table><thead><tr><th>{{ t('ikb19.time') }}</th><th>{{ t('ikb19.model') }}</th><th>{{ t('ikb19.purpose') }}</th><th>{{ t('ikb19.entry') }}</th><th>{{ t('ikb19.result') }}</th><th>{{ t('ikb19.elapsed') }}</th><th>{{ t('ikb19.inputTokens') }}</th><th>{{ t('ikb19.outputTokens') }}</th><th>{{ t('ikb19.cost') }}</th><th>{{ t('ikb19.attempt') }}</th></tr></thead><tbody><tr v-for="call in calls" :key="call.id"><td>{{ new Date(call.requested_at).toLocaleString() }}<button type="button" @click="viewingCall = call.id">{{ t('ikb23.view') }}</button></td><td>{{ call.model_name }}<p class="muted">{{ call.model_id }}</p></td><td>{{ purposeName(call.purpose) }}</td><td><a v-if="call.entry_id" :href="`/platform/entries?entry_id=${encodeURIComponent(call.entry_id)}`">{{ call.entry_id }}</a><span v-else>{{ t('ikb19.notApplicable') }}</span></td><td>{{ call.success ? t('ikb19.success') : t('ikb19.failed') }}<p v-if="call.error_category">{{ call.error_category }}</p></td><td>{{ call.duration_ms }} ms</td><td>{{ value(call.input_tokens) }}<p v-if="call.text_input_tokens != null" class="muted">{{ t('ikb18.textTokens') }}: {{ value(call.text_input_tokens) }}</p><p v-if="call.image_input_tokens != null" class="muted">{{ t('ikb18.imageTokens') }}: {{ value(call.image_input_tokens) }}</p></td><td>{{ value(call.output_tokens) }}</td><td>{{ call.cost_status === 'not_applicable' ? t('ikb19.notApplicable') : call.cost_status === 'unknown' ? t('ikb19.unknown') : value(call.cost, 0, call.currency) }}</td><td>{{ call.attempt }}<p class="muted">{{ t('ikb19.retryNumber') }}: {{ call.retry_number }}</p></td></tr></tbody></table></div><div class="toolbar"><button :disabled="loading || offset === 0" @click="page(-1)">{{ t('ikb19.previous') }}</button><button :disabled="loading || offset + calls.length >= total" @click="page(1)">{{ t('ikb19.next') }}</button></div></section>
    <section v-if="viewingCall" class="panel" role="region" :aria-label="t('ikb23.view')"><button type="button" @click="viewingCall = ''">{{ t('ikb23.close') }}</button><ModelCallContentViewer :call-id="viewingCall" /></section>
  </main>
</template>
