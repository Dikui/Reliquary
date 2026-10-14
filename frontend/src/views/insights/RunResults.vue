<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { metricKeys, metricLabels, type EvaluationRun, type Comparison } from '@/api/insights'
defineProps<{ run?: EvaluationRun; comparison?: Comparison }>()
const { t } = useI18n()
const number = (v: number | undefined, signed = false) => v == null ? t('ikb19.notApplicable') : `${signed && v > 0 ? '+' : ''}${v.toFixed(4)}`
</script>
<template>
  <section v-if="run" class="panel">
    <h2>{{ t('ikb19.runDetail') }}: {{ run.set_snapshot.name }}</h2>
    <p>{{ run.id }} <span class="badge">{{ run.status === 'completed' ? t('ikb19.completed') : run.status === 'failed' ? t('ikb19.failed') : t('ikb19.running') }}</span></p>
    <p v-if="run.status === 'failed'" role="alert" class="notice error">{{ t('ikb19.runFailed') }} {{ run.error }} {{ t('ikb19.runFailedNext') }}</p>
    <p v-if="run.status === 'running'" class="notice">{{ t('ikb19.runningHint') }}</p>
    <div class="metric-grid"><div v-for="(key, index) in metricKeys" :key="key">{{ metricLabels[index] }}<strong>{{ number(run.summary[key]) }}</strong></div></div>
    <details><summary>{{ t('ikb19.snapshot') }}</summary><pre>{{ JSON.stringify({ options: run.options, snapshot: run.snapshot }, null, 2) }}</pre></details>
    <h3>{{ t('ikb19.perQuestion') }}</h3>
    <div class="table-scroll"><table><thead><tr><th>{{ t('ikb19.query') }}</th><th v-for="label in metricLabels" :key="label">{{ label }}</th><th>{{ t('ikb19.hits') }}</th><th>{{ t('ikb19.results') }}</th></tr></thead>
      <tbody><tr v-for="row in run.questions || []" :key="row.question_id"><td>{{ row.question.request.query }}<p class="muted">{{ t('ikb19.revision') }} {{ row.question.revision }}</p><p v-if="row.error" class="notice error">{{ row.error }}</p><details><summary>{{ t('ikb19.snapshot') }}</summary><pre>{{ JSON.stringify({ question: row.question, snapshot: row.snapshot }, null, 2) }}</pre></details></td><td v-for="key in metricKeys" :key="key">{{ number(row.metrics[key]) }}</td><td><a v-for="id in row.hits || []" :key="id" :href="`/platform/entries?entry_id=${encodeURIComponent(id)}`">{{ id }} </a><span v-if="!row.hits?.length">{{ t('ikb19.noHits') }}</span></td><td><ol><li v-for="(entry, index) in row.results?.results || []" :key="index"><a :href="`/platform/entries?entry_id=${encodeURIComponent(entry.entry_id || entry.id || '')}`">{{ entry.title || entry.entry_id || entry.id }}</a><span class="badge">{{ row.hits?.includes(entry.entry_id || entry.id || '') ? t('ikb19.hit') : t('ikb19.miss') }}</span></li></ol></td></tr></tbody>
    </table></div>
  </section>
  <section v-if="comparison" class="panel">
    <h2>{{ t('ikb19.comparison') }}</h2><p class="muted">{{ t('ikb19.deltaHint') }}</p>
    <div class="table-scroll"><table><thead><tr><th>{{ t('ikb19.metric') }}</th><th>{{ t('ikb19.before') }}</th><th>{{ t('ikb19.after') }}</th><th>{{ t('ikb19.delta') }}</th></tr></thead><tbody><tr v-for="(key, index) in metricKeys" :key="key"><td>{{ metricLabels[index] }}</td><td>{{ number(comparison.before_summary[key]) }}</td><td>{{ number(comparison.after_summary[key]) }}</td><td>{{ number(comparison.delta[key], true) }}</td></tr></tbody></table></div>
    <h3>{{ t('ikb19.perQuestion') }}</h3><div class="table-scroll"><table><thead><tr><th>{{ t('ikb19.query') }}</th><th>{{ t('ikb19.revision') }}</th><th v-for="label in metricLabels" :key="label">{{ label }} Δ</th><th>{{ t('ikb19.addedHits') }}</th><th>{{ t('ikb19.lostHits') }}</th></tr></thead><tbody><tr v-for="row in comparison.questions" :key="row.question_id"><td>{{ row.after?.question.request.query || row.before?.question.request.query }}<details><summary>{{ t('ikb19.beforeAfter') }}</summary><pre>{{ JSON.stringify({ before: row.before, after: row.after }, null, 2) }}</pre></details></td><td>{{ row.question_changed ? t('ikb19.questionChanged') : row.change === 'added' ? t('ikb19.questionAdded') : row.change === 'removed' ? t('ikb19.questionRemoved') : t('ikb19.unchanged') }}<p>{{ row.before?.question.revision }} → {{ row.after?.question.revision }}</p></td><td v-for="key in metricKeys" :key="key">{{ number(row.delta[key], true) }}</td><td><a v-for="id in row.added_hits" :key="id" :href="`/platform/entries?entry_id=${encodeURIComponent(id)}`">{{ id }} </a></td><td><a v-for="id in row.lost_hits" :key="id" :href="`/platform/entries?entry_id=${encodeURIComponent(id)}`">{{ id }} </a></td></tr></tbody></table></div>
  </section>
</template>
