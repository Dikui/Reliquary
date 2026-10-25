<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { type ClassifiedEntry } from '@/types/entryClassification'
import { isValidURL } from '@/utils/security'
import { markdownToPlainText } from '@/utils/markdownPlainText'
import FollowToggle from './FollowToggle.vue'

const props = withDefaults(defineProps<{ entry: ClassifiedEntry; compact?: boolean; followable?: boolean; hideRelations?: boolean }>(), { compact: false, followable: false, hideRelations: false })
const emit = defineEmits<{ 'open-entity': [id: string] }>()
const { t } = useI18n()
const plainText = (value: string) => markdownToPlainText(value, `[${t('ingestStatus.missingTypes.image')}]`)
const originalURL = computed(() => {
  const url = props.entry.metadata?.intake?.source_info?.original_url || props.entry.source || ''
  return /^https?:\/\//i.test(url) && isValidURL(url) ? url : ''
})
</script>

<template>

  <div class="entry-classification" :class="{ compact }">
    <div class="classification-tags">
      <t-tag v-if="entry.library_name" size="small" variant="light-outline">{{ entry.library_name }}</t-tag>
      <t-tag v-if="entry.cannot_be_fact || entry.source_kind === 'report'" size="small" theme="warning" variant="light-outline">{{ t('ingestStatus.cannotBeFact') }}</t-tag>
      <t-tag v-if="entry.source_unverified" size="small" theme="warning" variant="light-outline">{{ t('knowledgeReview.sourceUnverified') }}</t-tag>
      <t-tag v-if="entry.acquisition === 'model_written'" size="small" variant="light-outline" :title="t('ingestStatus.modelWrittenHint')">{{ t('ingestStatus.modelWritten') }}</t-tag>
    </div>
    <a v-if="originalURL" class="original-link" :href="originalURL" target="_blank" rel="noopener noreferrer" @click.stop>{{ t('library.originalArticle') }}: {{ originalURL }}</a>
    <div v-if="entry.structured_summary" class="structured-summary">
      <div class="summary-overview"><strong>{{ t('library.summaryOverview') }}</strong><p>{{ plainText(entry.structured_summary.overview) }}</p></div>
    </div>
    <p v-else-if="entry.description" class="summary-description">{{ plainText(entry.description) }}</p>
    <div v-if="entry.domains?.length" class="classification-relations"><strong>{{ t('domains.title') }}</strong><t-tag v-for="domain in entry.domains" :key="domain.id" size="small" variant="light-outline" :title="domain.definition">{{ domain.name }}</t-tag></div>
    <div v-if="!hideRelations && entry.entities?.length" class="classification-relations entity-relations"><strong>{{ t('library.articleEntities') }}</strong><span v-for="entity in entry.entities" :key="entity.id" class="relation-target"><button type="button" @click.stop="emit('open-entity', entity.id)">{{ entity.canonical_name }}</button><FollowToggle v-if="followable" target-type="entity" :target-id="entity.id" :name="entity.canonical_name" compact /></span></div>
  </div>
</template>

<style scoped lang="less">
.entry-classification { min-width: 0; font-size: 13px; line-height: 1.65; color: var(--td-text-color-secondary); overflow-wrap: anywhere; }
.classification-tags, .classification-relations { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin: 8px 0; }
.structured-summary { display: grid; gap: 10px; margin: 14px 0; }
strong { color: var(--td-text-color-primary); font-weight: 600; }
p { margin: 4px 0 0; }
.entity-relations button { border: 0; border-radius: var(--td-radius-small); background: var(--td-bg-color-secondarycontainer); color: var(--td-brand-color); padding: 2px 6px; font: inherit; cursor: pointer; }
.entity-relations button:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.entity-relations button:hover, .original-link:hover { text-decoration: underline; }
.relation-target { display: inline-flex; align-items: center; flex-wrap: wrap; gap: 4px; min-width: 0; }
.original-link { display: block; color: var(--td-brand-color); }
.compact { font-size: 11px; line-height: 1.5; }
.compact .structured-summary { gap: 6px; margin: 8px 0; }
</style>
