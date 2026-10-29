<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { Button, EmptyState, ErrorState, Select, Tabs, Tag } from '@/components/ui'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'
import { listEntityMergeSuggestions, type EntityMergeSuggestion } from '@/api/entities'
import { listPendingEntry, type ReviewEntry } from '@/api/entry-review'
import { usePendingCountsStore } from '@/stores/pendingCounts'
import MergeSuggestions from './components/MergeSuggestions.vue'
import PendingReviews from './components/PendingReviews.vue'

const { t, n } = useI18n()
const route = useRoute(), router = useRouter(), counts = usePendingCountsStore()
const libraries = ref<LibraryCatalogItem[]>([]), catalogLoading = ref(true), catalogError = ref('')
const merges = ref<EntityMergeSuggestion[]>([]), reviews = ref<ReviewEntry[]>([])
const mergeLoading = ref(false), reviewLoading = ref(false), mergeError = ref(''), reviewError = ref('')
const reviewTotal = ref<number | null>(null), reviewPage = ref(1), busy = ref(false)
const scope = computed(() => String(route.query.libraries || '').split(',').filter(Boolean))
const activeTab = ref(route.query.tab === 'reviews' ? 'reviews' : 'merges')
const pendingMergeCount = computed(() => mergeError.value || mergeLoading.value ? null : merges.value.filter(item => item.status === 'pending').length)
const tabs = computed(() => [
  { value: 'merges', label: `${t('mergeSuggestions.title')}${pendingMergeCount.value === null ? '' : ` ${n(pendingMergeCount.value)}`}`, disabled: busy.value },
  { value: 'reviews', label: `${t('review.title')}${reviewTotal.value === null ? '' : ` ${n(reviewTotal.value)}`}`, disabled: busy.value },
])
const onlyReviews = computed(() => !mergeLoading.value && !mergeError.value && merges.value.length === 0 && reviewTotal.value !== null && reviewTotal.value > 0)
const showTabs = computed(() => !onlyReviews.value && (reviewTotal.value === null || reviewTotal.value > 0))
const empty = computed(() => !catalogLoading.value && !catalogError.value && !mergeLoading.value && !reviewLoading.value && !mergeError.value && !reviewError.value && merges.value.length === 0 && reviewTotal.value === 0)
let catalogGeneration = 0, mergeGeneration = 0, reviewGeneration = 0

function setTab(value: string) {
  if (busy.value) return
  activeTab.value = value
  void router.replace({ query: { ...route.query, tab: value }, hash: route.hash })
}
function setScope(ids: string[]) {
  if (busy.value) return
  const query = { ...route.query }
  if (ids.length) query.libraries = ids.join(',')
  else delete query.libraries
  void router.replace({ query, hash: route.hash })
}
function addScope(event: Event) {
  const select = event.target as HTMLSelectElement
  setScope(select.value ? [...scope.value, select.value] : [])
  select.value = ''
}
async function loadMerges() {
  const generation = ++mergeGeneration
  mergeLoading.value = true; mergeError.value = ''
  try {
    const ids = scope.value.length ? [...scope.value] : libraries.value.map(library => library.id)
    const pages = await Promise.all(ids.map(async libraryId => {
      const items: EntityMergeSuggestion[] = []
      for (let offset = 0; generation === mergeGeneration; offset += 100) {
        const response = await listEntityMergeSuggestions({ library_id: libraryId, status: 'all', limit: 100, offset })
        if (!response.success || !Array.isArray(response.data)) throw new Error(t('pendingPage.loadFailed'))
        items.push(...response.data)
        if (response.data.length < 100) break
      }
      return items
    }))
    if (generation !== mergeGeneration) return
    merges.value = [...new Map(pages.flat().map(item => [item.id, item])).values()]
      .sort((a, b) => b.created_at.localeCompare(a.created_at) || a.id.localeCompare(b.id))
  } catch { if (generation === mergeGeneration) mergeError.value = t('pendingPage.loadFailed') }
  finally { if (generation === mergeGeneration) mergeLoading.value = false }
}
async function loadReviews() {
  const generation = ++reviewGeneration
  reviewLoading.value = true; reviewError.value = ''; reviewTotal.value = null
  try {
    const response = await listPendingEntry({ libraries: [...scope.value], kind: 'intake', page: reviewPage.value, page_size: 20 })
    if (!response.success || !Array.isArray(response.data) || !Number.isSafeInteger(response.total) || response.total < 0) throw new Error(t('pendingPage.loadFailed'))
    if (generation !== reviewGeneration) return
    reviews.value = response.data; reviewTotal.value = response.total
    if (!response.data.length && reviewPage.value > 1) {
      reviewPage.value = Math.max(1, Math.ceil(response.total / 20))
      await loadReviews()
    }
  } catch { if (generation === reviewGeneration) reviewError.value = t('review.loadFailed') }
  finally { if (generation === reviewGeneration) reviewLoading.value = false }
}
async function loadCatalog() {
  const generation = ++catalogGeneration
  catalogLoading.value = true; catalogError.value = ''
  try {
    const response = await listLibraries()
    if (!response.success || !Array.isArray(response.data)) throw new Error(t('pendingPage.loadFailed'))
    if (generation !== catalogGeneration) return
    libraries.value = response.data
    catalogLoading.value = false
    await Promise.all([loadMerges(), loadReviews()])
  } catch { if (generation === catalogGeneration) catalogError.value = t('pendingPage.loadFailed') }
  finally { if (generation === catalogGeneration) catalogLoading.value = false }
}
async function refresh() { if (!busy.value) await Promise.all([loadMerges(), loadReviews(), counts.refresh()]) }
function changePage(page: number) { reviewPage.value = page; void loadReviews() }
async function mergeChanged(id: string, status: 'accepted' | 'rejected') {
  merges.value = merges.value.map(item => item.id === id ? { ...item, status } : item)
  await counts.refresh()
}
async function reviewChanged() { await Promise.all([loadReviews(), counts.refresh()]) }
watch(() => route.query.tab, value => { activeTab.value = value === 'reviews' ? 'reviews' : 'merges' })
watch(() => scope.value.join(','), () => {
  mergeGeneration++; reviewGeneration++; merges.value = []; reviews.value = []; reviewPage.value = 1
  if (!catalogLoading.value) void Promise.all([loadMerges(), loadReviews()])
})
watch(reviewTotal, total => { if (total === 0) activeTab.value = 'merges' })
onMounted(loadCatalog)
onBeforeUnmount(() => { catalogGeneration++; mergeGeneration++; reviewGeneration++ })
</script>

<template>
  <main class="pending-page">
    <div class="pending-wrap">
      <header class="pending-heading"><h1>{{ t('menu.pending') }}</h1><Button :disabled="busy || catalogLoading" :loading="mergeLoading || reviewLoading" @click="refresh">{{ t('common.refresh') }}</Button></header>
      <div class="pending-scope">
        <Select model-value="" :disabled="busy || catalogLoading" :aria-label="t('ikb18.scope')" @change="addScope"><option value="">{{ scope.length ? t('pendingPage.addLibrary') : t('ikb18.all') }}</option><option v-for="library in libraries.filter(item => !scope.includes(item.id))" :key="library.id" :value="library.id">{{ library.display_name || library.name }}</option></Select>
        <Tag v-for="id in scope" :key="id"><span :title="id">{{ libraries.find(item => item.id === id)?.display_name || libraries.find(item => item.id === id)?.name || id.slice(0, 8) }}</span><button type="button" :disabled="busy" :aria-label="t('pendingPage.removeLibrary', { name: libraries.find(item => item.id === id)?.name || id.slice(0, 8) })" @click="setScope(scope.filter(item => item !== id))">×</button></Tag>
        <Button v-if="scope.length" variant="ghost" :disabled="busy" @click="setScope([])">{{ t('ikb18.all') }}</Button>
      </div>
      <ErrorState v-if="catalogError" :title="catalogError"><Button @click="loadCatalog">{{ t('common.retry') }}</Button></ErrorState>
      <p v-else-if="catalogLoading" role="status">{{ t('common.loading') }}</p>
      <EmptyState v-else-if="empty" :title="t('pendingPage.empty')" :description="t('pendingPage.emptyHint')" />
      <template v-else>
        <Tabs v-if="showTabs" :model-value="activeTab" :items="tabs" :label="t('menu.pending')" @update:model-value="setTab">
          <template #merges><MergeSuggestions :items="merges" :libraries="libraries" :loading="mergeLoading" :error="mergeError" @refresh="loadMerges" @changed="mergeChanged" @busy-change="busy = $event" /></template>
          <template #reviews><PendingReviews :records="reviews" :libraries="libraries" :total="reviewTotal" :page="reviewPage" :loading="reviewLoading" :error="reviewError" @refresh="loadReviews" @page="changePage" @changed="reviewChanged" @busy-change="busy = $event" /></template>
        </Tabs>
        <PendingReviews v-else-if="onlyReviews" :records="reviews" :libraries="libraries" :total="reviewTotal" :page="reviewPage" :loading="reviewLoading" :error="reviewError" @refresh="loadReviews" @page="changePage" @changed="reviewChanged" @busy-change="busy = $event" />
        <MergeSuggestions v-else :items="merges" :libraries="libraries" :loading="mergeLoading" :error="mergeError" @refresh="loadMerges" @changed="mergeChanged" @busy-change="busy = $event" />
      </template>
    </div>
  </main>
</template>

<style scoped>
.pending-page { flex: 1; min-width: 0; min-height: 0; overflow: auto; background: var(--bg); color: var(--text); }
.pending-wrap { max-width: 1152px; padding: var(--space-5) var(--space-6) var(--space-7); display: flex; flex-direction: column; gap: var(--space-4); }
.pending-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: var(--space-3); }
.pending-heading h1 { margin: 0; color: var(--text); font: var(--type-page-weight) var(--type-page-size)/1 var(--font); letter-spacing: -0.045em; }
.pending-scope { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
.pending-scope .rq-select { width: auto; max-width: 100%; min-height: 32px; }
.pending-scope button { color: inherit; }
.pending-scope .rq-tag button { border: 0; background: transparent; cursor: pointer; }
.pending-wrap :deep(.rq-empty) { padding: var(--space-5) 0; }
.pending-wrap :deep(.rq-tabs__panel) { padding-top: var(--space-4); }
@media (max-width: 760px) { .pending-wrap { padding: var(--space-4); } }
</style>
