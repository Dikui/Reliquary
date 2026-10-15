<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CallSummary } from '@/api/insights'
import { mapTrend, chartGeometry, type TrendMetric } from './trend'
const props = defineProps<{ rows: CallSummary[]; metric: TrendMetric }>()
const { t } = useI18n()
const series = computed(() => mapTrend(props.rows, props.metric))
const maximum = computed(() => Math.max(0, ...series.value.flatMap(s => s.points.map(p => p.value || 0))) || 1)
const plots = computed(() => series.value.map(s => ({ ...s, ...chartGeometry(s.points, maximum.value) })))
const colors = ['var(--td-brand-color)', 'var(--td-success-color)', 'var(--td-warning-color)', 'var(--td-error-color)', 'var(--td-text-color-primary)']
const format = (value: number) => props.metric === 'cost' ? value.toFixed(6) : value.toLocaleString()
</script>
<template>
  <div v-if="!rows.length" class="empty">{{ t('ikb19.noTrend') }}</div>
  <figure v-else class="trend-figure">
    <figcaption>{{ t('ikb19.dailyTrend') }} · {{ metric === 'calls' ? t('ikb19.calls') : metric === 'tokens' ? t('ikb19.tokens') : t('ikb19.cost') }} (UTC)</figcaption>
    <svg viewBox="0 0 750 255" role="img" :aria-label="t('ikb19.dailyTrend')">
      <title>{{ t('ikb19.dailyTrend') }}</title><desc>{{ t('ikb19.trendHint') }}</desc>
      <g v-for="fraction in [0, .5, 1]" :key="fraction"><line x1="54" x2="704" :y1="210 - fraction * 175" :y2="210 - fraction * 175" stroke="var(--td-component-stroke)" /><text x="48" :y="214 - fraction * 175" text-anchor="end">{{ format(maximum * fraction) }}</text></g>
      <g v-for="(plot, index) in plots" :key="plot.currency" :data-currency="plot.currency">
        <polyline v-for="(segment, part) in plot.segments" :key="part" :points="segment" fill="none" :stroke="colors[index % colors.length]" stroke-width="2" />
        <template v-for="point in plot.points" :key="point.date"><circle v-if="point.y !== null" :cx="point.x" :cy="point.y" r="3" :fill="colors[index % colors.length]"><title>{{ point.date }} {{ plot.currency }}: {{ format(point.value!) }}{{ point.unknown ? ` (${t('ikb19.partial')})` : '' }}</title></circle></template>
      </g>
      <text x="54" y="240">{{ series[0]?.points[0]?.date }}</text><text x="704" y="240" text-anchor="end">{{ series[0]?.points.at(-1)?.date }}</text>
    </svg>
    <div class="actions"><span v-for="(plot, index) in plots" :key="plot.currency" :style="{ color: colors[index % colors.length] }">● {{ plot.currency || t('ikb19.noCurrency') }}</span></div>
    <p class="muted">{{ t('ikb19.trendHint') }}</p>
    <details><summary>{{ t('ikb19.chartData') }}</summary><div class="table-scroll"><table><thead><tr><th>{{ t('ikb19.time') }} UTC</th><th>{{ t('ikb19.currency') }}</th><th>{{ t('ikb19.value') }}</th><th>{{ t('ikb19.unknown') }}</th><th>{{ t('ikb19.notApplicable') }}</th></tr></thead><tbody><template v-for="plot in series" :key="plot.currency"><tr v-for="point in plot.points" :key="point.date"><td>{{ point.date }}</td><td>{{ plot.currency }}</td><td>{{ point.value === null ? (point.unknown ? t('ikb19.unknown') : point.notApplicable ? t('ikb19.notApplicable') : t('ikb19.unknown')) : format(point.value) }} {{ point.unknown && point.value !== null ? t('ikb19.partial') : '' }}</td><td>{{ point.unknown }}</td><td>{{ point.notApplicable }}</td></tr></template></tbody></table></div></details>
  </figure>
</template>
<style scoped>.trend-figure { margin: 16px 0; } svg { width: 100%; height: auto; display: block; min-width: 0; } svg text { fill: var(--td-text-color-secondary); font-size: 11px; } figcaption { margin-bottom: 8px; }</style>
