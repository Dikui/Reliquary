<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import type { LibraryCatalogItem } from '@/api/library'
import type { FollowFeedEntry } from '@/types/follows'
import type { SearchEntity, SourceLocation } from '@/types/search'
import { CONTENT_TYPES, type ContentType } from '@/types/entryClassification'
import { LibMark, Tag } from '@/components/ui'
import EntryUpdateNotice from '@/components/EntryUpdateNotice.vue'
import { entryDetailHref } from '@/utils/bodyReference'
import { markdownToPlainText } from '@/utils/markdownPlainText'

const props = defineProps<{ article: FollowFeedEntry; libraries: LibraryCatalogItem[] }>()
const emit = defineEmits<{ 'open-article': [article: FollowFeedEntry, location?: SourceLocation]; 'filter-target': [id: string] }>()
const { t, locale } = useI18n()
const router = useRouter()
const expanded = ref(false)
const library = computed(() => props.libraries.find(item => item.id === props.article.library_id))
const libraryName = computed(() => library.value?.display_name || library.value?.name || props.article.library_name || props.article.library_id)
const contentType = computed(() => CONTENT_TYPES.includes(props.article.content_type as ContentType) ? t(`library.contentTypes.${props.article.content_type}`) : props.article.content_type)
const plainText = (value?: string) => markdownToPlainText(value, `[${t('ingestStatus.missingTypes.image')}]`)
const collectedTime = computed(() => new Date(props.article.collected_at).toLocaleTimeString(locale.value, { hour: '2-digit', minute: '2-digit' }))
const originalURL = computed(() => /^https?:\/\//i.test(props.article.original_url || '') ? props.article.original_url : '')
function entityLocation(entity: SearchEntity) {
  const query: Record<string, string> = { entity_id: entity.id }
  const libraryId = entity.library_id || props.article.library_id
  if (libraryId) query.libraries = libraryId
  return { path: '/platform/entities', query }
}
const entityHref = (entity: SearchEntity) => `/platform/entities?${new URLSearchParams(entityLocation(entity).query)}`
</script>

<template>
  <li class="feed-item" :class="{ 'feed-item-read': !article.is_new }" :data-article-id="article.entry_id">
    <span class="feed-status" :class="{ 'feed-new': article.is_new }" :role="article.is_new ? 'img' : undefined" :aria-label="article.is_new ? t('follows.unread') : undefined" :aria-hidden="article.is_new ? undefined : true" />
    <div class="feed-item-body">
      <div v-if="article.matched_follows?.length" class="feed-reasons">
        <span v-for="target in article.matched_follows" :key="`${target.target_type}:${target.target_id}`">{{ t('follows.reason', { type: t(`follows.targetTypes.${target.target_type}`) }) }} <button type="button" @click="emit('filter-target', target.target_id)">{{ target.name }}</button></span>
      </div>
      <h3 class="feed-item-title"><a :href="entryDetailHref(article.entry_id)" @click.prevent="emit('open-article', article)">{{ article.title }}</a></h3>
      <p v-if="article.summary?.overview" class="feed-overview">{{ plainText(article.summary.overview) }}</p>
      <div class="feed-meta classification-tags">
        <LibMark :color="library?.color || 8" /><span v-if="libraryName">{{ libraryName }}</span>
        <Tag v-if="contentType">{{ contentType }}</Tag>
        <Tag v-if="article.acquisition === 'model_written'" :title="t('ingestStatus.modelWrittenHint')">{{ t('ingestStatus.modelWritten') }}</Tag>
        <Tag v-if="article.cannot_be_fact || article.source === 'report'">{{ t('ingestStatus.cannotBeFact') }}</Tag>
        <Tag v-if="article.source_unverified">{{ t('review.sourceUnverified') }}</Tag>
        <EntryUpdateNotice :has-update="article.has_update" :latest-entry-id="article.latest_entry_id" />
        <time v-if="article.collected_at" :datetime="article.collected_at" :title="t('follows.collectedAt', { time: new Date(article.collected_at).toLocaleString(locale) })">{{ collectedTime }}</time>
        <code :title="article.entry_id">{{ article.entry_id.slice(0, 8) }}</code>
      </div>
      <div v-if="article.entities?.length" class="feed-entities" :aria-label="t('library.articleEntities')"><a v-for="entity in article.entities" :key="entity.id" :href="entityHref(entity)" @click.prevent="router.push(entityLocation(entity))">{{ entity.canonical_name }}</a></div>
      <details v-if="article.summary?.points?.length || article.summary?.key_terms?.length || article.snippets?.length || originalURL" class="feed-details">
        <summary>{{ t('searchPage.summaryAndLinks') }}</summary>
        <ul v-if="article.summary?.points?.length"><li v-for="(point, index) in article.summary.points" :key="index">{{ plainText(point) }}</li></ul>
        <p v-if="article.summary?.key_terms?.length">{{ article.summary.key_terms.map(plainText).join(' · ') }}</p>
        <p v-for="(snippet, index) in article.snippets" :key="snippet.chunk_id || index" class="feed-snippet"><a :href="entryDetailHref(article.entry_id, snippet.locations?.[0])" @click.prevent="emit('open-article', article, snippet.locations?.[0])">{{ plainText(snippet.content) }}</a></p>
        <a v-if="originalURL" :href="originalURL" target="_blank" rel="noopener noreferrer">{{ t('library.originalArticle') }}</a>
      </details>
      <div v-if="article.similar_reports?.length" class="feed-similar">
        <button class="similar-toggle" type="button" :aria-expanded="expanded" :aria-controls="`follow-similar-${article.entry_id}`" @click="expanded = !expanded">{{ t(expanded ? 'search.hideSimilar' : 'search.showSimilar', { count: article.similar_reports.length }, article.similar_reports.length) }}</button>
        <ul v-if="expanded" :id="`follow-similar-${article.entry_id}`" class="feed-similar-items"><FollowFeedItem v-for="similar in article.similar_reports" :key="similar.entry_id" :article="similar" :libraries="libraries" @open-article="(entry, location) => emit('open-article', entry, location)" @filter-target="emit('filter-target', $event)" /></ul>
      </div>
    </div>
  </li>
</template>

<style scoped>
.feed-item { display: grid; grid-template-columns: 10px minmax(0, 1fr); gap: var(--space-3); padding: var(--space-4) 0; border-bottom: 1px solid var(--border); min-width: 0; }
.feed-status { width: 10px; height: 10px; margin-top: 5px; background: var(--clear); }
.feed-new { background: var(--accent); }
.feed-item-body { min-width: 0; display: flex; flex-direction: column; gap: var(--space-2); overflow-wrap: anywhere; }
.feed-reasons { display: flex; flex-wrap: wrap; gap: var(--space-2) var(--space-3); font-size: var(--text-xs); font-weight: 700; }
.feed-reasons button, .similar-toggle { border: 0; padding: 0; background: transparent; color: var(--text); font: inherit; text-decoration: underline; text-underline-offset: 2px; cursor: pointer; }
.feed-item .feed-item-title { margin: 0; padding: 0; font-size: var(--text-md); line-height: 1.45; font-weight: 800; }
.feed-item-title a { color: var(--text); text-decoration: underline; text-underline-offset: 3px; }
.feed-item-read .feed-item-title a { font-weight: 600; color: var(--muted); }
.feed-item-title a:hover, .feed-reasons button:hover, .similar-toggle:hover { color: var(--accent); }
.feed-item .feed-overview, .feed-item .feed-details p { margin: 0; font-size: var(--type-note-size); color: var(--muted); }
.feed-meta { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: center; font-size: var(--text-xs); color: var(--muted); }
.feed-meta code, .feed-meta time { font: var(--text-xs)/var(--leading) var(--mono); color: var(--muted); }
.feed-meta :deep(.entry-update-notice) { color: var(--text); }
.feed-meta :deep(.entry-update-notice a:focus-visible) { outline-color: var(--accent); }
.feed-entities { display: flex; flex-wrap: wrap; gap: var(--space-1) var(--space-3); font-size: var(--text-xs); }
.feed-details, .feed-similar { font-size: var(--text-sm); }
.feed-details summary { cursor: pointer; }
.feed-details ul { padding-left: var(--space-5); }
.feed-similar-items { list-style: none; padding: 0 0 0 var(--space-3); margin: var(--space-2) 0 0; border-left: 1px solid var(--border); }
.feed-item a:focus-visible, .feed-item button:focus-visible, .feed-details summary:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
</style>
