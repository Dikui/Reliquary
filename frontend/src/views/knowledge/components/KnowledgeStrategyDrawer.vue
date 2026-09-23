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
      <div class="strategy-reprocess">
        <h3>{{ t('knowledgeBase.strategy.reprocess') }}</h3>
        <p>{{ t('knowledgeBase.strategy.reprocessHint') }}</p>
        <t-radio-group v-model="scopeMode" :options="scopeOptions" />
        <t-input v-if="scopeMode === 'messages'" v-model.trim="reprocessIDsText" :placeholder="t('knowledgeBase.strategy.samples')" />
        <t-select v-else v-model="tagID" filterable :options="tagOptions" :placeholder="t('knowledgeBase.strategy.selectTag')" />
        <t-button :loading="reprocessing" :disabled="scopeMode === 'messages' ? !reprocessIDs.length : !tagID" @click="reprocess">
          {{ t('knowledgeBase.strategy.runReprocess') }}
        </t-button>
        <p v-if="lastResult">{{ t('knowledgeBase.strategy.reprocessResult', lastResult) }}</p>
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
          <t-button v-if="version.status !== 'active'" size="small" variant="outline" :loading="activating === version.id"
            @click="activate(version)">{{ t(version.status === 'candidate' ? 'knowledgeBase.strategy.activate' : 'knowledgeBase.strategy.restore') }}</t-button>
        </article>
      </t-loading>
    </div>
  </t-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { listKnowledgeStrategies, proposeKnowledgeStrategy, activateKnowledgeStrategy, reprocessKnowledge,
  type KnowledgeStrategyVersion, type KnowledgeReprocessResult } from '@/api/knowledge-strategies'

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
</style>
