<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { globeCountries, mountAtlas } from '../atlas'
import { countryStandard, marketShortcuts } from '../config/countryRegions'
import { findCountryRegion, findCountryRegions, searchKey, type Region, type ShelfRow } from '../data/shelf'

const props = defineProps<{
  rows: ShelfRow[]
  regions: Region[]
  asOf: string
  date: string
  hero: HTMLElement | null
  bridge: HTMLCanvasElement | null
}>()

const emit = defineEmits<{
  open: [row: ShelfRow, market: string]
  catalog: [region: string, status: string]
}>()

const countriesByCode = new Map(globeCountries.map((country) => [country.code, country]))
const shortcutCodes = new Set(marketShortcuts.map(({ code }) => code))
const countryOptions = [
  ...marketShortcuts.map(({ code }) => countriesByCode.get(code)!),
  ...globeCountries.filter(({ code }) => !shortcutCodes.has(code))
    .sort((a, b) => a.label.localeCompare(b.label, 'zh-CN')),
]

const canvas = ref<HTMLCanvasElement | null>(null)
const products = ref<HTMLUListElement | null>(null)
const selected = ref('')
const production = ref(false)
const pickerOpen = ref(false)
const term = ref('')
const active = ref(-1)
let atlas: ReturnType<typeof mountAtlas>

const country = computed(() => countriesByCode.get(selected.value))
const countryRegions = computed(() => country.value ? findCountryRegions(props.regions, selected.value) : [])
const region = computed(() => countryRegions.value[0])
const regionNames = computed(() => countryRegions.value.map(({ name }) => name).join(' / '))
const regionalRows = computed(() => {
  const keys = new Set(countryRegions.value.map(({ key }) => key))
  return props.rows.filter((row) => keys.has(row.regionKey))
})
const produced = computed(() => regionalRows.value.filter((row) => row.status.key === 'GA'))
const visible = computed(() => production.value ? produced.value : regionalRows.value)
const standard = computed(() => countryStandard(selected.value))
const options = computed(() => {
  const key = searchKey(term.value)
  if (!key) {
    return country.value ? [{ code: '', label: '清除选择' }, ...countryOptions] : countryOptions
  }
  return countryOptions.filter((item) => [item.label, item.name, item.code].some((value) => searchKey(value).includes(key)))
})
const countryInput = computed({
  get: () => pickerOpen.value ? term.value : country.value?.label ?? '',
  set: (value: string) => {
    term.value = value
    active.value = -1
    pickerOpen.value = true
  },
})

const shortcuts = computed(() => marketShortcuts.map((shortcut) => {
  const match = findCountryRegion(props.regions, shortcut.code)
  return { ...shortcut, count: match ? props.rows.filter((row) => row.regionKey === match.key).length : 0 }
}))

function closePicker() {
  pickerOpen.value = false
  active.value = -1
}

function choose(code: string, center = true) {
  selected.value = code
  production.value = false
  term.value = ''
  closePicker()
  if (products.value) products.value.scrollTop = 0
  atlas?.choose(code, center)
}

function openPicker() {
  term.value = ''
  active.value = -1
  pickerOpen.value = true
}

function handleKeys(event: KeyboardEvent) {
  if (event.isComposing) return
  if (event.key === 'Escape' && pickerOpen.value) {
    event.preventDefault()
    closePicker()
  } else if (event.key === 'Enter' && pickerOpen.value && options.value.length) {
    event.preventDefault()
    choose(options.value[Math.max(active.value, 0)].code)
  } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    if (!pickerOpen.value) openPicker()
    const next = active.value + (event.key === 'ArrowDown' ? 1 : -1)
    active.value = options.value.length ? Math.max(0, Math.min(options.value.length - 1, next)) : -1
    nextTick(() => document.getElementById('map-option-' + active.value)?.scrollIntoView({ block: 'nearest' }))
  }
}

onMounted(() => {
  if (canvas.value && props.hero && props.bridge) {
    atlas = mountAtlas(canvas.value, (code) => choose(code, false), props.hero, props.bridge)
  }
})
onBeforeUnmount(() => atlas?.dispose())
</script>

<template>
  <section class="market-section" aria-labelledby="market-title">
    <div class="market-heading section-head">
      <div><h2 id="market-title" tabindex="-1">全球市场</h2><p>搜索国家或地区，也可直接点选地图。</p></div>
      <div v-if="date" class="snapshot">数据截至 <time :datetime="asOf">{{ date }}</time></div>
    </div>
    <div class="market-layout">
      <div class="panel market-atlas">
        <div class="market-stage">
          <canvas ref="canvas" tabindex="0" aria-label="可交互的 3D 地球" aria-describedby="globe-help">可通过右侧国家或地区选择框查看产品。</canvas>
          <div class="globe-hit" aria-hidden="true"></div>
          <div id="globe-label" class="globe-label" aria-hidden="true" hidden></div>
          <div class="globe-tools" role="group" aria-label="地球视图控制">
            <button type="button" data-globe-action="in" aria-label="放大地球" title="放大">+</button>
            <button type="button" data-globe-action="out" aria-label="缩小地球" title="缩小" disabled>−</button>
            <button type="button" data-globe-action="reset" aria-label="恢复初始视角" title="恢复初始视角">↺</button>
          </div>
        </div>
        <p id="globe-help" class="globe-help">
          <span class="globe-desktop-hint">拖动旋转 · 点选国家 · 触控板捏合或用 + / − 缩放 · 滚轮滚动页面</span>
          <span class="globe-touch-hint">在球面滑动旋转 · 双指捏合缩放 · 轻触选择国家</span>
        </p>
      </div>
      <section id="map-results" class="panel market-results" aria-labelledby="map-result-title">
        <div class="market-country">
          <label for="map-country">国家或地区</label>
          <div class="box search-box">
            <input id="map-country" v-model="countryInput" type="text" role="combobox" aria-autocomplete="list"
              :aria-expanded="pickerOpen" :aria-activedescendant="pickerOpen && options[active] ? 'map-option-' + active : undefined"
              aria-controls="map-country-options" autocomplete="off"
              spellcheck="false" enterkeyhint="search" placeholder="搜索国家或地区"
              @focus="openPicker" @click="!pickerOpen && openPicker()" @blur="closePicker" @keydown="handleKeys">
            <span class="market-country-arrow" aria-hidden="true"></span>
            <div v-show="pickerOpen" class="market-country-popup">
              <div id="map-country-options" class="market-country-options" role="listbox" aria-label="国家或地区">
                <button v-for="(option, index) in options" :id="'map-option-' + index" :key="option.code"
                  class="market-country-option" type="button" role="option" tabindex="-1" :aria-selected="index === active"
                  @pointerdown.prevent @click="choose(option.code)">{{ option.label }}</button>
              </div>
              <p v-if="!options.length" class="market-country-empty">未找到匹配的国家或地区</p>
            </div>
          </div>
        </div>
        <div class="market-result-head">
          <div class="market-result-title">
            <h3 id="map-result-title" tabindex="-1">{{ country?.label ?? '选择目标市场' }}</h3>
            <span v-if="country" class="market-standard">{{ standard ? standard + ' 标准' : '区域货架' }}</span>
          </div>
          <p class="market-selection" role="status">
            <template v-if="!country">选择国家或地区，查看适用货架</template>
            <template v-else-if="!regionalRows.length">暂无该地区货架</template>
            <template v-else>{{ regionNames }} · 共 {{ regionalRows.length }} 条记录</template>
          </p>
          <div v-if="regionalRows.length" class="market-stat">
            <div><strong>{{ regionalRows.length }}</strong><span>条记录</span></div>
            <div><strong>{{ produced.length }}</strong><span>已量产</span></div>
          </div>
        </div>
        <div v-if="!country" class="market-choices">
          <button v-for="shortcut in shortcuts" :key="shortcut.code" class="market-choice" type="button" @click="choose(shortcut.code)">
            <span class="market-choice-label"><span>{{ shortcut.label }}</span><small>{{ shortcut.standard }}</small></span>
            <span class="market-choice-end"><small>{{ shortcut.count }} 条</small><span class="arw" aria-hidden="true"></span></span>
          </button>
        </div>
        <div v-if="regionalRows.length" class="market-scope market-chips" role="group" aria-label="筛选量产状态">
          <button class="button" type="button" :aria-pressed="!production" @click="production = false">全部记录</button>
          <button class="button" type="button" :aria-pressed="production" @click="production = true">仅已量产</button>
        </div>
        <ul v-if="visible.length" ref="products" class="market-products" aria-label="适用产品">
          <li v-for="row in visible" :key="row.id">
            <a class="market-product" href="#detail-title" @click.prevent="emit('open', row, country?.label ?? '')">
              <span class="market-product-body"><strong>{{ row.model.name }}</strong><small>{{ row.model.product_name }}<template v-if="countryRegions.length > 1"> · {{ row.regionLabel }}</template></small></span>
              <span class="badge" :class="row.status.tone">{{ row.statusLabel }}</span>
            </a>
          </li>
        </ul>
        <p v-if="production && !visible.length" class="market-empty" role="status">这个市场暂无已量产记录。切换至「全部记录」可查看其他状态。</p>
        <div v-if="region && visible.length" class="market-catalog">
          <a class="back" href="#catalog-title" @click.prevent="emit('catalog', region.key, production ? 'GA' : '')">
            在产品清单中查看<span class="arw" aria-hidden="true"></span>
          </a>
        </div>
      </section>
    </div>
  </section>
</template>
