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
const format = (value: number) => value.toLocaleString(undefined, { maximumSignificantDigits: 8 })
const tickLabel = (value: number) => value.toLocaleString(undefined, { notation: value >= 10000 ? 'compact' : value > 0 && value < .0001 ? 'scientific' : 'standard', maximumSignificantDigits: 4 })
</script>
<template>
  <p v-if="!rows.length" class="diagnostics-muted">{{ t('ikb19.noTrend') }}</p>
  <div v-else class="trend-charts">
    <div class="trend-legend"><span><i class="trend-success" />{{ metric === 'calls' ? t('ikb19.success') : t('ikb19.value') }}</span><span v-if="metric === 'calls'"><i class="trend-failure" />{{ t('ikb19.failed') }}</span></div>
    <figure v-for="plot in plots" :key="plot.currency" class="trend-figure" :data-currency="plot.currency">
      <figcaption>{{ t('ikb19.dailyTrend') }} · {{ metric === 'calls' ? t('ikb19.calls') : metric === 'tokens' ? t('ikb19.tokens') : t('ikb19.cost') }} · {{ plot.currency || t('ikb19.noCurrency') }} (UTC)</figcaption>
      <div class="trend-scroll" tabindex="0" role="region" :aria-label="t('ikb19.dailyTrend')">
        <svg viewBox="0 0 750 255" role="img" :aria-label="`${t('ikb19.dailyTrend')} ${plot.currency}`">
          <title>{{ t('ikb19.dailyTrend') }} {{ plot.currency }}</title><desc>{{ t('ikb19.trendHint') }}</desc>
          <g v-for="tick in plot.ticks" :key="tick.y"><line class="trend-grid" x1="70" x2="704" :y1="tick.y" :y2="tick.y" /><text x="64" :y="tick.y + 4" text-anchor="end">{{ tickLabel(tick.value) }}</text></g>
          <line class="trend-base" x1="70" x2="704" y1="210" y2="210" />
          <g v-for="point in plot.bars" :key="point.date">
            <title>{{ point.date }}: {{ point.value === null ? t('ikb19.unknown') : format(point.value) }}{{ point.unknown ? ` (${t('ikb19.partial')})` : '' }}{{ metric === 'calls' ? `; ${t('ikb19.failed')}: ${point.failures || 0}` : '' }}</title>
            <template v-if="point.y !== null"><rect class="trend-success" :x="point.x" :y="point.successY" :width="point.width" :height="point.successHeight" /><rect v-if="point.failureHeight" class="trend-failure" :x="point.x" :y="point.failureY" :width="point.width" :height="point.failureHeight" /></template>
          </g>
          <text x="70" y="240">{{ plot.bars[0]?.date }}</text><text x="704" y="240" text-anchor="end">{{ plot.bars.at(-1)?.date }}</text>
        </svg>
      </div>
    </figure>
    <p class="diagnostics-muted">{{ t('ikb19.trendHint') }}</p>
    <details><summary>{{ t('ikb19.chartData') }}</summary><div class="rq-table-wrap" tabindex="0" role="region" :aria-label="t('ikb19.chartData')"><table class="rq-table trend-table"><thead><tr><th>{{ t('ikb19.time') }} UTC</th><th>{{ t('ikb19.currency') }}</th><th>{{ t('ikb19.value') }}</th><th v-if="metric === 'calls'">{{ t('ikb19.failed') }}</th><th>{{ t('ikb19.unknown') }}</th><th>{{ t('ikb19.notApplicable') }}</th></tr></thead><tbody><template v-for="plot in series" :key="plot.currency"><tr v-for="point in plot.points" :key="point.date"><td>{{ point.date }}</td><td>{{ plot.currency || t('ikb19.noCurrency') }}</td><td>{{ point.value === null ? (point.unknown ? t('ikb19.unknown') : point.notApplicable ? t('ikb19.notApplicable') : t('ikb19.unknown')) : format(point.value) }} {{ point.unknown && point.value !== null ? t('ikb19.partial') : '' }}</td><td v-if="metric === 'calls'">{{ point.failures || 0 }}</td><td>{{ point.unknown }}</td><td>{{ point.notApplicable }}</td></tr></template></tbody></table></div></details>
  </div>
</template>
<style scoped>
.trend-charts { display: grid; gap: var(--space-3); min-width: 0; }
.trend-figure { margin: 0; min-width: 0; }
.trend-scroll { overflow-x: auto; }
.trend-scroll svg { width: 100%; height: auto; display: block; min-width: 560px; font-family: var(--mono); }
.trend-scroll text { fill: var(--muted); font-size: 11px; }
.trend-grid { stroke: var(--border); stroke-width: 1; }
.trend-base { stroke: var(--text); stroke-width: 2; }
.trend-success { fill: var(--text); background: var(--text); }
.trend-failure { fill: var(--danger); background: var(--danger); }
.trend-legend { display: flex; gap: var(--space-3); font-size: var(--text-xs); }
.trend-legend i { display: inline-block; width: 11px; height: 11px; margin-right: var(--space-1); }
figcaption { margin-bottom: var(--space-2); font-size: var(--text-sm); }
.trend-table { min-width: 680px; }
</style>
