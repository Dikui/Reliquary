<script setup lang="ts">
import { computed } from 'vue'
import { statuses, summarizeShelf, type Region, type ShelfFilters, type ShelfRow } from '../data/shelf'

const props = defineProps<{ rows: ShelfRow[]; regions: Region[]; filters: ShelfFilters; asOf: string; date: string }>()
const emit = defineEmits<{ filter: [region: string, status: string] }>()

const summary = computed(() => summarizeShelf(props.rows))
const areaBars = computed(() => {
  const regions = new Map(props.regions.map(({ key, name }) => [key, name]))
  for (const row of props.rows) {
    if (row.regionKey && !regions.has(row.regionKey)) regions.set(row.regionKey, row.regionLabel)
  }
  return Array.from(regions, ([key, label]) => ({
    key, label, count: summary.value.byRegion.get(key) ?? 0,
  }))
})
const statusBars = computed(() => statuses.map((status) => ({
  ...status, count: summary.value.byStatus.get(status.key) ?? 0,
})))
const areaMax = computed(() => Math.max(1, ...areaBars.value.map(({ count }) => count)))
const statusMax = computed(() => Math.max(1, ...statusBars.value.map(({ count }) => count)))
</script>

<template>
  <section aria-labelledby="overview-title">
    <div class="section-head">
      <h2 id="overview-title" tabindex="-1">货架总览</h2>
      <div v-if="date" class="snapshot">数据截至 <time :datetime="asOf">{{ date }}</time></div>
    </div>
    <div class="metrics">
      <button class="metric" type="button" @click="emit('filter', '', '')">
        <span class="metric-action"><span class="metric-label">货架记录</span><span class="arw" aria-hidden="true"></span></span>
        <span class="metric-value"><strong>{{ rows.length }}</strong><span>条</span></span>
      </button>
      <div class="metric">
        <span class="metric-action"><span class="metric-label">适用市场</span></span>
        <span class="metric-value"><strong>{{ summary.byRegion.size }}</strong><span>个</span></span>
      </div>
      <button class="metric" type="button" :aria-pressed="filters.status === 'GA'" @click="emit('filter', '', filters.status === 'GA' ? '' : 'GA')">
        <span class="metric-action"><span class="metric-label">已量产</span><span class="arw" aria-hidden="true"></span></span>
        <span class="metric-value"><strong>{{ summary.byStatus.get('GA') ?? 0 }}</strong><span>条</span></span>
      </button>
    </div>
    <div class="charts">
      <section class="panel chart" aria-labelledby="area-chart">
        <h3 id="area-chart">市场覆盖</h3><p>选择市场查看对应产品</p>
        <div class="chart-rows">
          <button v-for="bar in areaBars" :key="bar.key" class="chart-row" type="button" :aria-pressed="filters.region === bar.key"
            @click="emit('filter', filters.region === bar.key ? '' : bar.key, '')">
            <span>{{ bar.label }}</span>
            <span class="track" aria-hidden="true"><span class="bar" :style="{ width: bar.count / areaMax * 100 + '%', background: '#428eff' }"></span></span>
            <span class="chart-count">{{ bar.count }}</span>
          </button>
        </div>
      </section>
      <section class="panel chart" aria-labelledby="ga-chart">
        <h3 id="ga-chart">量产节点（GA）</h3><p>按可售状态统计</p>
        <div class="chart-rows">
          <button v-for="bar in statusBars" :key="bar.key" class="chart-row" type="button" :aria-pressed="filters.status === bar.key"
            @click="emit('filter', '', filters.status === bar.key ? '' : bar.key)">
            <span>{{ bar.label }}</span>
            <span class="track" aria-hidden="true"><span class="bar" :style="{ width: bar.count / statusMax * 100 + '%', background: bar.color }"></span></span>
            <span class="chart-count">{{ bar.count }}</span>
          </button>
        </div>
      </section>
    </div>
  </section>
</template>
