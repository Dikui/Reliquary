<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SourceLocation, SearchEntry } from '@/types/search'
import EntryUpdateNotice from './EntryUpdateNotice.vue'
import EntryClassification from './EntryClassification.vue'
import SearchImage from './SearchImage.vue'
import { highlightText } from './GlobalCommandPalette/useHighlight'
import { markdownToPlainText } from '@/utils/markdownPlainText'

const props = defineProps<{ article: SearchEntry; query: string }>()
const emit = defineEmits<{ 'open-article': [article: SearchEntry, location?: SourceLocation]; 'open-entity': [id: string] }>()
const { t, locale } = useI18n()
const plainText = (text: string | undefined) => markdownToPlainText(text, `[${t('ingestStatus.missingTypes.image')}]`)
const classification = computed(() => ({
  ...props.article,
  source: props.article.original_url,
  source_kind: props.article.source,
  structured_summary: props.article.summary,
}))
const published = computed(() => props.article.published_at
  ? new Date(props.article.published_at).toLocaleDateString(locale.value, { timeZone: 'UTC' })
  : t('knowledgeSearch.missingPublishedAt'))
</script>

<template>
  <article class="search-article" :data-article-id="article.entry_id">
    <div class="article-heading">
      <button type="button" class="article-title" @click="emit('open-article', article)">{{ article.title }}</button>
      <span class="article-score">{{ t('knowledgeSearch.score') }} {{ article.score.toLocaleString(locale, { maximumFractionDigits: 4 }) }}</span>
    </div>
    <p class="article-meta"><span v-if="article.library_name" class="article-library">{{ article.library_name }}</span><span>{{ t(`knowledgeSearch.sources.${article.source}`) }}</span><span>{{ published }}</span><code v-if="article.vault_path">{{ article.vault_path }}</code></p>
    <EntryClassification :entry="classification" compact @open-entity="emit('open-entity', $event)" />
    <EntryUpdateNotice :has-update="article.has_update" :latest-entry-id="article.latest_entry_id" />
    <div v-if="article.snippets?.length" class="article-snippets">
      <strong>{{ t('knowledgeSearch.snippets') }}</strong>
      <div v-for="(snippet, index) in article.snippets.slice(0, 3)" :key="snippet.chunk_id || snippet.image_path || index" class="article-snippet">
        <SearchImage v-if="snippet.image_path && article.source === 'article'" :article-id="article.entry_id" :path="snippet.image_path" :title="article.title" />
        <span v-if="snippet.chunk_type === 'image_description'">{{ t('articleJudgments.imageDescriptions') }}</span>
        <button v-if="snippet.content" type="button" class="snippet-reference" @click="emit('open-article', article, snippet.locations?.[0])"><span v-html="highlightText(plainText(snippet.content), query)" /></button>
      </div>
    </div>

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
.snippet-reference { border: 0; padding: 0; background: transparent; color: inherit; font: inherit; text-align: left; cursor: pointer; }
.snippet-reference:focus-visible { outline: 2px solid var(--td-brand-color); }
.snippet-reference:hover { color: var(--td-brand-color); }
.article-snippets p { margin: 0; white-space: pre-wrap; }
.article-snippet { display: grid; gap: 8px; }
.article-snippets :deep(.search-highlight) { color: var(--td-brand-color); background: var(--td-brand-color-light); border-radius: var(--td-radius-small); }
button:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
@media (max-width: 560px) { .search-article { padding: 14px; } .article-heading { flex-direction: column; gap: 4px; } }
</style>
