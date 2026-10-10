<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { KnowledgeSearchArticle } from '@/types/knowledgeSearch'
import KnowledgeClassification from './KnowledgeClassification.vue'
import { highlightText } from './GlobalCommandPalette/useHighlight'

const props = withDefaults(defineProps<{ article: KnowledgeSearchArticle; query: string; nested?: boolean }>(), { nested: false })
const emit = defineEmits<{ 'open-article': [article: KnowledgeSearchArticle]; 'open-entity': [id: string] }>()
const { t, locale } = useI18n()
const expanded = ref(false)
const classification = computed(() => ({ ...props.article, source: props.article.original_url, structured_summary: props.article.summary }))
const published = computed(() => props.article.published_at
  ? new Date(props.article.published_at).toLocaleDateString(locale.value)
  : t('knowledgeSearch.missingPublishedAt'))
const similarRegionID = computed(() => `similar-reports-${props.article.article_id}`)
</script>

<template>
  <article class="search-article" :class="{ nested }" :data-article-id="article.article_id">
    <div class="article-heading">
      <button type="button" class="article-title" @click="emit('open-article', article)">{{ article.title }}</button>
      <span class="article-score">{{ t('knowledgeSearch.score') }} {{ article.score.toLocaleString(locale, { maximumFractionDigits: 4 }) }}</span>
    </div>
    <p class="article-meta"><span>{{ t(`knowledgeSearch.sources.${article.source}`) }}</span><span>{{ published }}</span><code v-if="article.vault_path">{{ article.vault_path }}</code></p>
    <KnowledgeClassification v-if="article.source === 'article'" :knowledge="classification" compact @open-entity="emit('open-entity', $event)" />
    <div v-if="article.snippets?.length" class="article-snippets">
      <strong>{{ t('knowledgeSearch.snippets') }}</strong>
      <p v-for="(snippet, index) in article.snippets.slice(0, 3)" :key="snippet.chunk_id || index" v-html="highlightText(snippet.content, query)" />
    </div>
    <template v-if="!nested && article.similar_reports?.length">
      <button type="button" class="similar-toggle" :aria-expanded="expanded" :aria-controls="similarRegionID" @click="expanded = !expanded">
        <t-icon :name="expanded ? 'chevron-up' : 'chevron-down'" />
        {{ t(expanded ? 'knowledgeSearch.hideSimilar' : 'knowledgeSearch.showSimilar', { count: article.similar_reports.length }) }}
      </button>
      <div v-if="expanded" :id="similarRegionID" class="similar-reports">
        <KnowledgeSearchArticle v-for="similar in article.similar_reports" :key="similar.article_id" :article="similar" :query="query" nested @open-article="emit('open-article', $event)" @open-entity="emit('open-entity', $event)" />
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
.article-snippets { display: grid; gap: 8px; margin-top: 14px; color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.7; }
.article-snippets strong { color: var(--td-text-color-primary); font-weight: 500; }
.article-snippets p { margin: 0; white-space: pre-wrap; }
.article-snippets :deep(.search-highlight) { color: var(--td-brand-color); background: var(--td-brand-color-light); border-radius: var(--td-radius-small); }
.similar-toggle { display: inline-flex; gap: 6px; align-items: center; border: 0; border-radius: var(--td-radius-small); background: transparent; color: var(--td-brand-color); padding: 6px 0; margin-top: 12px; font: inherit; font-size: 13px; cursor: pointer; }
.similar-toggle:hover { text-decoration: underline; }
button:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
.similar-reports { display: grid; gap: 12px; margin-top: 10px; }
.nested { background: var(--td-bg-color-secondarycontainer); }
@media (max-width: 560px) { .search-article { padding: 14px; } .article-heading { flex-direction: column; gap: 4px; } }
</style>
