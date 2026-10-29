<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Tag } from '@/components/ui'
import { metricKeys, metricLabels, type EvaluationRun, type Comparison, type RunResult } from '@/api/insights'
defineProps<{ run?: EvaluationRun; comparison?: Comparison; beforeRun?: EvaluationRun; afterRun?: EvaluationRun }>()
const { t } = useI18n()
const number = (v: number | undefined, signed = false) => v == null ? t('ikb19.notApplicable') : `${signed && v > 0 ? '+' : ''}${v.toFixed(4)}`
const deltaClass = (v: number) => v > 0 ? 'metric-up' : v < 0 ? 'metric-down' : ''
const shortId = (id: string) => id.length > 12 ? id.slice(0, 8) : id
function firstHit(row?: RunResult) {
  if (!row) return t('ikb19.notApplicable')
  const index = row.results?.results.findIndex(entry => row.hits?.includes(entry.entry_id || entry.id || '')) ?? -1
  return index < 0 ? t('ikb19.noHits') : String(index + 1)
}
</script>
<template>
  <section v-if="comparison" class="analytics-stack comparison-results">
    <h2>{{ t('ikb19.comparison') }}</h2><p class="muted">{{ t('ikb19.deltaHint') }}</p>
    <div v-if="beforeRun && afterRun" class="run-configs">
      <div v-for="(item, index) in [beforeRun, afterRun]" :key="item.id" class="run-config">
        <span class="muted">{{ index === 0 ? t('ikb19.before') : t('ikb19.after') }} · {{ new Date(item.created_at).toLocaleString() }}</span>
        <strong :class="{ 'run-config-changed': beforeRun.options.k !== afterRun.options.k }">{{ item.set_snapshot.name }} · K={{ item.options.k }}</strong>
        <span :class="{ 'run-config-changed': beforeRun.options.vector_set !== afterRun.options.vector_set }">{{ t('ikb19.vectorSet') }}: {{ item.options.vector_set }}</span>
        <span :class="{ 'run-config-changed': beforeRun.options.rerank_model_id !== afterRun.options.rerank_model_id }">{{ t('ikb19.rerank') }}: {{ item.options.rerank_model_id === '' ? t('ikb19.disabled') : item.options.rerank_model_id || t('ikb19.libraryDefault') }}</span>
        <span :class="{ 'run-config-changed': beforeRun.options.collapse_similar !== afterRun.options.collapse_similar }">{{ t('ikb19.collapse') }}: {{ item.options.collapse_similar ? t('common.yes') : t('common.no') }}</span>
      </div>
    </div>
    <div class="analytics-metrics" :aria-label="t('ikb19.comparison')">
      <div v-for="(key, index) in metricKeys" :key="key" class="analytics-metric"><span class="metric-label">{{ metricLabels[index] }}</span><strong class="metric-value">{{ number(comparison.after_summary[key]) }}</strong><span class="metric-before">{{ t('ikb19.before') }} {{ number(comparison.before_summary[key]) }}</span><span class="metric-delta" :class="deltaClass(comparison.delta[key])">{{ number(comparison.delta[key], true) }}</span></div>
    </div>
    <h3>{{ t('ikb19.perQuestion') }}</h3>
    <div v-if="comparison.questions.length" class="table-scroll comparison-table" tabindex="0" role="region" :aria-label="t('ikb19.perQuestion')"><table class="rq-table"><thead><tr><th>{{ t('ikb19.query') }}</th><th>{{ t('diagnosticsB.firstHit') }}</th><th>MRR {{ t('ikb19.before') }}</th><th>MRR {{ t('ikb19.after') }}</th><th v-for="label in metricLabels" :key="label">{{ label }} Δ</th><th>{{ t('ikb19.addedHits') }}</th><th>{{ t('ikb19.lostHits') }}</th></tr></thead><tbody><tr v-for="row in comparison.questions" :key="row.question_id"><td class="question-cell">{{ row.after?.question.request.query || row.before?.question.request.query }}<p class="muted">{{ row.question_changed ? t('ikb19.questionChanged') : row.change === 'added' ? t('ikb19.questionAdded') : row.change === 'removed' ? t('ikb19.questionRemoved') : t('ikb19.unchanged') }} · {{ t('ikb19.revision') }} {{ row.before?.question.revision }} → {{ row.after?.question.revision }}</p><details><summary>{{ t('ikb19.beforeAfter') }}</summary><pre>{{ JSON.stringify({ before: row.before, after: row.after }, null, 2) }}</pre></details></td><td class="num">{{ firstHit(row.before) }} → {{ firstHit(row.after) }}</td><td class="num">{{ number(row.before?.metrics.mrr) }}</td><td class="num">{{ number(row.after?.metrics.mrr) }}</td><td v-for="key in metricKeys" :key="key" class="num" :class="deltaClass(row.delta[key])">{{ number(row.delta[key], true) }}</td><td><a v-for="id in row.added_hits" :key="id" :title="id" :href="`/platform/entries/${encodeURIComponent(id)}`">{{ shortId(id) }} </a></td><td><a v-for="id in row.lost_hits" :key="id" :title="id" :href="`/platform/entries/${encodeURIComponent(id)}`">{{ shortId(id) }} </a></td></tr></tbody></table></div>
  </section>
  <section v-if="run" class="analytics-stack run-results">
    <h2>{{ t('ikb19.runDetail') }}: {{ run.set_snapshot.name }}</h2>
    <p><span class="diagnostics-mono" :title="run.id">{{ shortId(run.id) }}</span> <Tag>{{ run.status === 'completed' ? t('ikb19.completed') : run.status === 'failed' ? t('ikb19.failed') : t('ikb19.running') }}</Tag></p>
    <p v-if="run.status === 'failed'" role="alert" class="notice error">{{ t('ikb19.runFailed') }} {{ run.error }} {{ t('ikb19.runFailedNext') }}</p>
    <p v-if="run.status === 'running'" class="notice">{{ t('ikb19.runningHint') }}</p>
    <div class="analytics-metrics"><div v-for="(key, index) in metricKeys" :key="key" class="analytics-metric"><span class="metric-label">{{ metricLabels[index] }}</span><strong class="metric-value">{{ number(run.summary[key]) }}</strong></div></div>
    <details><summary>{{ t('ikb19.snapshot') }}</summary><pre>{{ JSON.stringify({ options: run.options, snapshot: run.snapshot }, null, 2) }}</pre></details>
    <h3 v-if="run.questions?.length">{{ t('ikb19.perQuestion') }}</h3>
    <div v-if="run.questions?.length" class="table-scroll run-results-table" tabindex="0" role="region" :aria-label="t('ikb19.perQuestion')"><table class="rq-table"><thead><tr><th>{{ t('ikb19.query') }}</th><th v-for="label in metricLabels" :key="label">{{ label }}</th><th>{{ t('ikb19.hits') }}</th><th>{{ t('ikb19.results') }}</th></tr></thead>
      <tbody><tr v-for="row in run.questions" :key="row.question_id"><td class="question-cell">{{ row.question.request.query }}<p class="muted">{{ t('ikb19.revision') }} {{ row.question.revision }}</p><p v-if="row.error" class="notice error">{{ row.error }}</p><details><summary>{{ t('ikb19.snapshot') }}</summary><pre>{{ JSON.stringify({ question: row.question, snapshot: row.snapshot }, null, 2) }}</pre></details></td><td v-for="key in metricKeys" :key="key" class="num">{{ number(row.metrics[key]) }}</td><td><a v-for="id in row.hits || []" :key="id" :title="id" :href="`/platform/entries/${encodeURIComponent(id)}`">{{ shortId(id) }} </a><span v-if="!row.hits?.length">{{ t('ikb19.noHits') }}</span></td><td><ol><li v-for="(entry, index) in row.results?.results || []" :key="index"><a :title="entry.entry_id || entry.id" :href="`/platform/entries/${encodeURIComponent(entry.entry_id || entry.id || '')}`">{{ entry.title || shortId(entry.entry_id || entry.id || '') }}</a><Tag>{{ row.hits?.includes(entry.entry_id || entry.id || '') ? t('ikb19.hit') : t('ikb19.miss') }}</Tag></li></ol></td></tr></tbody>
    </table></div>
  </section>
</template>
