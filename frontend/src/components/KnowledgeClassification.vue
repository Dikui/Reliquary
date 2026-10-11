<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { CONTENT_TYPES, type ContentType, type ClassifiedKnowledge } from '@/types/knowledgeClassification'
import { isValidURL } from '@/utils/security'

const props = withDefaults(defineProps<{ knowledge: ClassifiedKnowledge; compact?: boolean }>(), { compact: false })
const emit = defineEmits<{ 'open-entity': [id: string] }>()
const { t } = useI18n()
const type = computed(() => CONTENT_TYPES.includes(props.knowledge.content_type as ContentType) ? props.knowledge.content_type : 'unclassified')
const originalURL = computed(() => {
  const url = props.knowledge.metadata?.intake?.source_info?.original_url || props.knowledge.source || ''
  return /^https?:\/\//i.test(url) && isValidURL(url) ? url : ''
})
</script>

<template>
  <div class="knowledge-classification" :class="{ compact }">
    <div class="classification-tags">
      <t-tag size="small" variant="light-outline">{{ t(`knowledgeBase.contentTypes.${type}`) }}</t-tag>
      <t-tag size="small" variant="light-outline">{{ t(knowledge.processing_mode === 'summary_only' ? 'knowledgeBase.processingModes.summary_only' : 'knowledgeBase.processingModes.full') }}</t-tag>
      <t-tag v-if="knowledge.source_unverified" size="small" theme="warning" variant="light-outline">{{ t('knowledgeReview.sourceUnverified') }}</t-tag>
    </div>
    <div v-if="knowledge.structured_summary" class="structured-summary">
      <div class="summary-overview"><strong>{{ t('knowledgeBase.summaryOverview') }}</strong><p>{{ knowledge.structured_summary.overview }}</p></div>
      <div class="summary-points"><strong>{{ t('knowledgeBase.summaryPoints') }}</strong><ul><li v-for="(point, index) in knowledge.structured_summary.points" :key="index">{{ point }}</li></ul></div>
      <div class="summary-key-terms"><strong>{{ t('knowledgeBase.summaryKeyTerms') }}</strong><span>{{ knowledge.structured_summary.key_terms.join(' · ') }}</span></div>
    </div>
    <p v-else-if="knowledge.description" class="summary-description">{{ knowledge.description }}</p>
    <div v-if="knowledge.entities?.length" class="classification-relations entity-relations"><strong>{{ t('knowledgeBase.articleEntities') }}</strong><button v-for="entity in knowledge.entities" :key="entity.id" type="button" @click.stop="emit('open-entity', entity.id)">{{ entity.canonical_name }}</button></div>
    <div v-if="knowledge.topics?.length" class="classification-relations topic-relations"><strong>{{ t('knowledgeBase.articleTopics') }}</strong><span v-for="topic in knowledge.topics" :key="topic.id" :title="topic.description">{{ topic.name }}</span></div>
    <a v-if="knowledge.processing_mode === 'summary_only' && originalURL" class="original-link" :href="originalURL" target="_blank" rel="noopener noreferrer" @click.stop>{{ t('knowledgeBase.originalArticle') }}: {{ originalURL }}</a>
    <p v-else-if="knowledge.processing_mode === 'summary_only'" class="missing-link">{{ t('knowledgeBase.originalLinkMissing') }}</p>
  </div>
</template>

<style scoped lang="less">
.knowledge-classification { min-width: 0; font-size: 13px; line-height: 1.65; color: var(--td-text-color-secondary); overflow-wrap: anywhere; }
.classification-tags, .classification-relations { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin: 8px 0; }
.structured-summary { display: grid; gap: 10px; margin: 14px 0; }
strong { color: var(--td-text-color-primary); font-weight: 600; }
p, ul { margin: 4px 0 0; }
ul { padding-left: 20px; }
.summary-key-terms { display: flex; flex-wrap: wrap; gap: 6px; }
.entity-relations button { border: 0; border-radius: var(--td-radius-small); background: var(--td-bg-color-secondarycontainer); color: var(--td-brand-color); padding: 2px 6px; font: inherit; cursor: pointer; }
.entity-relations button:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.entity-relations button:hover, .original-link:hover { text-decoration: underline; }
.topic-relations span { padding: 2px 6px; background: var(--td-bg-color-secondarycontainer); border-radius: var(--td-radius-small); }
.original-link { display: block; color: var(--td-brand-color); }
.compact { font-size: 11px; line-height: 1.5; }
.compact .structured-summary { gap: 6px; margin: 8px 0; }
</style>
