<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getKnowledgeSearchOptions, searchKnowledgeArticles } from '@/api/knowledge-search'
import KnowledgeSearchFilters from '@/components/KnowledgeSearchFilters.vue'
import KnowledgeSearchArticle from '@/components/KnowledgeSearchArticle.vue'
import type { KnowledgeSearchArticle as Article, KnowledgeSearchOptions } from '@/types/knowledgeSearch'
import { buildKnowledgeSearchRequest, knowledgeSearchErrorMessage, defaultSearchFilters, searchStateFromURL, searchStateToURL } from '@/utils/knowledgeSearch'

const { t } = useI18n()
const router = useRouter(), route = useRoute()
const query = ref('')
const filters = ref(defaultSearchFilters())
const options = ref<KnowledgeSearchOptions>({ entities: [], topics: [] })
const optionsLoading = ref(false), optionsError = ref('')
const results = ref<Article[]>([])
const excludedMissingPublishedAt = ref(0)
const loading = ref(false), hasSearched = ref(false), error = ref('')
const inputRef = ref<HTMLInputElement | null>(null)
const composing = ref(false)
let sequence = 0, synchronizing = false
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
  if (composing.value || synchronizing) return
  debounceTimer = setTimeout(() => { void runSearch() }, 300)
}
async function runSearch(updateURL = true) {
  invalidate()
  if (composing.value) return
  const request = buildKnowledgeSearchRequest(query.value, filters.value)
  if (updateURL) void router.replace({ query: searchStateToURL(query.value, filters.value) })
  if (!request.query) { results.value = []; hasSearched.value = false; error.value = ''; return }
  if (filters.value.published_from && filters.value.published_to && filters.value.published_from > filters.value.published_to) {
    error.value = t('knowledgeSearch.invalidDateRange'); return
  }
  if (!Number.isInteger(filters.value.limit) || filters.value.limit < 1 || filters.value.limit > 100) {
    error.value = t('knowledgeSearch.invalidLimit'); return
  }
  const current = sequence
  loading.value = true
  hasSearched.value = true
  error.value = ''
  try {
    const response = await searchKnowledgeArticles(request)
    if (current !== sequence) return
    if (!response.success) throw response
    results.value = response.data.results || []
    excludedMissingPublishedAt.value = response.data.excluded_missing_published_at || 0
  } catch (cause: any) {
    if (current !== sequence) return
    results.value = []
    error.value = knowledgeSearchErrorMessage(cause, t)
  } finally { if (current === sequence) loading.value = false }
}
async function loadOptions() {
  optionsLoading.value = true
  optionsError.value = ''
  try {
    const response = await getKnowledgeSearchOptions()
    if (!response.success) throw new Error(response.message)
    options.value = response.data
  } catch { optionsError.value = t('knowledgeSearch.optionsFailed') }
  finally { optionsLoading.value = false }
}
function clearQuery() {
  query.value = ''
  invalidate()
  results.value = []
  excludedMissingPublishedAt.value = 0
  hasSearched.value = false
  error.value = ''
  void router.replace({ query: searchStateToURL('', filters.value) })
  inputRef.value?.focus()
}
function finishComposition() { composing.value = false; scheduleSearch() }
function openArticle(article: Article) { void router.push({ path: '/platform/knowledge', query: { knowledge_id: article.article_id } }) }
function filterEntity(id: string) { filters.value.entity_ids = [id] }
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
onMounted(() => { void loadOptions() })
onUnmounted(invalidate)
</script>

<template>
  <main class="knowledge-search-page">
    <header class="search-page-heading"><h1>{{ t('knowledgeSearch.title') }}</h1><p>{{ t('knowledgeSearch.description') }}</p></header>
    <form class="search-form" novalidate @submit.prevent="runSearch()">
      <div class="query-row">
        <div class="query-field">
          <input ref="inputRef" v-model="query" type="search" :aria-label="t('knowledgeSearch.query')" :placeholder="t('knowledgeSearch.queryPlaceholder')" @compositionstart="composing = true; invalidate()" @compositionend="finishComposition" />
          <button v-if="query" type="button" class="clear-query" :aria-label="t('knowledgeSearch.clearQuery')" @click="clearQuery"><t-icon name="close" /></button>
        </div>
        <t-button type="submit" :loading="loading">{{ t('knowledgeSearch.search') }}</t-button>
      </div>
      <KnowledgeSearchFilters v-model="filters" :options="options" :loading="optionsLoading" />
      <t-button variant="text" type="button" @click="filters = defaultSearchFilters()">{{ t('knowledgeSearch.resetFilters') }}</t-button>
    </form>
    <t-alert v-if="optionsError" theme="error" :message="optionsError"><template #operation><t-button variant="text" @click="loadOptions">{{ t('common.retry') }}</t-button></template></t-alert>
    <section class="search-results" :aria-label="t('knowledgeSearch.results')" :aria-busy="loading">
      <p v-if="excludedMissingPublishedAt" class="excluded-count" role="status">{{ t('knowledgeSearch.excludedMissingPublishedAt', { count: excludedMissingPublishedAt }) }}</p>
      <t-alert v-if="error" theme="error" :message="error"><template #operation><t-button variant="text" @click="runSearch()">{{ t('common.retry') }}</t-button></template></t-alert>
      <t-loading v-else-if="loading" :text="t('knowledgeSearch.searching')" />
      <template v-else-if="results.length">
        <p class="result-count" role="status">{{ t('knowledgeSearch.resultCount', { count: results.length }) }}</p>
        <KnowledgeSearchArticle v-for="article in results" :key="article.article_id" :article="article" :query="query" @open-article="openArticle" @open-entity="filterEntity" />
      </template>
      <t-empty v-else :description="t(hasSearched ? 'knowledgeSearch.noResults' : 'knowledgeSearch.idle')" />
    </section>
  </main>
</template>

<style scoped lang="less">
.knowledge-search-page { width: 100%; height: 100%; overflow-y: auto; box-sizing: border-box; padding: 32px; color: var(--td-text-color-primary); }
.search-page-heading, .search-form, .search-results { max-width: 1000px; margin-inline: auto; }
.search-page-heading h1 { font-size: 22px; margin: 0 0 8px; }
.search-page-heading p { margin: 0 0 24px; color: var(--td-text-color-secondary); line-height: 1.6; }
.search-form { display: grid; gap: 18px; padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); }
.query-row { display: flex; gap: 12px; align-items: center; }
.query-field { flex: 1; display: flex; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-default); min-width: 0; }
.query-field:focus-within { border-color: var(--td-brand-color); outline: 2px solid var(--td-brand-color-light); }
.query-field input { min-width: 0; flex: 1; border: 0; background: transparent; color: var(--td-text-color-primary); font: inherit; padding: 8px 12px; outline: none; }
.query-field input::-webkit-search-cancel-button { display: none; }
.clear-query { display: flex; align-items: center; border: 0; padding: 6px 10px; background: transparent; color: var(--td-text-color-secondary); cursor: pointer; }
.clear-query:hover { color: var(--td-brand-color); }
.clear-query:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: -2px; }
.search-form > :deep(.t-button) { justify-self: start; }
.search-results { display: grid; gap: 16px; min-height: 120px; padding-top: 24px; }
.excluded-count, .result-count { color: var(--td-text-color-secondary); font-size: 13px; margin: 0; }
@media (max-width: 640px) { .knowledge-search-page { padding: 20px 12px; } .search-form { padding: 14px; } }
</style>
