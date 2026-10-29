<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getSearchOptions, searchEntries } from '@/api/search'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'
import { Button, EmptyState, ErrorState, LibMark, Skeleton, Tabs } from '@/components/ui'
import GraphSearchPanel from './GraphSearchPanel.vue'
import { scopedLibraryIDs } from '@/utils/libraryScope'
import { libraryColorVar } from '@/utils/libraryColor'
import { useLibraryScope } from '@/composables/useLibraryScope'
import { entryDetailLocation } from '@/utils/bodyReference'
import type { SourceLocation } from '@/types/search'
import SearchFilters from '@/components/SearchFilters.vue'
import SearchResult from './SearchResult.vue'
import type { SearchEntry as Article, SearchOptions } from '@/types/search'
import { buildSearchRequest, searchErrorMessage, defaultSearchFilters, searchStateFromURL, searchStateToURL, searchErrorCode } from '@/utils/search'

const { t } = useI18n()
const router = useRouter(), route = useRoute()
const libraryScope = useLibraryScope()
const activeTab = ref('normal')
const libraries = ref<LibraryCatalogItem[]>([])
const librariesLoading = ref(true), librariesError = ref('')
const vectorUnavailable = ref(false), dependencyUnavailable = ref(false)
const elapsedMs = ref<number | null>(null)
const tabs = computed(() => [{ value: 'normal', label: t('searchPage.normal') }, { value: 'graph', label: t('searchPage.graph') }])
let catalogSequence = 0
const query = ref('')
const filters = ref(defaultSearchFilters())
const options = ref<SearchOptions>({ entities: [], topics: [] })
const optionsLoading = ref(false), optionsError = ref('')
const results = ref<Article[]>([])
const excludedMissingPublishedAt = ref(0)
const loading = ref(false), hasSearched = ref(false), error = ref('')
const inputRef = ref<HTMLInputElement | null>(null)
const composing = ref(false)
let sequence = 0, synchronizing = false, disposed = false
let debounceTimer: ReturnType<typeof setTimeout> | undefined
const stateKey = computed(() => JSON.stringify({ query: query.value, filters: filters.value }))

function invalidate() {
  sequence++
  clearTimeout(debounceTimer)
  loading.value = false
}
function scheduleSearch() {
  invalidate()
  error.value = ''
  results.value = []
  excludedMissingPublishedAt.value = 0
  hasSearched.value = false
  vectorUnavailable.value = false
  elapsedMs.value = null
  if (composing.value || synchronizing || librariesLoading.value || librariesError.value) return
  debounceTimer = setTimeout(() => { void runSearch() }, 300)
}
async function runSearch(updateURL = true) {
  invalidate()
  if (composing.value || disposed) return
  const request = { ...buildSearchRequest(query.value, filters.value), libraries: [...libraryScope.value] }
  if (updateURL) void router.replace({ query: searchStateToURL(query.value, filters.value) })
  if (!request.query) { results.value = []; hasSearched.value = false; error.value = ''; return }
  if (librariesLoading.value) return
  if (!request.libraries.length) { error.value = t('searchPage.chooseLibraries'); return }
  if (filters.value.published_from && filters.value.published_to && filters.value.published_from > filters.value.published_to) {
    error.value = t('knowledgeSearch.invalidDateRange'); return
  }
  if (!Number.isInteger(filters.value.limit) || filters.value.limit < 1 || filters.value.limit > 100) {
    error.value = t('knowledgeSearch.invalidLimit'); return
  }
  const current = sequence
  const started = performance.now()
  results.value = []
  excludedMissingPublishedAt.value = 0
  vectorUnavailable.value = false
  dependencyUnavailable.value = false
  elapsedMs.value = null
  loading.value = true
  hasSearched.value = true
  error.value = ''
  try {
    const response = await searchEntries(request)
    if (current !== sequence) return
    if (!response.success) throw response
    results.value = response.data.results || []
    vectorUnavailable.value = !!response.data.vector_unavailable
    elapsedMs.value = performance.now() - started
    excludedMissingPublishedAt.value = response.data.excluded_missing_published_at || 0
  } catch (cause: unknown) {
    if (current !== sequence) return
    results.value = []
    error.value = searchErrorMessage(cause, t)
    dependencyUnavailable.value = searchErrorCode(cause) === 2400
  } finally { if (current === sequence) loading.value = false }
}
async function loadLibraries() {
  invalidate()
  const current = ++catalogSequence
  librariesLoading.value = true
  librariesError.value = ''
  try {
    const response = await listLibraries()
    if (current !== catalogSequence) return
    if (!response.success) throw new Error('Library catalog failed')
    libraries.value = response.data || []
    libraryScope.value = scopedLibraryIDs(libraryScope.value, libraries.value)
    await loadOptions()
    if (current !== catalogSequence) return
    librariesLoading.value = false
    if (current === catalogSequence && query.value.trim()) await runSearch(false)
  } catch { if (current === catalogSequence) librariesError.value = t('libraryNavigation.loadFailed') }
  finally { if (current === catalogSequence) librariesLoading.value = false }
}
function toggleLibrary(id: string) {
  libraryScope.value = libraryScope.value.includes(id) ? libraryScope.value.filter(value => value !== id) : [...libraryScope.value, id]
}
let optionsSequence = 0
async function loadOptions() {
  const current = ++optionsSequence
  optionsLoading.value = true
  optionsError.value = ''
  try {
    const response = await getSearchOptions(libraryScope.value)
    if (current !== optionsSequence) return
    if (!response.success) throw new Error(response.message)
    options.value = response.data
  } catch { if (current === optionsSequence) optionsError.value = t('knowledgeSearch.optionsFailed') }
  finally { if (current === optionsSequence) optionsLoading.value = false }
}
function clearQuery() {
  query.value = ''
  invalidate()
  results.value = []
  excludedMissingPublishedAt.value = 0
  hasSearched.value = false
  error.value = ''
  vectorUnavailable.value = false
  elapsedMs.value = null
  void router.replace({ query: searchStateToURL('', filters.value) })
  inputRef.value?.focus()
}
function finishComposition() { composing.value = false; scheduleSearch() }
function openArticle(article: Article, location?: SourceLocation) { void router.push(entryDetailLocation(article.entry_id, location ?? article.snippets?.flatMap(item => item.locations || [])[0])) }
function filterEntity(id: string) { filters.value.entity_ids = [id] }
watch(libraryScope, () => {
  if (librariesLoading.value) return
  filters.value.entity_ids = []
  filters.value.topic_ids = []
  options.value = { entities: [], topics: [] }
  if (libraryScope.value.length) void loadOptions()
  else { optionsSequence++; optionsLoading.value = false; optionsError.value = '' }
  scheduleSearch()
}, { deep: true })
watch(stateKey, () => { if (!synchronizing) scheduleSearch() })
watch(() => route.query, incoming => {
  const state = searchStateFromURL(incoming)
  if (JSON.stringify(state) === stateKey.value) return
  synchronizing = true
  invalidate()
  query.value = state.query
  filters.value = state.filters
  void nextTick(() => { synchronizing = false; void runSearch(false) })
}, { immediate: true })
onMounted(() => { void loadLibraries() })
onUnmounted(() => { disposed = true; catalogSequence++; optionsSequence++; invalidate() })
</script>

<template>
  <main class="search-page">
    <div class="search-wrap">
      <h1>{{ t('searchPage.pageTitle') }}</h1>
      <ErrorState v-if="librariesError" :title="librariesError"><Button @click="loadLibraries">{{ t('common.retry') }}</Button></ErrorState>
      <Skeleton v-else-if="librariesLoading" :label="t('common.loading')" />
      <EmptyState v-else-if="!libraries.length" :title="t('searchPage.noLibraries')" />
      <Tabs v-else v-model="activeTab" :label="t('searchPage.pageTitle')" :items="tabs" class="search-tabs">
        <template #normal>
          <div class="search-normal">
            <form class="search-form" role="search" novalidate @submit.prevent="runSearch()">
              <div class="query-row">
                <div class="query-field">
                  <input ref="inputRef" v-model="query" type="search" :aria-label="t('knowledgeSearch.query')" :placeholder="t('knowledgeSearch.queryPlaceholder')" @compositionstart="composing = true; invalidate()" @compositionend="finishComposition" />
                  <button v-if="query" type="button" class="clear-query" :aria-label="t('knowledgeSearch.clearQuery')" @click="clearQuery">×</button>
                </div>
                <Button variant="primary" type="submit" :loading="loading">{{ t('knowledgeSearch.search') }}</Button>
              </div>
              <SearchFilters v-model="filters" :options="options" :loading="optionsLoading">
                <template #libraries>
                <button v-for="(library, index) in libraries" :key="library.id" type="button" class="library-filter" :style="{ '--lib': libraryColorVar(library.color) }" :aria-pressed="libraryScope.includes(library.id)" @click="toggleLibrary(library.id)"><LibMark :color="library.color" /><span>{{ library.display_name || library.name }}</span><span class="library-number">{{ t('entries.libraryNumber', { number: String(index + 1).padStart(2, '0') }) }}</span></button>
                </template>
              </SearchFilters>
            </form>
            <ErrorState v-if="optionsError" :title="optionsError"><Button @click="loadOptions">{{ t('common.retry') }}</Button></ErrorState>
            <section class="search-results" :aria-label="t('knowledgeSearch.results')" :aria-busy="loading">
              <p v-if="vectorUnavailable" class="search-notice" role="status">{{ t('searchPage.vectorFallback') }}</p>
              <p v-if="excludedMissingPublishedAt" class="search-notice" role="status">{{ t('knowledgeSearch.excludedMissingPublishedAt', { count: excludedMissingPublishedAt }, excludedMissingPublishedAt) }}</p>
              <ErrorState v-if="error" :title="error" :description="dependencyUnavailable ? t('searchPage.fallbackCapability') : undefined"><Button @click="runSearch()">{{ t('common.retry') }}</Button></ErrorState>
              <div v-else-if="loading" class="loading-results"><Skeleton v-for="n in 3" :key="n" :label="t('knowledgeSearch.searching')" /></div>
              <template v-else-if="results.length">
                <div class="summary-line" role="status"><strong>{{ t('knowledgeSearch.resultCount', { count: results.length }, results.length) }} · {{ t('searchPage.librariesCount', { count: libraryScope.length }) }}</strong><span v-if="elapsedMs !== null">{{ t('searchPage.elapsed', { seconds: (elapsedMs / 1000).toFixed(2) }) }}</span></div>
                <ol class="results-list"><li v-for="article in results" :key="article.entry_id"><SearchResult :article="article" :query="query" :libraries="libraries" @open-article="openArticle" @open-entity="filterEntity" /></li></ol>
              </template>
              <EmptyState v-else :title="t(hasSearched ? 'knowledgeSearch.noResults' : 'knowledgeSearch.idle')" />
            </section>
          </div>
        </template>
        <template #graph><GraphSearchPanel :libraries="libraries" /></template>
      </Tabs>
    </div>
  </main>
</template>

<style scoped>
.search-page { width: 100%; height: 100%; min-height: 0; overflow-y: auto; color: var(--text); background: var(--bg); font: var(--text-md)/var(--leading) var(--font); }
.search-wrap { display: flex; flex-direction: column; gap: var(--space-4); max-width: 64rem; padding: var(--space-5) var(--space-6) var(--space-7); }
h1 { margin: 0; font-size: var(--text-3xl); font-weight: 800; line-height: 1.08; letter-spacing: -.035em; }
.search-normal, .search-form, .search-results, .loading-results { display: grid; gap: var(--space-4); min-width: 0; }
.search-tabs :deep(.rq-tabs__panel:not([hidden])) { padding-top: var(--space-4); }
.query-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; border: 2px solid var(--rule); }
.query-field { display: flex; min-width: 0; }
.query-field input { min-width: 0; flex: 1; border: 0; border-radius: 0; background: var(--clear); color: var(--text); font: 700 var(--text-lg)/1.4 var(--font); padding: var(--space-3) var(--space-4); }
.query-field input::-webkit-search-cancel-button { display: none; }
.query-row:focus-within { outline: 2px solid var(--rule); outline-offset: 3px; }
.query-row :deep(.rq-button) { min-height: 60px; border: 0; padding-inline: var(--space-6); }
.clear-query { border: 0; background: var(--clear); color: var(--text); padding-inline: var(--space-3); font-size: var(--text-xl); cursor: pointer; }
.library-filter { display: inline-flex; align-items: center; gap: var(--space-2); border: 1px solid var(--border); border-radius: 0; background: var(--clear); color: var(--text); padding: var(--space-1) var(--space-2); font: inherit; font-size: var(--text-sm); cursor: pointer; }
.library-filter[aria-pressed="true"] { border-color: var(--rule); font-weight: 700; box-shadow: inset 0 -3px var(--lib); }
.library-filter:hover { background: var(--surface-soft); }
.library-number { color: var(--muted); font-family: var(--mono); font-size: var(--text-xs); }
.summary-line { display: flex; flex-wrap: wrap; justify-content: space-between; gap: var(--space-2); border-top: 2px solid var(--rule); padding-top: var(--space-3); }
.summary-line > span { font-size: var(--text-sm); font-family: var(--mono); color: var(--muted); }
.results-list { list-style: none; padding: 0; margin: 0; }
.search-notice { padding: var(--space-3); background: var(--surface-soft); border-left: 2px solid var(--rule); margin: 0; font-size: var(--text-sm); }
button:focus-visible, input:focus-visible { outline: 2px solid var(--rule); outline-offset: 2px; }
@media (max-width: 760px) { .search-wrap { padding: var(--space-4); } .query-row :deep(.rq-button) { padding-inline: var(--space-4); } }
</style>
