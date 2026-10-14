<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SearchEntry } from '@/types/search'
import type { FollowFeedArticle } from '@/types/follows'
import EntryClassification from './EntryClassification.vue'
import SearchImage from './SearchImage.vue'
import { highlightText } from './GlobalCommandPalette/useHighlight'
import { markdownToPlainText } from '@/utils/markdownPlainText'

const props = withDefaults(defineProps<{ article: SearchEntry; query: string; nested?: boolean; followFeed?: boolean }>(), { nested: false, followFeed: false })
const emit = defineEmits<{ 'open-article': [article: SearchEntry]; 'open-entity': [id: string] }>()
const { t, locale } = useI18n()
const expanded = ref(false)
const plainText = (text: string | undefined) => markdownToPlainText(text, `[${t('ingestStatus.missingTypes.image')}]`)
const classification = computed(() => ({
  ...props.article,
  source: props.article.original_url,
  structured_summary: props.article.summary,
}))
const published = computed(() => props.article.published_at
  ? new Date(props.article.published_at).toLocaleDateString(locale.value)
  : t('knowledgeSearch.missingPublishedAt'))
const similarRegionID = computed(() => `similar-reports-${props.article.entry_id}`)
const feedArticle = computed(() => props.article as FollowFeedArticle)
</script>

<template>
  <article class="search-article" :class="{ nested }" :data-article-id="article.entry_id">
    <div class="article-heading">
      <button type="button" class="article-title" @click="emit('open-article', article)">{{ article.title }}</button>
      <t-tag v-if="followFeed && feedArticle.is_new" class="feed-new" size="small" theme="primary">{{ t('follows.new') }}</t-tag>
      <span v-else-if="!followFeed" class="article-score">{{ t('knowledgeSearch.score') }} {{ article.score.toLocaleString(locale, { maximumFractionDigits: 4 }) }}</span>
    </div>
    <p class="article-meta"><span v-if="article.library_name" class="article-library">{{ article.library_name }}</span><span>{{ t(`knowledgeSearch.sources.${article.source}`) }}</span><span>{{ published }}</span><code v-if="article.vault_path">{{ article.vault_path }}</code></p>
    <div v-if="followFeed" class="feed-attribution"><time :datetime="feedArticle.collected_at">{{ t('follows.collectedAt', { time: new Date(feedArticle.collected_at).toLocaleString(locale) }) }}</time><div class="feed-matches"><strong>{{ t('follows.matchedTargets') }}</strong><t-tag v-for="target in feedArticle.matched_follows" :key="`${target.target_type}:${target.target_id}`" size="small" variant="light-outline">{{ target.name }} ({{ t(`follows.targetTypes.${target.target_type}`) }})</t-tag></div></div>
    <EntryClassification v-if="article.source === 'article' || article.source === 'mine'" :entry="classification" compact @open-entity="emit('open-entity', $event)" />
    <div v-if="article.snippets?.length" class="article-snippets">
      <strong>{{ t('knowledgeSearch.snippets') }}</strong>
      <div v-for="(snippet, index) in article.snippets.slice(0, 3)" :key="snippet.chunk_id || snippet.image_path || index" class="article-snippet">
        <SearchImage v-if="snippet.image_path && article.source === 'article'" :article-id="article.entry_id" :path="snippet.image_path" :title="article.title" />
        <span v-if="snippet.chunk_type === 'image_description'">{{ t('articleJudgments.imageDescriptions') }}</span>
        <p v-if="snippet.content" v-html="highlightText(plainText(snippet.content), query)" />
      </div>
    </div>
    <template v-if="!nested && article.similar_reports?.length">
      <button type="button" class="similar-toggle" :aria-expanded="expanded" :aria-controls="similarRegionID" @click="expanded = !expanded">
        <t-icon :name="expanded ? 'chevron-up' : 'chevron-down'" />
        {{ t(expanded ? 'knowledgeSearch.hideSimilar' : 'knowledgeSearch.showSimilar', { count: article.similar_reports.length }) }}
      </button>
      <div v-if="expanded" :id="similarRegionID" class="similar-reports">
        <SearchEntry v-for="similar in article.similar_reports" :key="similar.entry_id" :article="similar" :query="query" :follow-feed="followFeed" nested @open-article="emit('open-article', $event)" @open-entity="emit('open-entity', $event)" />
      </div>
    </template>
  </article>
</template>

<style scoped lang="less">
.search-article { padding: 20px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); min-width: 0; overflow-wrap: anywhere; }
.article-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.article-title { min-width: 0; padding: 0; border: 0; background: transparent; color: var(--td-text-color-primary); font: inherit; font-size: 16px; font-weight: 600; text-align: left; line-height: 1.6; cursor: pointer; overflow-wrap: anywhere; }
.article-title:hover { color: var(--td-brand-color); }
.article-score { flex-shrink: 0; color: var(--td-text-color-secondary); font-size: 12px; line-height: 24px; }
.article-meta { display: flex; flex-wrap: wrap; gap: 6px 14px; color: var(--td-text-color-secondary); font-size: 12px; }
.article-meta code { white-space: normal; }
.feed-attribution { display: grid; gap: 6px; margin-block: 12px; color: var(--td-text-color-secondary); font-size: 12px; }
.feed-matches { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; }
.feed-matches strong { font-weight: 500; }
.feed-new { flex-shrink: 0; }
.article-snippets { display: grid; gap: 8px; margin-top: 14px; color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.7; }
.article-snippets strong { color: var(--td-text-color-primary); font-weight: 500; }
.article-snippets p { margin: 0; white-space: pre-wrap; }
.article-snippet { display: grid; gap: 8px; }
.article-snippets :deep(.search-highlight) { color: var(--td-brand-color); background: var(--td-brand-color-light); border-radius: var(--td-radius-small); }
.similar-toggle { display: inline-flex; gap: 6px; align-items: center; border: 0; border-radius: var(--td-radius-small); background: transparent; color: var(--td-brand-color); padding: 6px 0; margin-top: 12px; font: inherit; font-size: 13px; cursor: pointer; }
.similar-toggle:hover { text-decoration: underline; }
button:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
.similar-reports { display: grid; gap: 12px; margin-top: 10px; }
.nested { background: var(--td-bg-color-secondarycontainer); }
@media (max-width: 560px) { .search-article { padding: 14px; } .article-heading { flex-direction: column; gap: 4px; } }
</style>
