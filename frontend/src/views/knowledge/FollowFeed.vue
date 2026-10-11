<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getFollowFeed } from '@/api/follows'
import { useFollowStore } from '@/stores/follows'
import KnowledgeSearchArticle from '@/components/KnowledgeSearchArticle.vue'
import type { FollowFeedArticle } from '@/types/follows'
import { isRFC3339After } from '@/utils/rfc3339'

const { t } = useI18n()
const route = useRoute(), router = useRouter(), store = useFollowStore()
const targetIds = ref<string[]>([]), collapseSimilar = ref(true), limit = ref(20)
const results = ref<FollowFeedArticle[]>([]), total = ref(0), loading = ref(false), error = ref(''), viewError = ref(''), marking = ref(false)
const viewRetryAction = ref<'open' | 'all'>('open')
const options = computed(() => store.targets.map(target => ({ value: target.target_id, label: `${target.name} (${t(`follows.targetTypes.${target.target_type}`)})` })))
const limitOptions = [20, 50, 100].map(value => ({ value, label: String(value) }))
let sequence = 0, disposed = false, mounted = false, opened = false, snapshotLastViewed: string | null = null, snapshotViewedAt: string | undefined
const countArticles = (articles: FollowFeedArticle[]): number => articles.reduce((count, article) => count + 1 + countArticles(article.similar_reports || []), 0)
const shownArticleCount = computed(() => countArticles(results.value))
function applyNewSnapshot(articles: FollowFeedArticle[]) { for (const article of articles) { article.is_new = article.entered_at !== null && (snapshotLastViewed === null || isRFC3339After(article.entered_at, snapshotLastViewed)); applyNewSnapshot(article.similar_reports || []) } }
const filterKey = () => JSON.stringify({ targetIds: targetIds.value, collapseSimilar: collapseSimilar.value, limit: limit.value })
function readURL() {
  const ids = route.query.target_ids
  targetIds.value = [...new Set((Array.isArray(ids) ? ids : typeof ids === 'string' ? ids.split(',') : []).filter((id): id is string => Boolean(id)))]
  collapseSimilar.value = route.query.collapse_similar !== 'false'
  const value = Number(route.query.limit)
  limit.value = [20, 50, 100].includes(value) ? value : 20
}
function syncURL() {
  void router.replace({ query: { ...(targetIds.value.length ? { target_ids: targetIds.value.join(',') } : {}), ...(!collapseSimilar.value ? { collapse_similar: 'false' } : {}), ...(limit.value !== 20 ? { limit: String(limit.value) } : {}) } })
}
async function recordOpening() {
  if (!snapshotViewedAt || marking.value) return
  marking.value = true
  viewError.value = ''
  viewRetryAction.value = 'open'
  try { await store.markViewed(snapshotViewedAt) }
  catch { if (!disposed) viewError.value = t('follows.markFailed') }
  finally { if (!disposed) marking.value = false }
}
async function load(updateURL = false) {
  if (disposed) return
  if (updateURL) syncURL()
  const current = ++sequence
  loading.value = true
  error.value = ''
  try {
    await store.loadTargets(true)
    if (current !== sequence || disposed) return
    const response = await getFollowFeed({ include_seen: true, ...(targetIds.value.length ? { target_ids: [...targetIds.value] } : {}), limit: limit.value, collapse_similar: collapseSimilar.value })
    if (!response.success) throw new Error(response.message)
    if (current !== sequence || disposed) return
    total.value = response.data.total || 0
    if (!opened) {
      opened = true
      snapshotLastViewed = response.data.last_viewed_at
      snapshotViewedAt = response.data.viewed_at
      results.value = response.data.results || []
      applyNewSnapshot(results.value)
      await recordOpening()
    } else { results.value = response.data.results || []; applyNewSnapshot(results.value) }
  } catch { if (current === sequence && !disposed) error.value = t('follows.feedFailed') }
  finally { if (current === sequence && !disposed) loading.value = false }
}
function clearNew(articles: FollowFeedArticle[]) { for (const article of articles) { article.is_new = false; clearNew(article.similar_reports || []) } }
async function markAllViewed() {
  if (marking.value || loading.value) return
  marking.value = true
  viewError.value = ''
  viewRetryAction.value = 'all'
  try {
    const state = await store.markViewed()
    if (disposed) return
    snapshotLastViewed = state.last_viewed_at
    snapshotViewedAt = state.last_viewed_at || undefined
    clearNew(results.value)
    await load()
  } catch { if (!disposed) viewError.value = t('follows.markFailed') }
  finally { if (!disposed) marking.value = false }
}
function openArticle(article: FollowFeedArticle) { void router.push({ path: '/platform/knowledge', query: { knowledge_id: article.article_id } }) }
function openEntity(id: string) { void router.push({ path: '/platform/knowledge', query: { tab: 'entities', entity_id: id } }) }
function retryViewed() { return viewRetryAction.value === 'all' ? markAllViewed() : recordOpening() }
watch(() => route.query, () => {
  const previous = filterKey()
  readURL()
  if (mounted && previous !== filterKey()) void load()
})
onMounted(() => { readURL(); mounted = true; void load() })
onUnmounted(() => { disposed = true; sequence++ })
</script>

<template>
  <main class="follow-feed-page">
    <header class="feed-heading"><div><h1>{{ t('follows.title') }}</h1><p>{{ t('follows.description') }}</p></div><t-button variant="outline" :loading="marking" :disabled="loading || marking" @click="markAllViewed">{{ t('follows.markAllViewed') }}</t-button></header>
    <form class="feed-toolbar" @submit.prevent="load(true)">
      <label class="feed-filter-field"><span>{{ t('follows.filterTarget') }}</span><t-select v-model="targetIds" :options="options" multiple clearable filterable :loading="store.targetsLoading" :disabled="loading || marking" :placeholder="t('follows.allTargets')" :aria-label="t('follows.filterTarget')" @change="load(true)" /></label>
      <label class="feed-filter-field feed-limit"><span>{{ t('follows.limit') }}</span><t-select v-model="limit" :options="limitOptions" :disabled="loading || marking" :aria-label="t('follows.limit')" @change="load(true)" /></label>
      <label class="feed-filter-field fold-control"><span>{{ t('knowledgeSearch.collapseSimilar') }}</span><t-switch v-model="collapseSimilar" :disabled="loading || marking" :aria-label="t('knowledgeSearch.collapseSimilar')" @change="load(true)" /></label>
      <t-button class="feed-refresh" variant="outline" :loading="loading" :disabled="marking" @click="load()">{{ t('follows.refresh') }}</t-button>
    </form>
    <t-alert v-if="viewError" theme="error" :message="viewError" role="alert"><template #operation><t-button variant="text" :loading="marking" @click="retryViewed">{{ t('common.retry') }}</t-button></template></t-alert>
    <section class="feed-results" :aria-label="t('follows.results')" :aria-busy="loading">
      <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button variant="text" @click="load()">{{ t('common.retry') }}</t-button></template></t-alert>
      <t-loading v-else-if="loading" :text="t('follows.loading')" />
      <template v-else-if="results.length">
        <p class="feed-count" role="status">{{ t('follows.resultCount', { count: results.length }) }}</p>
        <p v-if="total > shownArticleCount" class="feed-count" role="status">{{ t('follows.limitHint', { shown: shownArticleCount, total }) }}</p>
        <KnowledgeSearchArticle v-for="article in results" :key="article.article_id" :article="article" query="" follow-feed @open-article="openArticle($event as FollowFeedArticle)" @open-entity="openEntity" />
      </template>
      <div v-else class="feed-empty"><t-empty :description="t(store.targets.length ? 'follows.emptyFeed' : 'follows.emptyTargets')" /><div v-if="!store.targets.length" class="empty-actions"><router-link to="/platform/knowledge?tab=entities">{{ t('knowledgeWorkbench.title') }}</router-link><router-link to="/platform/topics">{{ t('topicManagement.title') }}</router-link></div></div>
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
.empty-actions { display: flex; justify-content: center; gap: 24px; margin-top: 16px; }
.empty-actions a { color: var(--td-brand-color); }
.empty-actions a:hover { text-decoration: underline; }
.empty-actions a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
@media (max-width: 640px) { .follow-feed-page { padding: 16px; } .feed-heading { flex-direction: column; margin-bottom: 16px; } .feed-heading p { margin-bottom: 0; } }
@media (max-width: 560px) { .feed-toolbar { grid-template-columns: minmax(0, 1fr); } }
</style>
