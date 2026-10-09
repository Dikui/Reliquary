<template>
  <t-drawer :visible="visible" :zIndex="1900" :header="t('knowledgeBase.strategy.title')" size="600px" :footer="false"
    @close="emit('update:visible', false)">
    <div class="strategy-drawer">
      <p>{{ t('knowledgeBase.strategy.hint') }}</p>
      <t-textarea v-model.trim="instruction" :placeholder="t('knowledgeBase.strategy.instruction')" :maxlength="2000" />
      <t-input v-model.trim="sampleText" :placeholder="t('knowledgeBase.strategy.samples')" />
      <t-button :loading="saving" :disabled="!instruction || !sampleIDs.length || sampleIDs.length > 3" @click="propose">
        {{ t('knowledgeBase.strategy.trial') }}
      </t-button>
      <section class="strategy-evaluation-input">
        <h3>{{ t('knowledgeStrategy.evaluation.title') }}</h3>
        <p>{{ t('knowledgeStrategy.evaluation.intro') }}</p>
        <t-textarea v-model="evaluationText" :placeholder="t('knowledgeStrategy.evaluation.format')" :autosize="{ minRows: 4, maxRows: 8 }" />
        <p v-if="evaluationInputError" class="strategy-evaluation__error">{{ evaluationInputError }}</p>
      </section>
      <div class="strategy-reprocess">
        <h3>{{ t('knowledgeBase.strategy.reprocess') }}</h3>
        <p>{{ t('knowledgeBase.strategy.reprocessHint') }}</p>
        <t-radio-group v-model="scopeMode" :options="scopeOptions" />
        <t-input v-if="scopeMode === 'messages'" v-model.trim="reprocessIDsText" :placeholder="t('knowledgeBase.strategy.samples')" />
        <t-select v-else v-model="tagID" filterable :options="tagOptions" :placeholder="t('knowledgeBase.strategy.selectTag')" />
        <t-button :loading="reprocessing" :disabled="scopeMode === 'messages' ? !reprocessIDs.length : !tagID" @click="reprocess">
          {{ t('knowledgeBase.strategy.runReprocess') }}
        </t-button>
        <p v-if="lastResult">{{ formatCountCopy(t, 'knowledgeBase.strategy.reprocessResult', { flagged: lastResult.flagged, conflicts: lastResult.conflicts }, lastResult) }}</p>
        <p v-for="error in lastResult?.errors || []" :key="error">{{ error }}</p>
      </div>
      <t-loading :loading="loading" size="small">
        <t-empty v-if="!loading && !versions.length" :description="t('knowledgeBase.strategy.empty')" />
        <article v-for="version in versions" :key="version.id" class="strategy-version">
          <div class="strategy-version__head">
            <strong>{{ t(`knowledgeBase.strategy.status.${version.status}`) }}</strong>
            <span>{{ new Date(version.created_at).toLocaleString() }}</span>
          </div>
          <p>{{ version.reason }}</p>
          <div class="strategy-rule">
            <strong>{{ t('knowledgeBase.strategy.filter') }}</strong>
            <p>{{ t('knowledgeBase.strategy.oldFilter') }}: {{ version.previous_rules?.filter || t('knowledgeBase.strategy.default') }}</p>
            <p>{{ t('knowledgeBase.strategy.newFilter') }}: {{ version.rules.filter || t('knowledgeBase.strategy.default') }}</p>
            <strong>{{ t('knowledgeBase.strategy.tags') }}</strong>
            <p>{{ t('knowledgeBase.strategy.oldTags') }}: {{ version.previous_rules?.tags || t('knowledgeBase.strategy.default') }}</p>
            <p>{{ t('knowledgeBase.strategy.newTags') }}: {{ version.rules.tags || t('knowledgeBase.strategy.default') }}</p>
            <strong>{{ t('knowledgeBase.strategy.collection') }}</strong>
            <p>{{ t('knowledgeBase.strategy.oldCollection') }}: {{ version.previous_rules?.collection || t('knowledgeBase.strategy.default') }}</p>
            <p>{{ t('knowledgeBase.strategy.newCollection') }}: {{ version.rules.collection || t('knowledgeBase.strategy.default') }}</p>
            <strong>{{ t('knowledgeBase.strategy.research') }}</strong>
            <p>{{ t('knowledgeBase.strategy.oldResearch') }}: {{ version.previous_rules?.research || t('knowledgeBase.strategy.default') }}</p>
            <p>{{ t('knowledgeBase.strategy.newResearch') }}: {{ version.rules.research || t('knowledgeBase.strategy.default') }}</p>
          </div>
          <details v-if="version.sample_results?.length">
            <summary>{{ t('knowledgeBase.strategy.comparison') }}</summary>
            <div v-for="sample in version.sample_results" :key="sample.knowledge_id" class="strategy-sample">
              <strong>{{ sample.knowledge_id }}</strong>
              <div>{{ t('knowledgeBase.strategy.oldFilter') }}: {{ sample.old_filter }}</div>
              <div>{{ t('knowledgeBase.strategy.newFilter') }}: {{ sample.new_filter }}</div>
              <div>{{ t('knowledgeBase.strategy.oldTags') }}: {{ sample.old_tags }}</div>
              <div>{{ t('knowledgeBase.strategy.newTags') }}: {{ sample.new_tags }}</div>
              <div>{{ t('knowledgeBase.strategy.oldCollection') }}: {{ sample.old_collection }}</div>
              <div>{{ t('knowledgeBase.strategy.newCollection') }}: {{ sample.new_collection }}</div>
              <div>{{ t('knowledgeBase.strategy.oldResearch') }}: {{ sample.old_research }}</div>
              <div>{{ t('knowledgeBase.strategy.newResearch') }}: {{ sample.new_research }}</div>
            </div>
          </details>
          <t-button size="small" variant="outline" :loading="evaluating === version.id"
            :disabled="evaluating !== '' || !evaluationText.trim()" @click="evaluate(version)">
            {{ t('knowledgeStrategy.evaluation.run') }}
          </t-button>
          <p v-if="evaluationErrors[version.id]" class="strategy-evaluation__error">{{ evaluationErrors[version.id] }}</p>
          <section v-if="evaluationResults[version.id]" class="strategy-evaluation-result">
            <h4>{{ t('knowledgeStrategy.evaluation.result') }}</h4>
            <div v-for="part in evaluationParts(version.id)" :key="part.key" class="strategy-evaluation-split">
              <strong>{{ part.title }} · {{ t('knowledgeStrategy.evaluation.countedSamples', { count: part.result.samples }) }}</strong>
              <div class="strategy-evaluation-compare">
                <div v-for="column in [{ key: 'old', value: part.result.old }, { key: 'new', value: part.result.new }]" :key="column.key" class="strategy-evaluation-model">
                  <strong>{{ t(`knowledgeStrategy.evaluation.${column.key}`) }}</strong>
                  <div class="strategy-evaluation-counts">
                    <span>{{ t('knowledgeStrategy.evaluation.tp') }} {{ column.value.tp }}</span><span>{{ t('knowledgeStrategy.evaluation.fp') }} {{ column.value.fp }}</span>
                    <span>{{ t('knowledgeStrategy.evaluation.fn') }} {{ column.value.fn }}</span><span>{{ t('knowledgeStrategy.evaluation.tn') }} {{ column.value.tn }}</span>
                  </div>
                  <div>{{ t('knowledgeStrategy.evaluation.precision') }}: {{ formatMetric(column.value.precision) }}</div>
                  <div>{{ t('knowledgeStrategy.evaluation.recall') }}: {{ formatMetric(column.value.recall) }}</div>
                </div>
              </div>
            </div>
            <details v-if="evaluationCasesFor(version.id).length">
              <summary>{{ t('knowledgeStrategy.evaluation.allSamples', { count: evaluationCasesFor(version.id).length }) }} · {{ t('knowledgeStrategy.evaluation.mistakes', { count: evaluationErrorsFor(version.id).length }) }}</summary>
              <div v-for="sample in evaluationCasesFor(version.id)" :key="sample.knowledge_id" class="strategy-sample">
                <strong>{{ sample.knowledge_id }} · {{ t(`knowledgeStrategy.evaluation.labels.${sample.label}`) }} · {{ t(`knowledgeStrategy.evaluation.splits.${sample.split}`) }}</strong>
                <div>{{ t('knowledgeStrategy.evaluation.old') }}: {{ t(`knowledgeStrategy.evaluation.decisions.${sample.old_decision}`) }} · {{ t(`knowledgeStrategy.evaluation.${sample.old_correct == null ? 'notCounted' : sample.old_correct ? 'correct' : 'incorrect'}`) }} — {{ sample.old_reason }}</div>
                <div>{{ t('knowledgeStrategy.evaluation.new') }}: {{ t(`knowledgeStrategy.evaluation.decisions.${sample.new_decision}`) }} · {{ t(`knowledgeStrategy.evaluation.${sample.new_correct == null ? 'notCounted' : sample.new_correct ? 'correct' : 'incorrect'}`) }} — {{ sample.new_reason }}</div>
              </div>
            </details>
            <p class="strategy-evaluation__note">{{ t('knowledgeStrategy.evaluation.note') }}</p>
          </section>
          <t-button v-if="version.status !== 'active'" size="small" variant="outline" :loading="activating === version.id"
            @click="activate(version)">{{ t(version.status === 'candidate' ? 'knowledgeBase.strategy.activate' : 'knowledgeBase.strategy.restore') }}</t-button>
        </article>
      </t-loading>
    </div>
  </t-drawer>
</template>

<script setup lang="ts">
import { formatCountCopy } from '@/i18n/countCopy'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { listKnowledgeStrategies, proposeKnowledgeStrategy, activateKnowledgeStrategy, reprocessKnowledge,
  evaluateKnowledgeStrategy, type KnowledgeStrategyVersion, type KnowledgeReprocessResult,
  type KnowledgeStrategyEvaluationResult, type KnowledgeStrategyEvaluationSample } from '@/api/knowledge-strategies'

const props = defineProps<{ visible: boolean; kbId: string; selectedIds: string[]; initialTagId?: string;
  tags: Array<{ id: string; name: string; kind?: string }> }>()
const emit = defineEmits<{ 'update:visible': [value: boolean]; reprocessed: [] }>()
const { t } = useI18n()
const versions = ref<KnowledgeStrategyVersion[]>([])
const instruction = ref('')
const sampleText = ref('')
const loading = ref(false)
const saving = ref(false)
const activating = ref('')
const evaluating = ref('')
const evaluationText = ref('')
const evaluationInputError = ref('')
const evaluationResults = ref<Record<string, KnowledgeStrategyEvaluationResult>>({})
const evaluationErrors = ref<Record<string, string>>({})
const reprocessing = ref(false)
const lastResult = ref<KnowledgeReprocessResult | null>(null)
const scopeMode = ref<'messages' | 'tag'>('messages')
const scopeOptions = computed(() => [
  { label: t('knowledgeBase.strategy.messagesScope'), value: 'messages' },
  { label: t('knowledgeBase.strategy.tagScope'), value: 'tag' },
])
const reprocessIDsText = ref('')
const reprocessIDs = computed(() => [...new Set(reprocessIDsText.value.split(',').map(id => id.trim()).filter(Boolean))])
const tagID = ref('')
const tagOptions = computed(() => props.tags.filter(tag => ['topic', 'object', 'event'].includes(tag.kind || ''))
  .map(tag => ({ value: tag.id, label: tag.name })))
const sampleIDs = computed(() => [...new Set(sampleText.value.split(',').map(id => id.trim()).filter(Boolean))])

async function load() {
  if (!props.kbId) return
  loading.value = true
  try {
    versions.value = (await listKnowledgeStrategies(props.kbId)).data || []
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.strategy.failed'))
  } finally {
    loading.value = false
  }
}

async function propose() {
  if (!instruction.value || sampleIDs.value.length < 1 || sampleIDs.value.length > 3) return
  saving.value = true
  try {
    await proposeKnowledgeStrategy(props.kbId, instruction.value, sampleIDs.value)
    instruction.value = ''
    await load()
    MessagePlugin.success(t('knowledgeBase.strategy.candidateSaved'))
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.strategy.failed'))
  } finally {
    saving.value = false
  }
}

function parseEvaluationSamples(): KnowledgeStrategyEvaluationSample[] {
  const lines = evaluationText.value.split(/\r?\n/).map(line => line.trim()).filter(Boolean)
  if (!lines.length || lines.length > 20) throw new Error(t('knowledgeStrategy.evaluation.invalidCount'))
  const samples = lines.map(line => {
    const [knowledge_id, label, split, ...extra] = line.split(/[\t,]/).map(value => value.trim())
    if (!knowledge_id || extra.length || !['select', 'reject', 'either'].includes(label) || !['development', 'holdout'].includes(split)) {
      throw new Error(t('knowledgeStrategy.evaluation.invalidFormat'))
    }
    return { knowledge_id, label, split } as KnowledgeStrategyEvaluationSample
  })
  if (new Set(samples.map(sample => sample.knowledge_id)).size !== samples.length) {
    throw new Error(t('knowledgeStrategy.evaluation.duplicateIDs'))
  }
  return samples
}

async function evaluate(version: KnowledgeStrategyVersion) {
  evaluationInputError.value = ''
  evaluationErrors.value[version.id] = ''
  let samples: KnowledgeStrategyEvaluationSample[]
  try {
    samples = parseEvaluationSamples()
  } catch (error: any) {
    evaluationInputError.value = error?.message || t('knowledgeStrategy.evaluation.invalidFormat')
    return
  }
  evaluating.value = version.id
  try {
    evaluationResults.value[version.id] = (await evaluateKnowledgeStrategy(props.kbId, version.id, samples)).data
  } catch (error: any) {
    evaluationErrors.value[version.id] = error?.message || t('knowledgeBase.strategy.failed')
  } finally {
    evaluating.value = ''
  }
}

function evaluationParts(versionID: string) {
  const result = evaluationResults.value[versionID]
  if (!result) return []
  return [
    { key: 'development', title: t('knowledgeStrategy.evaluation.splits.development'), result: result.development },
    { key: 'holdout', title: t('knowledgeStrategy.evaluation.splits.holdout'), result: result.holdout },
  ]
}

function evaluationErrorsFor(versionID: string) {
  return (evaluationResults.value[versionID]?.cases || []).filter(sample => sample.old_correct === false || sample.new_correct === false)
}

function evaluationCasesFor(versionID: string) {
  return evaluationResults.value[versionID]?.cases || []
}

function formatMetric(value: number | null) {
  return value == null ? '—' : `${(value * 100).toFixed(1)}%`
}

function activate(version: KnowledgeStrategyVersion) {
  DialogPlugin.confirm({ header: t('knowledgeBase.strategy.confirmTitle'), body: t('knowledgeBase.strategy.confirm'),
    onConfirm: async () => {
      activating.value = version.id
      try {
        await activateKnowledgeStrategy(props.kbId, version.id)
        await load()
        MessagePlugin.success(t('knowledgeBase.strategy.activeSaved'))
      } catch (error: any) {
        MessagePlugin.error(error?.message || t('knowledgeBase.strategy.failed'))
      } finally {
        activating.value = ''
      }
    } })
}

function reprocess() {
  DialogPlugin.confirm({ header: t('knowledgeBase.strategy.reprocessConfirmTitle'), body: t('knowledgeBase.strategy.reprocessConfirm'),
    onConfirm: async () => {
      reprocessing.value = true
      try {
        const scope = scopeMode.value === 'tag' ? { tag_id: tagID.value } : { message_ids: reprocessIDs.value }
        lastResult.value = (await reprocessKnowledge(props.kbId, scope)).data
        emit('reprocessed')
      } catch (error: any) {
        MessagePlugin.error(error?.message || t('knowledgeBase.strategy.failed'))
      } finally {
        reprocessing.value = false
      }
    } })
}

watch(() => props.visible, visible => {
  if (visible) {
    sampleText.value = props.selectedIds.slice(0, 3).join(', ')
    reprocessIDsText.value = props.selectedIds.join(', ')
    tagID.value = props.initialTagId || ''
    scopeMode.value = props.selectedIds.length ? 'messages' : 'tag'
    lastResult.value = null
    evaluationResults.value = {}
    evaluationErrors.value = {}
    evaluationInputError.value = ''
    evaluationText.value = ''
    load()
  }
})
</script>

<style scoped>
.strategy-drawer { display: grid; gap: 12px; }
.strategy-version { padding: 14px 0; border-top: 1px solid var(--td-component-stroke); }
.strategy-version__head { display: flex; justify-content: space-between; gap: 12px; }
.strategy-rule, .strategy-sample { padding: 8px 0; white-space: pre-wrap; }
.strategy-rule p { margin: 4px 0 10px; }
.strategy-reprocess { display: grid; gap: 10px; padding: 12px 0; border-top: 1px solid var(--td-component-stroke); }
.strategy-reprocess p { margin: 0; }
.strategy-evaluation-input, .strategy-evaluation-result { display: grid; gap: 10px; padding: 12px 0; border-top: 1px solid var(--td-component-stroke); }
.strategy-evaluation-input h3, .strategy-evaluation-result h4 { margin: 0; }
.strategy-evaluation-input p, .strategy-evaluation-result p { margin: 0; }
.strategy-evaluation-compare { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-top: 8px; }
.strategy-evaluation-model { display: grid; gap: 5px; padding: 10px; border: 1px solid var(--td-component-stroke); border-radius: 6px; }
.strategy-evaluation-counts { display: grid; grid-template-columns: repeat(2, 1fr); gap: 4px; }
.strategy-evaluation-split { display: grid; gap: 4px; }
.strategy-evaluation__error { color: var(--td-error-color); }
.strategy-evaluation__note { color: var(--td-text-color-secondary); }
@media (max-width: 520px) { .strategy-evaluation-compare { grid-template-columns: 1fr; } }
</style>
