<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LibraryCatalogItem } from '@/api/library'
import type { SearchEntry, SourceLocation } from '@/types/search'
import { CONTENT_TYPES, type ContentType } from '@/types/entryClassification'
import { LibMark, Tag } from '@/components/ui'
import SearchImage from '@/components/SearchImage.vue'
import EntryUpdateNotice from '@/components/EntryUpdateNotice.vue'
import { highlightText } from '@/components/GlobalCommandPalette/useHighlight'
import { markdownToPlainText } from '@/utils/markdownPlainText'
import { isValidURL } from '@/utils/security'
import { entryDetailHref } from '@/utils/bodyReference'

const props = withDefaults(defineProps<{ article: SearchEntry; query: string; libraries: LibraryCatalogItem[]; nested?: boolean }>(), { nested: false })
const emit = defineEmits<{ 'open-article': [article: SearchEntry, location?: SourceLocation]; 'open-entity': [id: string] }>()
const { t, locale } = useI18n()
const expanded = ref(false)
const library = computed(() => props.libraries.find(item => item.id === props.article.library_id))
const libraryName = computed(() => library.value?.display_name || library.value?.name || props.article.library_name || props.article.library_id)
const firstLocation = computed(() => props.article.snippets?.flatMap(item => item.locations || [])[0])
const contentType = computed(() => CONTENT_TYPES.includes(props.article.content_type as ContentType) ? t(`library.contentTypes.${props.article.content_type}`) : props.article.content_type)
const originalURL = computed(() => /^https?:\/\//i.test(props.article.original_url || '') && isValidURL(props.article.original_url) ? props.article.original_url : '')
const plainText = (text: string) => markdownToPlainText(text, `[${t('ingestStatus.missingTypes.image')}]`)
const similarRegionID = computed(() => `similar-reports-${props.article.entry_id}`)
</script>

<template>
  <article class="search-result" :class="{ nested }" :data-article-id="article.entry_id">
    <LibMark class="result-mark" :color="library?.color || 8" :label="libraryName" />
    <h2 class="result-title"><a :href="entryDetailHref(article.entry_id, firstLocation)" @click.prevent="emit('open-article', article, firstLocation)">{{ article.title }}</a></h2>
    <span class="result-score" :aria-label="t('knowledgeSearch.score')">{{ article.score.toLocaleString(locale, { maximumFractionDigits: 4 }) }}</span>
    <div v-if="article.snippets?.length" class="result-snippets">
      <div v-for="(snippet, index) in article.snippets.slice(0, 3)" :key="snippet.chunk_id || snippet.image_path || index" class="result-snippet">
        <SearchImage v-if="snippet.image_path && article.source === 'article'" :article-id="article.entry_id" :path="snippet.image_path" :title="article.title" swiss />
        <span v-if="snippet.chunk_type === 'image_description'">{{ t('articleJudgments.imageDescriptions') }}</span>
        <a v-if="snippet.content" class="snippet-reference" :href="entryDetailHref(article.entry_id, snippet.locations?.[0])" @click.prevent="emit('open-article', article, snippet.locations?.[0])"><span v-html="highlightText(plainText(snippet.content), query)" /></a>
        <div v-if="snippet.locations?.length" class="snippet-positions"><a v-for="(location, locationIndex) in snippet.locations" :key="locationIndex" :href="entryDetailHref(article.entry_id, location)" :title="location.heading_path?.join(' / ')" @click.prevent="emit('open-article', article, location)">{{ t('searchPage.hitParagraph', { paragraph: location.paragraph }) }}</a></div>
      </div>
    </div>
    <div class="result-meta">
      <Tag v-if="contentType">{{ contentType }}</Tag><span class="article-library">{{ libraryName }}</span><span v-if="library">{{ t('entries.libraryNumber', { number: String(libraries.indexOf(library) + 1).padStart(2, '0') }) }}</span>
      <time v-if="article.published_at" :datetime="article.published_at">{{ new Date(article.published_at).toLocaleDateString(locale) }}</time><span v-else>{{ t('knowledgeSearch.missingPublishedAt') }}</span>
      <code>{{ article.entry_id }}</code><span>{{ t(`knowledgeSearch.sources.${article.source}`) }}</span>
      <Tag v-if="article.cannot_be_fact || article.source === 'report'">{{ t('ingestStatus.cannotBeFact') }}</Tag>
      <Tag v-if="article.source_unverified">{{ t('knowledgeReview.sourceUnverified') }}</Tag>
      <Tag v-if="article.acquisition === 'model_written'" :title="t('ingestStatus.modelWrittenHint')">{{ t('ingestStatus.modelWritten') }}</Tag>
      <EntryUpdateNotice :has-update="article.has_update" :latest-entry-id="article.latest_entry_id" />
    </div>
    <details v-if="article.summary || article.entities?.length || article.topics?.length || originalURL || article.vault_path" class="result-details">
      <summary>{{ t('searchPage.summaryAndLinks') }}</summary>
      <div v-if="article.summary" class="result-summary"><p>{{ plainText(article.summary.overview) }}</p><ul><li v-for="(point, index) in article.summary.points" :key="index">{{ plainText(point) }}</li></ul><p>{{ article.summary.key_terms.map(plainText).join(' · ') }}</p></div>
      <div v-if="article.entities?.length" class="result-relations"><strong>{{ t('library.articleEntities') }}</strong><button v-for="entity in article.entities" :key="entity.id" type="button" @click="emit('open-entity', entity.id)">{{ entity.canonical_name }}</button></div>
      <div v-if="article.topics?.length" class="result-relations"><strong>{{ t('library.articleTopics') }}</strong><span v-for="topic in article.topics" :key="topic.id">{{ topic.name }}</span></div>
      <a v-if="originalURL" :href="originalURL" target="_blank" rel="noopener noreferrer">{{ t('library.originalArticle') }}: {{ originalURL }}</a>
      <code v-if="article.vault_path" class="result-path">{{ article.vault_path }}</code>
    </details>
    <div v-if="!nested && article.similar_reports?.length" class="result-similar">
      <button type="button" :aria-expanded="expanded" :aria-controls="similarRegionID" @click="expanded = !expanded">{{ t(expanded ? 'knowledgeSearch.hideSimilar' : 'knowledgeSearch.showSimilar', { count: article.similar_reports.length }, article.similar_reports.length) }}</button>
      <div v-if="expanded" :id="similarRegionID"><SearchResult v-for="similar in article.similar_reports" :key="similar.entry_id" :article="similar" :query="query" :libraries="libraries" nested @open-article="(entry, location) => emit('open-article', entry, location)" @open-entity="emit('open-entity', $event)" /></div>
    </div>
  </article>
</template>

<style scoped>
.search-result { display: grid; grid-template-columns: 12px minmax(0, 1fr) auto; gap: var(--space-2) var(--space-3); padding: var(--space-4) 0; border-bottom: 1px solid var(--border); min-width: 0; overflow-wrap: anywhere; }
.result-mark { margin-top: .4rem; }
.result-title { margin: 0; font-size: var(--text-lg); font-weight: 800; line-height: 1.4; letter-spacing: -.01em; }
.result-title a { text-decoration: none; }
.result-title a:hover { text-decoration: underline; }
.result-score { font-family: var(--mono); font-size: var(--text-sm); font-weight: 700; }
.result-snippets, .result-meta, .result-details, .result-similar { grid-column: 2 / 4; min-width: 0; }
.result-snippets { display: grid; gap: var(--space-3); max-width: 46em; line-height: var(--leading); }
.result-snippet { display: grid; gap: var(--space-2); }
.search-result a { color: var(--text); text-underline-offset: .18em; }
.search-result a:hover { color: var(--accent); }
.snippet-reference { text-decoration: none; }
.snippet-reference:hover { text-decoration: underline; }
.snippet-positions, .result-meta { display: flex; flex-wrap: wrap; gap: var(--space-2); font-size: var(--text-xs); color: var(--muted); align-items: center; }
.result-meta code, .result-meta time, .result-path { font-family: var(--mono); }
.result-details { font-size: var(--text-sm); line-height: var(--leading); }
.result-details summary { cursor: pointer; color: var(--muted); }
.result-summary { margin-block: var(--space-3); }
.result-summary p, .result-summary ul { margin-block: var(--space-2); }
.result-relations { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: center; margin-block: var(--space-2); }
.result-path { display: block; margin-top: var(--space-2); font-size: var(--text-xs); color: var(--muted); }
.result-similar button, .result-relations button { padding: 0; border: 0; background: var(--clear); color: var(--text); font: inherit; text-decoration: underline; cursor: pointer; }
.result-similar button:hover, .result-relations button:hover { color: var(--accent); }
.result-similar { font-size: var(--text-sm); }
.search-result :deep(.search-highlight) { background: var(--select); color: var(--on-select); font-weight: 700; border-radius: 0; }
.search-result:focus-within:not(:has(.search-result:focus-within)) { background: var(--select); color: var(--on-select); }
.search-result:focus-within:not(:has(.search-result:focus-within)) a, .search-result:focus-within:not(:has(.search-result:focus-within)) button, .search-result:focus-within:not(:has(.search-result:focus-within)) summary, .search-result:focus-within:not(:has(.search-result:focus-within)) .result-meta, .search-result:focus-within:not(:has(.search-result:focus-within)) .result-path { color: var(--on-select); }
.search-result:focus-within:not(:has(.search-result:focus-within)) :deep(.search-highlight) { background: var(--select); color: var(--on-select); }
.search-result:focus-within:not(:has(.search-result:focus-within)) :deep(.rq-tag) { background: var(--clear); color: var(--on-select); }
.search-result :deep(.entry-update-notice), .search-result :deep(.entry-update-notice a) { color: inherit; }
.search-result :deep(.entry-update-notice a:focus-visible), .search-result a:focus-visible, .search-result button:focus-visible, .search-result summary:focus-visible { outline: 2px solid currentColor; outline-offset: 2px; }
.nested { border-top: 1px solid var(--border); border-bottom: 0; margin-top: var(--space-3); }
@media (max-width: 560px) { .result-title { font-size: var(--text-md); } }
</style>
