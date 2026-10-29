<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getFollowFeed } from '@/api/follows'
import { useFollowStore } from '@/stores/follows'
import SearchEntry from '@/components/SearchEntry.vue'
import type { FollowFeedEntry } from '@/types/follows'
import { isRFC3339After } from '@/utils/rfc3339'

const { t } = useI18n()
const route = useRoute(), router = useRouter(), store = useFollowStore()
const libraryId = ref('')
const libraryOptions = computed(() => [{ value: '', label: `${t('follows.allLibraries')} (${store.libraries.reduce((n, lib) => n + lib.new_count, 0)})` }, ...store.libraries.map(lib => ({ value: lib.library_id, label: `${lib.display_name || lib.library_name} (${lib.new_count})` }))])
const scope = () => libraryId.value ? [libraryId.value] : undefined
const targetIds = ref<string[]>([]), collapseSimilar = ref(true), limit = ref(20)
const results = ref<FollowFeedEntry[]>([]), total = ref(0), loading = ref(false), error = ref(''), viewError = ref(''), marking = ref(false)
const cursor = ref(''), nextCursor = ref(''), cursorStack = ref<string[]>([]), paginationAction = ref<'previous' | 'next' | ''>('')
const viewRetryAction = ref<'open' | 'all'>('open')
const options = computed(() => store.targets.map(target => ({
  value: target.target_id,
  label: t('follows.targetLabel', { name: target.name, type: t(`follows.targetTypes.${target.target_type}`), count: target.recent_entry_count }),
})))
const limitOptions = [20, 50, 100].map(value => ({ value, label: String(value) }))
let snapshotByLibrary: Record<string, string | null> = {}
let sequence = 0, disposed = false, mounted = false, opened = false, snapshotLastViewed: string | null = null, snapshotViewedAt: string | undefined
const countArticles = (articles: FollowFeedEntry[]): number => articles.reduce((count, article) => count + 1 + countArticles(article.similar_reports || []), 0)
const shownArticleCount = computed(() => countArticles(results.value))
function applyNewSnapshot(articles: FollowFeedEntry[]) { for (const article of articles) { const viewed = (article.library_id || "") in snapshotByLibrary ? snapshotByLibrary[article.library_id || ""]! : snapshotLastViewed; article.is_new = article.entered_at !== null && (viewed === null || isRFC3339After(article.entered_at, viewed)); applyNewSnapshot(article.similar_reports || []) } }
const filterKey = () => JSON.stringify({ libraryId: libraryId.value, targetIds: targetIds.value, collapseSimilar: collapseSimilar.value, limit: limit.value })
function readURL() {
  const nextLibrary = typeof route.query.library === 'string' ? route.query.library : ''
  if (nextLibrary !== libraryId.value) { opened = false; snapshotByLibrary = {} }
  libraryId.value = nextLibrary
  const ids = route.query.target_ids
  targetIds.value = [...new Set((Array.isArray(ids) ? ids : typeof ids === 'string' ? ids.split(',') : []).filter((id): id is string => Boolean(id)))]
  collapseSimilar.value = route.query.collapse_similar !== 'false'
  const value = Number(route.query.limit)
  limit.value = [20, 50, 100].includes(value) ? value : 20
}
function syncURL() {
  void router.replace({ query: { ...(libraryId.value ? { library: libraryId.value } : {}), ...(targetIds.value.length ? { target_ids: targetIds.value.join(',') } : {}), ...(!collapseSimilar.value ? { collapse_similar: 'false' } : {}), ...(limit.value !== 20 ? { limit: String(limit.value) } : {}) } })
}
function resetPagination() { cursor.value = ''; nextCursor.value = ''; cursorStack.value = [] }
function changeFilters() { resetPagination(); void load(true) }
function changeLibrary() { opened = false; snapshotByLibrary = {}; changeFilters() }
async function recordOpening() {
  if (!snapshotViewedAt || marking.value) return
  marking.value = true
  viewError.value = ''
  viewRetryAction.value = 'open'
  try { await store.markViewed(snapshotViewedAt, scope()) }
  catch { if (!disposed) viewError.value = t('follows.markFailed') }
  finally { if (!disposed) marking.value = false }
}
async function load(updateURL = false) {
  if (disposed) return false
  if (updateURL) syncURL()
  const current = ++sequence
  loading.value = true
  error.value = ''
  try {
    await store.loadTargets(true)
    await store.refreshState()
    if (current !== sequence || disposed) return false
    const response = await getFollowFeed({ include_seen: true, ...(scope() ? { libraries: scope() } : {}), ...(cursor.value ? { cursor: cursor.value } : {}), ...(targetIds.value.length ? { target_ids: [...targetIds.value] } : {}), limit: limit.value, collapse_similar: collapseSimilar.value })
    if (!response.success) throw new Error(response.message)
    if (current !== sequence || disposed) return false
    total.value = response.data.total || 0
    nextCursor.value = response.data.next_cursor || ''
    if (!opened) {
      opened = true
      snapshotByLibrary = Object.fromEntries((response.data.libraries || []).map(lib => [lib.library_id, lib.last_viewed_at]))
      snapshotLastViewed = response.data.last_viewed_at
      snapshotViewedAt = response.data.viewed_at
      results.value = response.data.results || []
      applyNewSnapshot(results.value)
      await recordOpening()
    } else { results.value = response.data.results || []; applyNewSnapshot(results.value) }
    return true
  } catch { if (current === sequence && !disposed) error.value = t('follows.feedFailed'); return false }
  finally { if (current === sequence && !disposed) loading.value = false }
}
async function nextPage() {
  if (loading.value || !nextCursor.value) return
  const previousCursor = cursor.value, previousStack = [...cursorStack.value], filters = filterKey(), requestedCursor = nextCursor.value
  cursorStack.value = [...cursorStack.value, cursor.value]
  cursor.value = requestedCursor
  paginationAction.value = 'next'
  if (!await load() && filterKey() === filters && cursor.value === requestedCursor) { cursor.value = previousCursor; cursorStack.value = previousStack }
  paginationAction.value = ''
}
async function previousPage() {
  if (loading.value || !cursorStack.value.length) return
  const previousCursor = cursor.value, previousStack = [...cursorStack.value], filters = filterKey(), requestedCursor = cursorStack.value.at(-1) || ''
  cursor.value = requestedCursor
  cursorStack.value = cursorStack.value.slice(0, -1)
  paginationAction.value = 'previous'
  if (!await load() && filterKey() === filters && cursor.value === requestedCursor) { cursor.value = previousCursor; cursorStack.value = previousStack }
  paginationAction.value = ''
}
function clearNew(articles: FollowFeedEntry[]) { for (const article of articles) { article.is_new = false; clearNew(article.similar_reports || []) } }
async function markAllViewed() {
  if (marking.value || loading.value) return
  marking.value = true
  viewError.value = ''
  viewRetryAction.value = 'all'
  try {
    const state = await store.markViewed(undefined, scope())
    if (disposed) return
    snapshotByLibrary = Object.fromEntries((state.libraries || []).map(lib => [lib.library_id, lib.last_viewed_at]))
    snapshotLastViewed = state.last_viewed_at
    snapshotViewedAt = state.last_viewed_at || undefined
    clearNew(results.value)
    await load()
  } catch { if (!disposed) viewError.value = t('follows.markFailed') }
  finally { if (!disposed) marking.value = false }
}
function openArticle(article: FollowFeedEntry) { void router.push({ path: '/platform/entries', query: { entry_id: article.entry_id } }) }
function openEntity(id: string) { void router.push({ path: '/platform/entries', query: { tab: 'entities', entity_id: id } }) }
function retryViewed() { return viewRetryAction.value === 'all' ? markAllViewed() : recordOpening() }
watch(() => route.query, () => {
  const previous = filterKey()
  readURL()
  if (mounted && previous !== filterKey()) { resetPagination(); void load() }
})
onMounted(() => { readURL(); mounted = true; void load() })
onUnmounted(() => { disposed = true; sequence++ })
</script>

<template>
  <main class="follow-feed-page">
    <header class="feed-heading"><div><h1>{{ t('follows.title') }}</h1><p>{{ t('follows.description') }}</p></div><t-button variant="outline" :loading="marking" :disabled="loading || marking" @click="markAllViewed">{{ t('follows.markAllViewed') }}</t-button></header>
    <form class="feed-toolbar" @submit.prevent="changeFilters">
      <label class="feed-filter-field"><span>{{ t('follows.library') }}</span><t-select v-model="libraryId" :options="libraryOptions" :disabled="loading || marking" :aria-label="t('follows.library')" @change="changeLibrary" /></label>
      <label class="feed-filter-field"><span>{{ t('follows.filterTarget') }}</span><t-select v-model="targetIds" :options="options" multiple clearable filterable :loading="store.targetsLoading" :disabled="loading || marking" :placeholder="t('follows.allTargets')" :aria-label="t('follows.filterTarget')" @change="changeFilters" /></label>
      <label class="feed-filter-field feed-limit"><span>{{ t('follows.limit') }}</span><t-select v-model="limit" :options="limitOptions" :disabled="loading || marking" :aria-label="t('follows.limit')" @change="changeFilters" /></label>
      <label class="feed-filter-field fold-control"><span>{{ t('knowledgeSearch.collapseSimilar') }}</span><t-switch v-model="collapseSimilar" :disabled="loading || marking" :aria-label="t('knowledgeSearch.collapseSimilar')" @change="changeFilters" /></label>
      <t-button class="feed-refresh" variant="outline" :loading="loading" :disabled="marking" @click="load()">{{ t('follows.refresh') }}</t-button>
    </form>
    <t-alert v-if="viewError" theme="error" :message="viewError" role="alert"><template #operation><t-button variant="text" :loading="marking" @click="retryViewed">{{ t('common.retry') }}</t-button></template></t-alert>
    <section class="feed-results" :aria-label="t('follows.results')" :aria-busy="loading">
      <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button variant="text" @click="load()">{{ t('common.retry') }}</t-button></template></t-alert>
      <t-loading v-else-if="loading" :text="t('follows.loading')" />
      <template v-else-if="results.length">
        <p class="feed-count" role="status">{{ t('follows.resultCount', { count: results.length }) }}</p>
        <p v-if="total > shownArticleCount" class="feed-count" role="status">{{ t('follows.limitHint', { shown: shownArticleCount, total }) }}</p>
        <SearchEntry v-for="article in results" :key="article.entry_id" :article="article" query="" follow-feed @open-article="openArticle($event as FollowFeedEntry)" @open-entity="openEntity" />
      </template>
      <div v-else class="feed-empty"><t-empty :description="t(store.targets.length ? 'follows.emptyFeed' : 'follows.emptyTargets')" /><div v-if="!store.targets.length" class="empty-actions"><router-link to="/platform/entries?tab=entities">{{ t('knowledgeWorkbench.title') }}</router-link><router-link to="/platform/topics">{{ t('topicManagement.title') }}</router-link></div></div>
      <nav v-if="results.length || cursorStack.length" class="feed-pagination" :aria-label="t('follows.pagination')" :aria-busy="loading">
        <t-button variant="outline" :loading="loading && paginationAction === 'previous'" :disabled="loading || !cursorStack.length" @click="previousPage">{{ t('follows.previousPage') }}</t-button>
        <span>{{ t('follows.pageNumber', { page: cursorStack.length + 1 }) }}</span>
        <t-button variant="outline" :loading="loading && paginationAction === 'next'" :disabled="loading || !nextCursor" @click="nextPage">{{ t('follows.nextPage') }}</t-button>
      </nav>
    </section>
  </main>
</template>

<style scoped lang="less">
.follow-feed-page { width: 100%; height: 100%; overflow-y: auto; box-sizing: border-box; padding: 32px; color: var(--td-text-color-primary); }
.feed-heading, .feed-toolbar, .feed-results { max-width: 1000px; margin-inline: auto; }
.feed-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.feed-heading h1 { margin: 0 0 8px; font-size: 22px; }
.feed-heading p { margin: 0 0 24px; line-height: 1.6; color: var(--td-text-color-secondary); }
.feed-toolbar { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: end; gap: 14px 16px; padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); }
.feed-filter-field { display: flex; flex-direction: column; gap: 6px; min-width: 0; font-size: 13px; color: var(--td-text-color-secondary); overflow-wrap: anywhere; }
.feed-filter-field :deep(.t-select__wrap) { width: 100%; min-width: 0; }
.fold-control { align-items: flex-start; }
.feed-refresh { justify-self: start; }
.feed-results { display: grid; gap: 16px; padding-block: 24px; min-height: 160px; }
.feed-count { margin: 0; color: var(--td-text-color-secondary); font-size: 13px; }
.feed-pagination { display: flex; align-items: center; justify-content: center; gap: 12px; color: var(--td-text-color-secondary); font-size: 13px; }
.empty-actions { display: flex; justify-content: center; gap: 24px; margin-top: 16px; }
.empty-actions a { color: var(--td-brand-color); }
.empty-actions a:hover { text-decoration: underline; }
.empty-actions a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
@media (max-width: 640px) { .follow-feed-page { padding: 16px; } .feed-heading { flex-direction: column; margin-bottom: 16px; } .feed-heading p { margin-bottom: 0; } }
@media (max-width: 560px) { .feed-toolbar { grid-template-columns: minmax(0, 1fr); } }
</style>
