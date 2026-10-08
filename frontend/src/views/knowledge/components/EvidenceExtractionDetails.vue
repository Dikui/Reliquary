<template>
  <div class="extraction-details">
    <p v-if="evidence.extraction_mode" class="extraction-mode" role="status">{{ t('knowledgeWorkbench.evidence.extractionMode') }}: {{ t(`knowledgeWorkbench.evidence.extractionModes.${evidence.extraction_mode}`) }}</p>
    <p v-if="!compact && evidence.extraction_mode === 'template_only'" class="extraction-partial-hint">{{ t('knowledgeWorkbench.evidence.templateOnlyHint') }}</p>
    <div v-if="shouldShowExtractionCoverage(evidence)" class="extraction-coverage" role="status"><t-tag v-if="evidence.extraction_coverage === 'partial'" theme="warning" variant="light" size="small">{{ t('extractionDetails.partial') }}</t-tag><span v-else>{{ t('extractionDetails.complete') }}</span><span>{{ t('extractionDetails.segments', { processed: evidence.processed_segments || 0, total: evidence.total_segments || 0 }) }}</span></div>
    <p v-if="evidence.extraction_coverage === 'partial' && !compact" class="extraction-partial-hint">{{ t('extractionDetails.partialHint') }}</p>
    <t-alert v-if="!compact && evidence.extraction_error" theme="error" :message="evidence.extraction_error" role="alert" />
    <ModelCallSummary v-if="!compact" :calls="calls" />
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { shouldShowExtractionCoverage } from '@/utils/extractionCoverage'
import ModelCallSummary from '@/components/ModelCallSummary.vue'
import { useI18n } from 'vue-i18n'
import type { KnowledgeEvidence } from '@/api/knowledge-context'
const props = withDefaults(defineProps<{ evidence: KnowledgeEvidence; compact?: boolean }>(), { compact: false })
const { t } = useI18n()
const calls = computed(() => Array.isArray(props.evidence.model_calls) ? props.evidence.model_calls : [])
</script>
<style scoped lang="less">
.extraction-details { font-size: 12px; margin: 10px 0; color: var(--td-text-color-secondary); }
.extraction-coverage { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.extraction-mode { margin: 0 0 8px; }
.extraction-partial-hint, .extraction-no-calls { line-height: 1.6; }
</style>
