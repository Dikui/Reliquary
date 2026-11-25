<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { filterShelf, statuses, type ShelfFilters, type ShelfRow, type ShelfSnapshot } from '../data/shelf'

const props = defineProps<{
  snapshot: ShelfSnapshot
  rows: ShelfRow[]
  filters: ShelfFilters
  compared: ShelfRow[]
}>()
const emit = defineEmits<{
  filters: [changes: Partial<ShelfFilters>]
  reset: []
  open: [row: ShelfRow]
  toggle: [row: ShelfRow]
  clear: []
}>()

const pageSize = 12
const compareLimit = 3
const page = ref(1)
const filtered = computed(() => filterShelf(props.rows, props.filters))
const hasFilters = computed(() => Boolean(props.filters.region || props.filters.productLine || props.filters.status || props.filters.query.trim()))
const pages = computed(() => Math.max(1, Math.ceil(filtered.value.length / pageSize)))
const first = computed(() => (page.value - 1) * pageSize)
const visible = computed(() => filtered.value.slice(first.value, first.value + pageSize))
const selectedStatus = computed(() => statuses.find((status) => status.key === props.filters.status))
const query = computed({
  get: () => props.filters.query,
  set: (value: string) => emit('filters', { query: value }),
})
const comparedIds = computed(() => new Set(props.compared.map((row) => row.model.id)))
const comparedRegions = computed(() => {
  const byModel = new Map<string, ShelfRow[]>()
  for (const row of props.rows) {
    if (!comparedIds.value.has(row.model.id)) continue
    const group = byModel.get(row.model.id) ?? []
    group.push(row)
    byModel.set(row.model.id, group)
  }
  return byModel
})

watch(() => props.filters, () => { page.value = 1 })
watch(pages, () => { page.value = Math.min(page.value, pages.value) })

function updateFilter(key: keyof ShelfFilters, event: Event) {
  emit('filters', { [key]: (event.target as HTMLInputElement | HTMLSelectElement).value })
}

function compareLabel(row: ShelfRow) {
  return (comparedIds.value.has(row.model.id) ? '取消对比 ' : '加入对比 ') + row.model.name
}
</script>

<template>
  <section class="catalog-section" aria-labelledby="catalog-title">
    <div class="section-head">
      <div><h2 id="catalog-title" tabindex="-1">产品清单</h2><p>点击型号查看详情，或勾选 2–3 个型号进行比较。</p></div>
    </div>
    <form class="filters panel" role="search" aria-label="筛选产品" @submit.prevent>
      <div class="field">
        <label for="area">适用国家或地区</label>
        <div class="box select-box">
          <select id="area" :value="filters.region" @change="updateFilter('region', $event)">
            <option value="">全部地区</option>
            <option v-for="region in snapshot.regions" :key="region.key" :value="region.key">{{ region.name }}</option>
          </select>
        </div>
      </div>
      <div class="field">
        <label for="product-line-type">产品线类型</label>
        <div class="box select-box">
          <select id="product-line-type" :value="filters.productLine" @change="updateFilter('productLine', $event)">
            <option value="">全部</option>
            <option v-for="line in snapshot.product_lines" :key="line.id" :value="line.id">{{ line.name }}</option>
          </select>
        </div>
      </div>
      <div class="field">
        <label for="query">搜索型号或名称</label>
        <div class="box search-box">
          <input id="query" v-model="query" type="search" autocomplete="off" placeholder="输入型号或产品名称">
        </div>
      </div>
      <button class="button reset" type="button" :disabled="!hasFilters && !filters.query" @click="emit('reset')">重置筛选</button>
      <button v-if="selectedStatus" class="button catalog-ga-filter" type="button" @click="emit('filters', { status: '' })">
        <span>GA 筛选：{{ selectedStatus.label }}</span><span aria-hidden="true">×</span>
      </button>
    </form>
    <section v-if="compared.length" class="compare-panel" aria-labelledby="compare-title">
      <div class="compare-heading">
        <div>
          <h2 id="compare-title" tabindex="-1">型号对比</h2>
          <p>{{ compared.length < 2 ? '再选择 1 个型号开始对比' : '已选 ' + compared.length + ' 个型号 · 点击列首查看详情' }}</p>
        </div>
        <button class="compare-clear" type="button" @click="emit('clear')">清空</button>
      </div>
      <div class="compare-selection">
        <div v-for="row in compared" :key="row.model.id" class="compare-selected">
          <button type="button" :aria-label="'从对比中移除 ' + row.model.name" @click="emit('toggle', row)">
            {{ row.model.product_name || row.model.name }}<span aria-hidden="true">×</span>
          </button>
        </div>
        <span class="compare-limit">{{ compared.length }} / {{ compareLimit }}</span>
      </div>
      <div v-if="compared.length >= 2" class="compare-table-scroll">
        <table class="compare-table">
          <thead>
            <tr>
              <th scope="col">对比项目</th>
              <th v-for="row in compared" :key="row.model.id" scope="col">
                <a href="#detail-title" @click.prevent="emit('open', row)">
                  {{ row.model.product_name || row.model.name }}<span v-if="row.model.product_name">{{ row.model.name }}</span>
                </a>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <th scope="row">适用国家或地区</th>
              <td v-for="row in compared" :key="row.model.id">{{ [...new Set(comparedRegions.get(row.model.id)?.map((entry) => entry.regionLabel))].join('、') }}</td>
            </tr>
            <tr>
              <th scope="row">产品线类型</th>
              <td v-for="row in compared" :key="row.model.id">{{ row.model.product_line.name || '未记录' }}</td>
            </tr>
            <tr>
              <th scope="row">量产节点（GA）</th>
              <td v-for="row in compared" :key="row.model.id">
                <ul class="record-statuses">
                  <li v-for="entry in comparedRegions.get(row.model.id)" :key="entry.id">
                    <span class="badge" :class="entry.status.tone">{{ entry.statusLabel }}</span>
                    <small v-if="entry.regionKey">{{ entry.regionLabel }}</small>
                  </li>
                </ul>
              </td>
            </tr>
            <tr>
              <th scope="row">当前阶段</th>
              <td v-for="row in compared" :key="row.model.id">{{ row.model.current_stage || '未记录' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
    <div class="panel catalog-panel">
      <div class="panel-head">
        <h2 id="results-title" tabindex="-1">搜索结果</h2>
        <span class="count" role="status">{{ hasFilters ? '匹配' : '共' }} {{ filtered.length }} 条记录</span>
      </div>
      <div v-if="!filtered.length" class="empty">
        <p>{{ rows.length ? '没有符合条件的产品' : '货架库暂无型号' }}</p>
        <button v-if="hasFilters" class="button" type="button" @click="emit('reset')">清除筛选</button>
      </div>
      <template v-else>
        <div id="results">
          <table class="catalog" aria-labelledby="catalog-title">
            <thead>
              <tr>
                <th scope="col" class="compare-col">对比</th><th scope="col">产品名称</th><th scope="col">产品型号</th>
                <th scope="col">适用国家或地区</th><th scope="col">量产节点（GA）</th><th scope="col">产品线类型</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in visible" :key="row.id">
                <td class="compare-col">
                  <label class="compare-check" :title="compareLabel(row)">
                    <input type="checkbox" :checked="comparedIds.has(row.model.id)" :aria-label="compareLabel(row)"
                      :disabled="compared.length >= compareLimit && !comparedIds.has(row.model.id)" @change="emit('toggle', row)">
                    <span aria-hidden="true"></span>
                  </label>
                </td>
                <td>{{ row.model.product_name || '未记录' }}</td>
                <td>
                  <a class="model" href="#detail-title" @click.prevent="emit('open', row)">
                    <span>{{ row.model.name }}</span><span class="arw" aria-hidden="true"></span>
                  </a>
                </td>
                <td><span class="area">{{ row.regionLabel }}</span></td>
                <td><span class="badge" :class="row.status.tone">{{ row.statusLabel }}</span></td>
                <td class="product-line">{{ row.model.product_line.name || '未记录' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="catalog-foot">
          <span class="range">显示第 {{ first + 1 }}–{{ first + visible.length }} 条</span>
          <nav class="page-tools" aria-label="产品清单分页">
            <button class="button" type="button" :disabled="page <= 1" @click="page--">上一页</button>
            <span class="page-count">{{ page }} / {{ pages }}</span>
            <button class="button" type="button" :disabled="page >= pages" @click="page++">下一页</button>
          </nav>
        </div>
      </template>
    </div>
  </section>
</template>
