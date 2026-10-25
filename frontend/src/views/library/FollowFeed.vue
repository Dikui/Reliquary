<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getFollowFeed } from '@/api/follows'
import { useFollowStore } from '@/stores/follows'
import FollowToggle from '@/components/FollowToggle.vue'
import type { FollowFeedCard, FollowLibrary } from '@/types/follows'

const { t, locale } = useI18n()
const route = useRoute(), router = useRouter(), store = useFollowStore()
const libraryId = ref(''), libraries = ref<FollowLibrary[]>([])
const libraryOptions = computed(() => [{ value: '', label: t('follows.allLibraries') }, ...libraries.value.map(lib => ({ value: lib.library_id, label: lib.display_name || lib.library_name }))])
const targetIds = ref<string[]>([]), limit = ref(20)
const results = ref<FollowFeedCard[]>([]), total = ref(0), loading = ref(false), error = ref('')
const cursor = ref(''), nextCursor = ref(''), cursorStack = ref<string[]>([])
const options = computed(() => store.targets.map(target => ({ value: target.target_id, label: t('follows.targetLabel', { name: target.name, type: t(`follows.targetTypes.${target.target_type}`) }) })))
const limitOptions = [20, 50, 100].map(value => ({ value, label: String(value) }))
const storyLocation = (id: string, factId?: string) => ({ path: `/platform/stories/${encodeURIComponent(id)}`, ...(factId ? { hash: `#fact-${encodeURIComponent(factId)}` } : {}) })
const entryLocation = (id: string) => ({ path: '/platform/entries', query: { entry_id: id } })
const timestamp = (value: string) => new Date(value).toLocaleString(locale.value, { timeZoneName: 'short' })
let sequence = 0, disposed = false, mounted = false
const filterKey = () => JSON.stringify({ libraryId: libraryId.value, targetIds: targetIds.value, limit: limit.value })
function readURL() {
  libraryId.value = typeof route.query.library === 'string' ? route.query.library : ''
  const ids = route.query.target_ids
  targetIds.value = [...new Set((Array.isArray(ids) ? ids : typeof ids === 'string' ? ids.split(',') : []).filter((id): id is string => Boolean(id)))]
  const value = Number(route.query.limit)
  limit.value = [20, 50, 100].includes(value) ? value : 20
}
function syncURL() {
  void router.replace({ query: { ...(libraryId.value ? { library: libraryId.value } : {}), ...(targetIds.value.length ? { target_ids: targetIds.value.join(',') } : {}), ...(limit.value !== 20 ? { limit: String(limit.value) } : {}) } })
}
function resetPagination() { cursor.value = ''; nextCursor.value = ''; cursorStack.value = [] }
function changeFilters() { resetPagination(); syncURL(); void load() }
function refresh() { resetPagination(); void load() }
async function load() {
  if (disposed) return
  const current = ++sequence
  loading.value = true; error.value = ''
  try {
    await store.loadTargets(true)
    if (current !== sequence || disposed) return
    const response = await getFollowFeed({ ...(libraryId.value ? { libraries: [libraryId.value] } : {}), ...(cursor.value ? { cursor: cursor.value } : {}), ...(targetIds.value.length ? { target_ids: [...targetIds.value] } : {}), limit: limit.value })
    if (!response.success) throw new Error(response.message)
    if (current !== sequence || disposed) return
    total.value = response.data.total
    nextCursor.value = response.data.next_cursor || ''
    results.value = response.data.results
    libraries.value = response.data.libraries
  } catch { if (current === sequence && !disposed) error.value = t('follows.feedFailed') }
  finally { if (current === sequence && !disposed) loading.value = false }
}
function nextPage() {
  if (loading.value || !nextCursor.value) return
  cursorStack.value = [...cursorStack.value, cursor.value]
  cursor.value = nextCursor.value
  void load()
}
function previousPage() {
  if (loading.value || !cursorStack.value.length) return
  cursor.value = cursorStack.value.at(-1) || ''
  cursorStack.value = cursorStack.value.slice(0, -1)
  void load()
}
watch(() => route.query, () => {
  const previous = filterKey(); readURL()
  if (mounted && previous !== filterKey()) { resetPagination(); void load() }
})
onMounted(() => { readURL(); mounted = true; void load() })
onUnmounted(() => { disposed = true; sequence++ })
</script>

<template>
  <main class="follow-feed-page">
    <header class="feed-heading"><h1>{{ t('follows.title') }}</h1><p>{{ t('follows.description') }}</p></header>
    <form class="feed-toolbar" @submit.prevent="changeFilters">
      <label class="feed-filter-field"><span>{{ t('follows.library') }}</span><t-select v-model="libraryId" :options="libraryOptions" :disabled="loading" :aria-label="t('follows.library')" @change="changeFilters" /></label>
      <label class="feed-filter-field"><span>{{ t('follows.filterTarget') }}</span><t-select v-model="targetIds" :options="options" multiple clearable filterable :loading="store.targetsLoading" :disabled="loading" :placeholder="t('follows.allTargets')" :aria-label="t('follows.filterTarget')" @change="changeFilters" /></label>
      <label class="feed-filter-field"><span>{{ t('follows.limit') }}</span><t-select v-model="limit" :options="limitOptions" :disabled="loading" :aria-label="t('follows.limit')" @change="changeFilters" /></label>
      <t-button variant="outline" :loading="loading" @click="refresh">{{ t('follows.refresh') }}</t-button>
    </form>
    <section class="feed-results" :aria-label="t('follows.results')" :aria-busy="loading">
      <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button variant="text" @click="load">{{ t('common.retry') }}</t-button></template></t-alert>
      <t-loading v-else-if="loading" :text="t('follows.loading')" />
      <template v-else-if="results.length">
        <p class="feed-count" role="status">{{ t('follows.resultCount', { count: results.length }) }} · {{ t('follows.limitHint', { shown: results.length, total }) }}</p>
        <article v-for="card in results" :key="card.story.id" class="story-card" :data-story-id="card.story.id">
          <header class="card-heading"><h2><router-link :to="storyLocation(card.story.id)">{{ card.story.title }}</router-link></h2><FollowToggle target-type="story" :target-id="card.story.id" :name="card.story.title" /></header>
          <p class="muted"><span>{{ t('stories.lastFact') }}</span> <time :datetime="card.story.last_fact_at">{{ timestamp(card.story.last_fact_at) }}</time></p>
          <p class="matched-follows"><strong>{{ t('follows.matchedTargets') }}</strong><span v-for="target in card.matched_follows" :key="`${target.target_type}:${target.target_id}`">{{ t('follows.targetLabel', { name: target.name, type: t(`follows.targetTypes.${target.target_type}`) }) }}</span></p>
          <section class="trigger-fact" :aria-label="t('follows.triggerFact')">
            <h3>{{ t('follows.triggerFact') }}</h3>
            <router-link :to="storyLocation(card.story.id, card.trigger_fact.id)">{{ card.trigger_fact.title }}</router-link>
            <p v-if="card.trigger_fact.no_source" class="muted">{{ t('stories.noSource') }}</p>
            <ul v-else class="sources"><li v-for="source in card.trigger_fact.sources" :key="source.entry_id"><router-link :to="entryLocation(source.entry_id)">{{ source.title }}</router-link><blockquote>{{ source.quote }}</blockquote></li></ul>
          </section>
          <section :aria-label="t('follows.recentFacts')"><h3>{{ t('follows.recentFacts') }}</h3><ol class="recent-facts"><li v-for="fact in card.recent_facts" :key="fact.id"><router-link :to="storyLocation(card.story.id, fact.id)">{{ fact.title }}</router-link><span v-if="fact.no_source" class="muted"> · {{ t('stories.noSource') }}</span></li></ol></section>
        </article>
      </template>
      <div v-else class="feed-empty"><t-empty :description="t(store.targets.length ? 'follows.emptyFeed' : 'follows.emptyTargets')" /><div v-if="!store.targets.length" class="empty-actions"><router-link to="/platform/stories">{{ t('stories.title') }}</router-link><router-link to="/platform/entries?tab=entities">{{ t('knowledgeWorkbench.title') }}</router-link></div></div>
      <nav v-if="results.length || cursorStack.length" class="feed-pagination" :aria-label="t('follows.pagination')">
        <t-button variant="outline" :disabled="loading || !cursorStack.length" @click="previousPage">{{ t('follows.previousPage') }}</t-button>
        <span>{{ t('follows.pageNumber', { page: cursorStack.length + 1 }) }}</span>
        <t-button variant="outline" :disabled="loading || !!error || !nextCursor" @click="nextPage">{{ t('follows.nextPage') }}</t-button>
      </nav>
    </section>
  </main>
</template>

<style scoped lang="less">
.follow-feed-page { width: 100%; height: 100%; overflow-y: auto; box-sizing: border-box; padding: 32px; color: var(--td-text-color-primary); }
.feed-heading, .feed-toolbar, .feed-results { max-width: 1000px; margin-inline: auto; }
h1 { margin: 0 0 8px; font-size: 22px; }
h2 { margin: 0; font-size: 18px; }
h3 { margin: 0 0 10px; font-size: 14px; }
.feed-heading p { margin: 0 0 24px; line-height: 1.6; color: var(--td-text-color-secondary); }
.feed-toolbar { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); align-items: end; gap: 14px 16px; padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); }
.feed-filter-field { display: flex; flex-direction: column; gap: 6px; min-width: 0; font-size: 13px; color: var(--td-text-color-secondary); overflow-wrap: anywhere; }
.feed-filter-field :deep(.t-select__wrap) { width: 100%; min-width: 0; }
.feed-toolbar > :last-child { justify-self: start; }
.feed-results { display: grid; gap: 16px; padding-block: 24px; min-height: 160px; }
.feed-count, .muted { color: var(--td-text-color-secondary); font-size: 13px; }
.feed-count { margin: 0; }
.story-card { padding: 24px; min-width: 0; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); overflow-wrap: anywhere; }
.card-heading { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 12px; align-items: center; }
.card-heading h2 { flex: 1; min-width: 0; }
.matched-follows { display: flex; flex-wrap: wrap; gap: 6px 12px; font-size: 13px; }
.trigger-fact { margin-block: 20px; padding: 16px; border-inline-start: 3px solid var(--td-brand-color); background: var(--td-bg-color-secondarycontainer); }
.sources { list-style: none; margin: 12px 0 0; padding: 0; font-size: 13px; }
blockquote { margin: 8px 0 12px; white-space: pre-wrap; }
.recent-facts { padding-inline-start: 20px; margin-bottom: 0; line-height: 1.8; }
.feed-pagination { display: flex; flex-wrap: wrap; align-items: center; justify-content: center; gap: 12px; color: var(--td-text-color-secondary); font-size: 13px; }
.empty-actions { display: flex; justify-content: center; flex-wrap: wrap; gap: 24px; margin-top: 16px; }
a { color: var(--td-brand-color); text-decoration: none; }
a:hover { text-decoration: underline; }
a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
@media (max-width: 640px) { .follow-feed-page { padding: 16px; } .story-card { padding: 16px; } }
@media (max-width: 560px) { .feed-toolbar { grid-template-columns: minmax(0, 1fr); } }
</style>
