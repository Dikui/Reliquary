<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import Catalog from './components/Catalog.vue'
import Market from './components/Market.vue'
import ShelfDetail from './components/ShelfDetail.vue'
import ShelfOverview from './components/ShelfOverview.vue'
import HeroMedia from './components/HeroMedia.vue'
import clouLogo from './assets/clou.png'
import { expandShelf, snapshotDate, type ShelfFilters, type ShelfRow, type ShelfSnapshot } from './data/shelf'

const navLinks = [
  { id: 'overview-title', label: '货架总览' },
  { id: 'catalog-title', label: '产品清单' },
  { id: 'market-title', label: '全球市场' },
]
const emptyFilters: ShelfFilters = { region: '', productLine: '', status: '', query: '' }
const filters = shallowRef({ ...emptyFilters })
const snapshot = shallowRef<ShelfSnapshot | null>(null)
const rows = computed(() => snapshot.value ? expandShelf(snapshot.value) : [])
const date = computed(() => snapshotDate(snapshot.value?.as_of ?? ''))
const loading = ref(true)
const error = ref('')
const detail = shallowRef<ShelfRow | null>(null)
const detailMarket = ref('')
const compared = shallowRef<ShelfRow[]>([])
const menuOpen = ref(false)
const scrolled = ref(false)
const motion = window.matchMedia('(prefers-reduced-motion: reduce)')
const paused = ref(false)
const reduced = ref(motion.matches)
const mediaReady = ref(false)
const hero = ref<HTMLElement | null>(null)
const bridge = ref<HTMLCanvasElement | null>(null)
const abort = new AbortController()

async function loadShelf() {
  loading.value = true
  error.value = ''
  try {
    const response = await fetch('/api/v1/showcase/shelf', { signal: abort.signal })
    if (!response.ok) {
      const body = await response.json().catch(() => null)
      error.value = body?.error === 'showcase_library_not_found'
        ? '未找到展示货架库。'
        : body?.error === 'showcase_library_ambiguous'
          ? '存在多个可展示的货架库，请联系管理员指定展示库。'
          : '暂时无法加载货架，请重试。'
      return
    }
    const data = await response.json() as ShelfSnapshot
    if (!Array.isArray(data.models) || !Array.isArray(data.regions) || !Array.isArray(data.product_lines)) {
      throw new Error('Invalid shelf snapshot')
    }
    snapshot.value = data
  } catch {
    if (!abort.signal.aborted) error.value = '暂时无法加载货架，请重试。'
  } finally {
    loading.value = false
  }
}

async function scrollToSection(id: string) {
  await nextTick()
  const target = document.getElementById(id) ?? document.getElementById('shelf-status')
  target?.scrollIntoView({ block: 'start' })
  target?.focus({ preventScroll: true })
}

function navigate(id: string) {
  detail.value = null
  menuOpen.value = false
  void scrollToSection(id)
}

function setFilters(changes: Partial<ShelfFilters>) {
  filters.value = { ...filters.value, ...changes }
}

function showCatalog(region: string, status: string) {
  setFilters({ ...emptyFilters, region, status })
  navigate('catalog-title')
}

async function openDetail(row: ShelfRow, market = '') {
  detail.value = row
  detailMarket.value = market
  menuOpen.value = false
  await nextTick()
  window.scrollTo({ top: 0 })
  document.getElementById('detail-title')?.focus({ preventScroll: true })
}

function toggleCompare(row: ShelfRow) {
  const selected = compared.value.some((entry) => entry.model.id === row.model.id)
  if (selected) {
    compared.value = compared.value.filter((entry) => entry.model.id !== row.model.id)
  } else if (compared.value.length < 3) {
    compared.value = [...compared.value, row]
  }
}

function trackScroll() {
  scrolled.value = window.scrollY > 20
}

function updateMotion(event: MediaQueryListEvent) {
  reduced.value = event.matches
}

onMounted(() => {
  window.addEventListener('scroll', trackScroll, { passive: true })
  motion.addEventListener('change', updateMotion)
  trackScroll()
  void loadShelf()
})
onBeforeUnmount(() => {
  window.removeEventListener('scroll', trackScroll)
  motion.removeEventListener('change', updateMotion)
  abort.abort()
})
</script>

<template>
  <div :class="detail ? 'is-detail' : 'shelf-page'">
    <a class="skip" href="#content" @click.prevent="scrollToSection('content')">跳至主要内容</a>
    <header class="topbar" :class="{ 'is-scrolled': scrolled }">
      <div class="nav">
        <a class="logo" href="#home" aria-label="CLOU 储能产品货架首页" @click.prevent="navigate('home')">
          <img :src="clouLogo" width="310" height="140" alt="">
        </a>
        <nav class="nav-links" aria-label="主要导航">
          <a v-for="link in navLinks" :key="link.id" :href="'#' + link.id" @click.prevent="navigate(link.id)">{{ link.label }}</a>
        </nav>
        <div class="nav-actions">
          <a class="btn btn-primary btn-nav" href="#query" @click.prevent="navigate('query')">查找产品<span class="arw" aria-hidden="true"></span></a>
        </div>
        <button class="burger" type="button" :aria-label="menuOpen ? '关闭菜单' : '打开菜单'" :aria-expanded="menuOpen"
          aria-controls="menu" @click="menuOpen = !menuOpen"><span aria-hidden="true"></span></button>
      </div>
      <nav id="menu" class="menu" :class="{ open: menuOpen }" aria-label="移动端导航">
        <a v-for="link in navLinks" :key="link.id" :href="'#' + link.id" @click.prevent="navigate(link.id)">{{ link.label }}</a>
        <a class="btn btn-primary" href="#query" @click.prevent="navigate('query')">查找产品<span class="arw" aria-hidden="true"></span></a>
      </nav>
    </header>

    <section id="home" ref="hero" class="hero" :hidden="Boolean(detail)" aria-labelledby="hero-title">
      <div class="bg media-ready" :class="{ 'video-ready': mediaReady }" aria-hidden="true">
        <HeroMedia :paused="paused || reduced" :hidden="Boolean(detail)" @ready="mediaReady = $event" />
      </div>
      <div class="hero-inner">
        <div class="hero-copy">
          <h1 id="hero-title" class="hero-title" tabindex="-1">
            <span class="ln hero-brand" aria-hidden="true"><span class="ln-i">CLOU</span></span>
            <span class="ln"><span class="ln-i">储能产品<br>速配全球市场</span></span>
          </h1>
          <p class="sub"><span>按市场或型号查找产品，</span><span>比较货架记录，查看量产节点、资料和认证情况。</span></p>
        </div>
        <div class="ctas">
          <a class="btn btn-lg btn-primary" href="#market-title" @click.prevent="navigate('market-title')">按市场找产品<span class="arw" aria-hidden="true"></span></a>
          <a class="btn btn-lg btn-ghost" href="#query" @click.prevent="navigate('query')">按型号查找<span class="arw" aria-hidden="true"></span></a>
          <a class="btn btn-lg btn-ghost" href="#overview-title" @click.prevent="navigate('overview-title')">查看货架总览<span class="arw" aria-hidden="true"></span></a>
        </div>
      </div>
      <div class="hero-foot"><a href="#market-title" @click.prevent="navigate('market-title')">向下浏览<span class="chevron" aria-hidden="true"></span></a></div>
      <button class="motion-toggle" type="button" :hidden="reduced || !mediaReady" @click="paused = !paused">{{ paused ? '播放动画' : '暂停动画' }}</button>
    </section>
    <canvas ref="bridge" class="globe-bridge" aria-hidden="true" hidden></canvas>

    <main id="content" class="shell" tabindex="-1">
      <div :hidden="Boolean(detail)" class="shelf-sections">
        <div v-if="loading || error || (snapshot && !rows.length)" id="shelf-status" class="shelf-status panel empty" tabindex="-1">
          <p v-if="loading" role="status">正在加载货架数据…</p>
          <template v-else-if="error">
            <p role="alert">{{ error }}</p>
            <button class="button" type="button" @click="loadShelf">重试</button>
          </template>
          <p v-else role="status">展示货架库暂无型号。</p>
        </div>
        <template v-if="snapshot">
          <Market :rows="rows" :regions="snapshot.regions" :as-of="snapshot.as_of" :date="date" :hero="hero" :bridge="bridge"
            @open="openDetail" @catalog="showCatalog" />
          <ShelfOverview :rows="rows" :regions="snapshot.regions" :filters="filters" :as-of="snapshot.as_of" :date="date" @filter="showCatalog" />
          <Catalog :snapshot="snapshot" :rows="rows" :filters="filters" :compared="compared"
            @filters="setFilters" @reset="setFilters(emptyFilters)" @open="openDetail" @toggle="toggleCompare" @clear="compared = []" />
        </template>
      </div>
      <ShelfDetail v-if="detail" :key="detail.id" :row="detail" :market="detailMarket" :compare-count="compared.length"
        :compared="compared.some((row) => row.model.id === detail?.model.id)"
        @back="navigate(detailMarket ? 'map-result-title' : 'catalog-title')" @toggle="toggleCompare(detail)" />
    </main>
    <footer class="site-foot shell">
      <span>BESS 储能产品中心</span>
      <span v-if="date">数据截至 <time :datetime="snapshot?.as_of">{{ date }}</time></span>
    </footer>
  </div>
</template>
