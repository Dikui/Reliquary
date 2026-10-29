<script setup lang="ts">
import { entryDetailLocation } from '@/utils/bodyReference'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getFollowFeed } from '@/api/follows'
import { useFollowStore } from '@/stores/follows'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'
import { Button, Chip, EmptyState, ErrorState, Input, Select, Switch, Tag } from '@/components/ui'
import FollowFeedItem from './components/FollowFeedItem.vue'
import AddFollowDialog from './components/AddFollowDialog.vue'
import type { FollowFeedEntry } from '@/types/follows'
import type { SourceLocation } from '@/types/search'
import { compareRFC3339, isRFC3339After } from '@/utils/rfc3339'

const { t, locale } = useI18n()
const libraries = ref<LibraryCatalogItem[]>([])
const libraryError = ref('')
async function loadLibraries() {
  libraryError.value = ''
  try {
    const response = await listLibraries()
    if (!response.success) throw new Error(response.message)
    if (!disposed) libraries.value = response.data || []
  } catch { if (!disposed) libraryError.value = t('follows.librariesFailed') }
}
const route = useRoute(), router = useRouter(), store = useFollowStore()
const libraryId = ref('')
const libraryOptions = computed(() => [{ value: '', label: `${t('follows.allLibraries')} (${store.newCount})` }, ...store.libraries.map(lib => ({ value: lib.library_id, label: `${lib.display_name || lib.library_name} (${lib.new_count})` }))])
const scope = () => libraryId.value ? [libraryId.value] : undefined
const targetSearch = ref('')
const targetSearchInput = ref<InstanceType<typeof Input>>()
function clearTargetSearch() { targetSearch.value = ''; targetSearchInput.value?.focus() }
const unreadOnly = ref(false)
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
// Preserve the opening snapshot while the server advances the library checkpoint.
// A seen representative must not hide an unread folded report.
function unreadArticles(articles: FollowFeedEntry[]): FollowFeedEntry[] {
  return articles.flatMap(article => {
    const similar = unreadArticles(article.similar_reports || [])
    return article.is_new ? [{ ...article, similar_reports: similar }] : similar
  })
}
const visibleArticles = computed(() => unreadOnly.value ? unreadArticles(results.value) : results.value)
const unreadCount = computed(() => countArticles(unreadArticles(results.value)))
const groups = computed(() => {
  const grouped = new Map<string, { date: string; label: string; articles: FollowFeedEntry[] }>()
  const sorted = [...visibleArticles.value].sort((left, right) => compareRFC3339(right.entered_at || right.collected_at, left.entered_at || left.collected_at) ?? 0)
  for (const article of sorted) {
    const timestamp = article.entered_at || article.collected_at
    const date = new Date(timestamp)
    const key = `${date.getFullYear()}-${date.getMonth() + 1}-${date.getDate()}`
    if (!grouped.has(key)) grouped.set(key, { date: key, label: date.toLocaleDateString(locale.value, { year: 'numeric', month: 'long', day: 'numeric' }), articles: [] })
    grouped.get(key)!.articles.push(article)
  }
  return [...grouped.values()]
})
const filteredOptions = computed(() => options.value.filter(option => !targetIds.value.includes(option.value) && option.label.toLocaleLowerCase().includes(targetSearch.value.toLocaleLowerCase())))
const selectedTargets = computed(() => targetIds.value.map(id => ({
  id,
  name: store.targets.find(target => target.target_id === id)?.name || id.slice(0, 8),
})))
function selectTarget(id?: string) { targetIds.value = id ? [id] : []; changeFilters() }
function addTarget(event: Event) {
  const select = event.target as HTMLSelectElement
  const id = select.value
  select.value = ''
  if (loading.value || marking.value || !id || targetIds.value.includes(id) || !options.value.some(option => option.value === id)) return
  targetIds.value = [...targetIds.value, id]
  changeFilters()
}
function removeTarget(id: string) {
  if (loading.value || marking.value) return
  targetIds.value = targetIds.value.filter(value => value !== id)
  changeFilters()
}
const shownArticleCount = computed(() => countArticles(results.value))
function applyNewSnapshot(articles: FollowFeedEntry[]) { for (const article of articles) { const viewed = (article.library_id || "") in snapshotByLibrary ? snapshotByLibrary[article.library_id || ""]! : snapshotLastViewed; article.is_new = article.entered_at !== null && (viewed === null || isRFC3339After(article.entered_at, viewed)); applyNewSnapshot(article.similar_reports || []) } }
const filterKey = () => JSON.stringify({ libraryId: libraryId.value, targetIds: targetIds.value, collapseSimilar: collapseSimilar.value, limit: limit.value, unreadOnly: unreadOnly.value })
function readURL() {
  const nextLibrary = typeof route.query.library === 'string' ? route.query.library : ''
  if (nextLibrary !== libraryId.value) { opened = false; snapshotByLibrary = {} }
  libraryId.value = nextLibrary
  const ids = route.query.target_ids
  targetIds.value = [...new Set((Array.isArray(ids) ? ids : typeof ids === 'string' ? ids.split(',') : []).filter((id): id is string => Boolean(id)))]
  unreadOnly.value = route.query.read === 'unread'
  collapseSimilar.value = route.query.collapse_similar !== 'false'
  const value = Number(route.query.limit)
  limit.value = [20, 50, 100].includes(value) ? value : 20
}
function syncURL() {
  void router.replace({ query: { ...(libraryId.value ? { library: libraryId.value } : {}), ...(targetIds.value.length ? { target_ids: targetIds.value.join(',') } : {}), ...(!collapseSimilar.value ? { collapse_similar: 'false' } : {}), ...(limit.value !== 20 ? { limit: String(limit.value) } : {}), ...(unreadOnly.value ? { read: 'unread' } : {}) } })
}
function filterRead(value: boolean) { unreadOnly.value = value; syncURL() }
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
function openArticle(article: FollowFeedEntry, location?: SourceLocation) { void router.push(entryDetailLocation(article.entry_id, location)) }
function retryViewed() { return viewRetryAction.value === 'all' ? markAllViewed() : recordOpening() }
watch(() => route.query, () => {
  const previous = filterKey()
  readURL()
  if (mounted && previous !== filterKey()) { resetPagination(); void load() }
})
onMounted(() => { readURL(); mounted = true; void load(); void loadLibraries() })
onUnmounted(() => { disposed = true; sequence++ })
</script>

<template>
  <main class="follow-feed-page rq-ui">
    <section class="feed-main">
      <header class="feed-heading">
        <h1>{{ t('follows.title') }}</h1>
        <div class="feed-heading-actions">
          <div class="feed-read-filters" :aria-label="t('follows.readFilter')"><Chip :model-value="unreadOnly" :title="t('follows.pageUnread')" data-feed-filter="unread" @update:model-value="filterRead(true)">{{ t('follows.unread') }} <span class="feed-number">{{ unreadCount }}</span></Chip><Chip :model-value="!unreadOnly" data-feed-filter="all" @update:model-value="filterRead(false)">{{ t('follows.all') }}</Chip></div>
          <Button :loading="marking" :disabled="loading || marking" @click="markAllViewed">{{ t('follows.markAllViewed') }}</Button>
        </div>
      </header>
      <form novalidate class="feed-toolbar" @submit.prevent="changeFilters">
        <label class="feed-library-field"><span>{{ t('follows.library') }}</span><Select v-model="libraryId" :disabled="loading || marking" :aria-label="t('follows.library')" @update:model-value="changeLibrary"><option v-for="option in libraryOptions" :key="option.value" :value="option.value">{{ option.label }}</option></Select></label>
        <details class="feed-more-filters">
          <summary>{{ t('ikb18.moreFilters') }}</summary>
          <div class="feed-advanced-filters">
            <div v-if="store.targets.length" class="feed-filter-field">
              <label for="follow-target-search">{{ t('follows.searchTargets') }}</label>
              <div class="feed-target-search"><Input id="follow-target-search" ref="targetSearchInput" v-model="targetSearch" :disabled="loading || marking" :aria-label="t('follows.searchTargets')" :placeholder="t('follows.searchTargets')" /><Button v-if="targetSearch" variant="ghost" :disabled="loading || marking" :aria-label="t('common.clear')" @click="clearTargetSearch">×</Button></div>
              <label for="follow-target-picker">{{ t('follows.filterTarget') }}</label>
              <select id="follow-target-picker" class="rq-control rq-select feed-target-select" :disabled="loading || marking || !filteredOptions.length" :aria-label="t('follows.filterTarget')" @change="addTarget"><option value="">{{ t('follows.filterTarget') }}</option><option v-for="option in filteredOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select>
            </div>
            <label class="feed-filter-field feed-limit"><span>{{ t('follows.limit') }}</span><Select :model-value="limit" :disabled="loading || marking" :aria-label="t('follows.limit')" @update:model-value="limit = Number($event); changeFilters()"><option v-for="option in limitOptions" :key="option.value" :value="option.value">{{ option.label }}</option></Select></label>
            <label class="feed-filter-field fold-control"><span>{{ t('search.collapseSimilar') }}</span><Switch v-model="collapseSimilar" :disabled="loading || marking" :aria-label="t('search.collapseSimilar')" @update:model-value="changeFilters" /></label>
          </div>
        </details>
        <Button class="feed-refresh" :loading="loading" :disabled="marking" @click="load()">{{ t('follows.refresh') }}</Button>
      </form>
      <div v-if="selectedTargets.length" class="feed-selected-targets" role="group" :aria-label="t('follows.filterTarget')">
        <Tag v-for="target in selectedTargets" :key="target.id" class="feed-selected-target" :data-target-id="target.id"><span :title="target.id">{{ target.name }}</span><button type="button" :disabled="loading || marking" :aria-label="t('searchPage.removeFilter', { name: target.name })" @click="removeTarget(target.id)">×</button></Tag>
      </div>
      <ErrorState v-if="viewError" :title="viewError"><Button :loading="marking" @click="retryViewed">{{ t('common.retry') }}</Button></ErrorState>
      <section class="feed-results" :aria-label="t('follows.results')" :aria-busy="loading">
        <ErrorState v-if="error" :title="error"><Button @click="load()">{{ t('common.retry') }}</Button></ErrorState>
        <p v-else-if="loading" role="status">{{ t('follows.loading') }}</p>
        <template v-else-if="visibleArticles.length">
          <section v-for="group in groups" :key="group.date" :data-feed-date="group.date" class="feed-day">
            <h2>{{ group.label }}</h2>
            <ul class="feed-items"><FollowFeedItem v-for="article in group.articles" :key="article.entry_id" :article="article" :libraries="libraries" @open-article="openArticle" @filter-target="selectTarget" /></ul>
          </section>
          <p class="feed-count" role="status">{{ t('follows.resultCount', { count: visibleArticles.length }) }}</p>
          <p v-if="total > shownArticleCount" class="feed-count" role="status">{{ t('follows.limitHint', { shown: shownArticleCount, total }) }}</p>
        </template>
        <EmptyState v-else :title="t(!store.targets.length ? 'follows.emptyTargets' : unreadOnly ? 'follows.emptyUnread' : 'follows.emptyFeed')"><div v-if="!store.targets.length" class="empty-actions"><router-link to="/platform/entries?tab=entities">{{ t('workbench.title') }}</router-link><router-link to="/platform/topics">{{ t('topicManagement.title') }}</router-link></div></EmptyState>
        <nav v-if="results.length || cursorStack.length" class="feed-pagination" :aria-label="t('follows.pagination')" :aria-busy="loading">
          <Button :loading="loading && paginationAction === 'previous'" :disabled="loading || !cursorStack.length" @click="previousPage">{{ t('follows.previousPage') }}</Button>
          <span>{{ t('follows.pageNumber', { page: cursorStack.length + 1 }) }}</span>
          <Button :loading="loading && paginationAction === 'next'" :disabled="loading || !nextCursor" @click="nextPage">{{ t('follows.nextPage') }}</Button>
        </nav>
      </section>
    </section>
    <aside class="feed-targets" :aria-label="t('follows.targets')">
      <div class="feed-target-heading"><h2>{{ t('follows.targets') }}</h2><AddFollowDialog v-if="libraries.length" :libraries="libraries" :library-id="libraryId" @added="changeFilters" /></div>
      <button class="feed-target" type="button" :aria-current="!targetIds.length ? 'true' : undefined" :disabled="loading || marking" @click="selectTarget()"><b>{{ t('follows.allTargets') }}</b><span class="feed-target-count">{{ store.newCount }}</span></button>
      <button v-for="target in store.targets" :key="`${target.target_type}:${target.target_id}`" class="feed-target" type="button" :aria-current="targetIds.length === 1 && targetIds[0] === target.target_id ? 'true' : undefined" :disabled="loading || marking" @click="selectTarget(target.target_id)"><b>{{ target.name }}</b><span class="feed-target-count">{{ target.recent_entry_count }}</span><span class="feed-target-hint">{{ t(`follows.targetTypes.${target.target_type}`) }} · {{ t('follows.recentCount', { count: target.recent_entry_count }) }}</span></button>
      <ErrorState v-if="libraryError" :title="libraryError"><Button @click="loadLibraries">{{ t('common.retry') }}</Button></ErrorState>
    </aside>
  </main>
</template>

<style scoped>
.follow-feed-page { width: 100%; height: 100%; min-height: 0; overflow: auto; display: grid; grid-template-columns: minmax(0, 1fr) 288px; background: var(--bg); color: var(--text); }
.feed-main { min-width: 0; padding: var(--space-5) var(--space-6) var(--space-7); }
.feed-heading { display: flex; flex-wrap: wrap; align-items: end; justify-content: space-between; gap: var(--space-3); margin-bottom: var(--space-2); }
.follow-feed-page .feed-heading h1 { margin: 0; padding: 0; font-size: var(--type-page-size); font-weight: var(--type-page-weight); line-height: 1.05; letter-spacing: -.03em; }
.feed-heading-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); }
.feed-read-filters { display: flex; gap: var(--space-2); }
.feed-number { font-family: var(--mono); }
.feed-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); padding-block: var(--space-2); border-bottom: 1px solid var(--border); }
.feed-library-field { display: flex; align-items: center; gap: var(--space-2); flex: 0 1 260px; min-width: 0; font-size: var(--text-xs); }
.feed-library-field :deep(.rq-select) { flex: 1; min-width: 0; width: auto; }
.feed-toolbar :deep(.rq-control), .feed-toolbar :deep(.rq-button) { min-height: 32px; padding: var(--space-1) var(--space-2); font-size: var(--text-sm); }
.feed-more-filters { min-width: 0; }
.feed-more-filters[open] { flex-basis: 100%; order: 2; }
.feed-more-filters summary { min-height: 32px; padding: var(--space-1) var(--space-2); border: 1px solid var(--border); font-size: var(--text-sm); cursor: pointer; }
.feed-more-filters summary:hover { background: var(--surface-soft); }
.feed-more-filters summary:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.feed-advanced-filters { display: flex; flex-wrap: wrap; align-items: flex-start; gap: var(--space-3); padding-top: var(--space-3); }
.feed-filter-field { display: flex; flex-direction: column; gap: var(--space-1); flex: 1 1 180px; min-width: 0; font-size: var(--text-xs); color: var(--muted); overflow-wrap: anywhere; }
.feed-limit { flex: 0 1 100px; }
.feed-target-search { display: flex; gap: var(--space-1); }
.feed-target-search :deep(input) { min-width: 0; }
.feed-target-select { font-size: var(--text-sm); }
.feed-selected-targets { display: flex; flex-wrap: wrap; gap: var(--space-2); padding-block: var(--space-2); }
.feed-selected-target { display: inline-flex; align-items: center; gap: var(--space-2); max-width: 100%; }
.feed-selected-target > span { overflow-wrap: anywhere; }
.feed-selected-target button { border: 0; padding: var(--space-1); background: transparent; color: var(--text); font: inherit; cursor: pointer; }
.feed-selected-target button:hover { color: var(--accent); }
.feed-selected-target button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.feed-selected-target button:disabled { cursor: wait; opacity: .5; }
.fold-control { flex: 0 1 auto; align-items: flex-start; }
.feed-refresh { flex-shrink: 0; }
.feed-results { display: flex; flex-direction: column; gap: var(--space-4); padding-block: var(--space-4); }
.feed-count { margin: 0; color: var(--muted); font-size: var(--text-xs); }
.feed-day h2 { margin: 0; padding: var(--space-2) 0; border-bottom: 2px solid var(--rule); font-size: var(--type-subsection-size); font-weight: var(--type-subsection-weight); }
.feed-items { list-style: none; margin: 0; padding: 0; }
.feed-pagination { display: flex; flex-wrap: wrap; align-items: center; justify-content: center; gap: var(--space-3); font-size: var(--text-sm); }
.empty-actions { display: flex; flex-wrap: wrap; justify-content: center; gap: var(--space-4); }
.feed-targets { min-width: 0; border-left: 2px solid var(--rule); padding: var(--space-5) var(--space-4); }
.feed-target-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-2); margin-bottom: var(--space-3); }
.feed-targets h2 { margin: 0 0 var(--space-3); font-size: var(--type-eyebrow-size); font-weight: var(--type-eyebrow-weight); }
.feed-target { width: 100%; display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 0 var(--space-2); text-align: left; padding: var(--space-2); border: 0; border-bottom: 1px solid var(--border); background: transparent; color: var(--text); font: inherit; cursor: pointer; overflow-wrap: anywhere; }
.feed-target:hover { background: var(--surface-soft); }
.feed-target-count { font-family: var(--mono); font-weight: 700; }
.feed-target-hint { grid-column: 1 / -1; font-size: var(--text-xs); color: var(--muted); }
.feed-target[aria-current="true"] { background: var(--select); color: var(--on-select); }
.feed-target[aria-current="true"] .feed-target-hint { color: var(--on-select); }
.feed-target:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.feed-target:disabled { cursor: wait; }
.feed-results :deep(.rq-empty) { padding: var(--space-5) 0; }
@media (max-width: 900px) { .follow-feed-page { grid-template-columns: minmax(0, 1fr); } .feed-targets { border-left: 0; border-top: 2px solid var(--rule); } .feed-main { padding: var(--space-4); } }
@media (max-width: 560px) { .feed-library-field { flex: 1 1 180px; } }
</style>
