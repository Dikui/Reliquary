<template>
  <t-drawer :visible="visible" :zIndex="1900" :header="t('knowledgeBase.anomalies.title')" size="560px" :footer="false"
    @close="emit('update:visible', false)">
    <div class="anomaly-window">
      <div class="anomaly-report">
        <h3>{{ t('knowledgeBase.anomalies.report') }}</h3>
        <t-input v-model.trim="messageIds" :placeholder="t('knowledgeBase.anomalies.messageId')" />
        <t-select v-model="kind" :options="kindOptions" />
        <t-textarea v-model.trim="description" :placeholder="t('knowledgeBase.anomalies.description')" :maxlength="2000" />
        <t-select v-model="proposalType" :options="proposalOptions" />
        <template v-if="proposalType === 'change_tags'">
          <t-select v-model="proposedTagIds" multiple filterable :options="tagOptions" :placeholder="t('knowledgeBase.anomalies.addTags')" />
          <t-select v-model="proposedRemoveTagIds" multiple filterable :options="tagOptions" :placeholder="t('knowledgeBase.anomalies.removeTags')" />
        </template>
        <t-select v-if="proposalType === 'delete_tag'" v-model="proposedDeleteTagId" filterable :options="tagOptions"
          :placeholder="t('knowledgeBase.anomalies.proposedDelete')" />
        <t-button theme="primary" :loading="saving" :disabled="!canSubmit" @click="submitReport">
          {{ t('knowledgeBase.anomalies.submit') }}
        </t-button>
      </div>

      <div class="anomaly-list">
        <h3>{{ t('knowledgeBase.anomalies.list') }}</h3>
        <t-loading :loading="loading" size="small">
          <t-empty v-if="!loading && !items.length" :description="t('knowledgeBase.anomalies.empty')" />
          <article v-for="item in items" :key="item.id" class="anomaly-item">
            <div class="anomaly-item__head">
              <strong>{{ kindLabel(item.kind) }}</strong>
              <t-tag :theme="item.status === 'pending' ? 'warning' : 'default'" size="small">
                {{ t(`knowledgeBase.anomalies.status.${item.status}`) }}
              </t-tag>
            </div>
            <p>{{ item.description }}</p>
            <div class="anomaly-item__meta">{{ item.reported_by }} · {{ new Date(item.created_at).toLocaleString() }}</div>
            <div v-if="item.proposal_type === 'change_tags'" class="anomaly-item__proposal">
              + {{ item.proposed_tag_ids.map(tagName).join('、') || t('knowledgeBase.anomalies.noTags') }}
              <br />− {{ item.proposed_remove_tag_ids.map(tagName).join('、') || t('knowledgeBase.anomalies.noTags') }}
            </div>
            <div v-else-if="item.proposal_type === 'delete_tag'" class="anomaly-item__proposal">
              {{ t('knowledgeBase.anomalies.proposedDelete') }}: {{ tagName(item.proposed_delete_tag_id) }}
            </div>
            <div class="anomaly-item__actions">
              <t-button v-for="id in item.knowledge_ids" :key="id" variant="text" size="small" @click="emit('open-message', id)">
                {{ t('knowledgeBase.anomalies.openMessage') }} · {{ id.slice(0, 8) }}
              </t-button>
              <t-button v-if="isOwner && item.status === 'pending' && item.knowledge_ids.length === 1" variant="text" size="small"
                @click="emit('edit-tags', item.knowledge_ids[0], item.id)">{{ t('knowledgeBase.anomalies.editTags') }}</t-button>
            </div>
            <div v-if="item.status === 'pending' && isOwner" class="anomaly-item__decisions">
              <t-button v-if="item.proposal_type !== 'none'" size="small" theme="primary" :loading="decidingId === item.id"
                @click="decide(item, 'apply')">{{ t('knowledgeBase.anomalies.apply') }}</t-button>
              <t-button size="small" variant="outline" :loading="decidingId === item.id" @click="decide(item, 'resolved')">
                {{ t('knowledgeBase.anomalies.resolved') }}
              </t-button>
              <t-button size="small" variant="text" :loading="decidingId === item.id" @click="decide(item, 'dismiss')">
                {{ t('knowledgeBase.anomalies.dismiss') }}
              </t-button>
            </div>
            <div v-if="item.decided_at" class="anomaly-item__result">
              {{ t('knowledgeBase.anomalies.decision') }}: {{ item.decided_by }} · {{ new Date(item.decided_at).toLocaleString() }}
              <span v-if="item.result"> · {{ t(`knowledgeBase.anomalies.result.${item.result}`) }}</span>
              <span v-if="item.decision_note"> · {{ item.decision_note }}</span>
              <span v-for="(change, id) in item.applied_changes || {}" :key="id">
                · {{ id.slice(0, 8) }}: +{{ change.added.map(tagName).join('、') || '∅' }} −{{ change.removed.map(tagName).join('、') || '∅' }}
              </span>
            </div>
          </article>
          <t-button v-if="hasMore" variant="outline" :loading="loading" @click="load(true)">
            {{ t('knowledgeBase.anomalies.loadMore') }}
          </t-button>
        </t-loading>
      </div>
    </div>
  </t-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { listKnowledgeTags } from '@/api/knowledge-base'
import { listKnowledgeAnomalies, createKnowledgeAnomaly, decideKnowledgeAnomaly,
  type KnowledgeAnomaly, type KnowledgeAnomalyKind, type KnowledgeAnomalyProposal } from '@/api/knowledge-anomalies'

const props = defineProps<{
  visible: boolean
  kbId: string
  isOwner: boolean
  initialKnowledgeId?: string
  tags: Array<{ id: string; name: string }>
}>()
const emit = defineEmits<{
  'update:visible': [value: boolean]
  'open-message': [id: string]
  'edit-tags': [id: string, anomalyId: string]
  changed: []
}>()
const { t } = useI18n()
const items = ref<KnowledgeAnomaly[]>([])
const hasMore = ref(false)
const availableTags = ref<Array<{ id: string; name: string }>>([])
const loading = ref(false)
const saving = ref(false)
const decidingId = ref('')
const messageIds = ref('')
const kind = ref<KnowledgeAnomalyKind>('suspected_error')
const description = ref('')
const proposalType = ref<KnowledgeAnomalyProposal>('none')
const proposedTagIds = ref<string[]>([])
const proposedRemoveTagIds = ref<string[]>([])
const proposedDeleteTagId = ref('')
const kinds: KnowledgeAnomalyKind[] = ['suspected_error', 'outdated', 'source_conflict', 'manual_conflict', 'tag_deletion']
const kindLabel = (value: KnowledgeAnomalyKind) => t(`knowledgeBase.anomalies.kind.${value}`)
const kindOptions = computed(() => kinds.map(value => ({ value, label: kindLabel(value) })))
const proposalOptions = computed(() => (['none', 'change_tags', 'delete_tag'] as KnowledgeAnomalyProposal[])
  .map(value => ({ value, label: t(`knowledgeBase.anomalies.proposal.${value}`) })))
const tagOptions = computed(() => availableTags.value.map(tag => ({ value: tag.id, label: tag.name })))
const tagName = (id: string) => availableTags.value.find(tag => tag.id === id)?.name || props.tags.find(tag => tag.id === id)?.name || id
const canSubmit = computed(() => (proposalType.value === 'delete_tag' || !!messageIds.value) && !!description.value &&
  (proposalType.value === 'none' || proposalType.value === 'delete_tag' && !!proposedDeleteTagId.value ||
    proposalType.value === 'change_tags' && proposedTagIds.value.length + proposedRemoveTagIds.value.length > 0 &&
    !proposedTagIds.value.some(id => proposedRemoveTagIds.value.includes(id))))

async function load(more = false) {
  if (!props.kbId) return
  loading.value = true
  try {
    const response = await listKnowledgeAnomalies(props.kbId, more ? items.value.length : 0)
    items.value = more ? [...items.value, ...(response.data || [])] : (response.data || [])
    hasMore.value = response.has_more
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.anomalies.loadFailed'))
  } finally {
    loading.value = false
  }
}
defineExpose({ refresh: () => load() })

async function loadTags() {
  try {
    const response: any = await listKnowledgeTags(props.kbId, { page: 1, page_size: 1000 })
    availableTags.value = response?.data?.data || []
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.anomalies.loadFailed'))
  }
}

watch(() => props.visible, visible => {
  if (visible) {
    messageIds.value = props.initialKnowledgeId || ''
    void load()
    void loadTags()
  }
})
watch(() => props.kbId, () => { if (props.visible) void load() })

async function submitReport() {
  saving.value = true
  try {
    await createKnowledgeAnomaly(props.kbId, {
      knowledge_ids: messageIds.value.split(/[，,\s]+/).filter(Boolean), kind: kind.value, description: description.value,
      proposal_type: proposalType.value, proposed_tag_ids: proposedTagIds.value,
      proposed_remove_tag_ids: proposedRemoveTagIds.value, proposed_delete_tag_id: proposedDeleteTagId.value,
    })
    description.value = ''
    proposalType.value = 'none'
    proposedTagIds.value = []
    proposedRemoveTagIds.value = []
    proposedDeleteTagId.value = ''
    MessagePlugin.success(t('knowledgeBase.anomalies.submitted'))
    await load()
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.anomalies.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function runDecision(item: KnowledgeAnomaly, action: 'apply' | 'resolved' | 'dismiss') {
  decidingId.value = item.id
  try {
    await decideKnowledgeAnomaly(props.kbId, item.id, action)
    MessagePlugin.success(t('knowledgeBase.anomalies.saved'))
    await load()
    emit('changed')
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('knowledgeBase.anomalies.saveFailed'))
  } finally {
    decidingId.value = ''
  }
}

function decide(item: KnowledgeAnomaly, action: 'apply' | 'resolved' | 'dismiss') {
  if (action === 'apply' && item.proposal_type === 'delete_tag') {
    const dialog = DialogPlugin.confirm({
      header: t('knowledgeBase.anomalies.deleteConfirmTitle'),
      body: t('knowledgeBase.anomalies.deleteConfirm', { name: tagName(item.proposed_delete_tag_id) }),
      onConfirm: () => { dialog.destroy(); void runDecision(item, action) },
      onCancel: () => dialog.destroy(),
    })
    return
  }
  void runDecision(item, action)
}
</script>

<style scoped>
.anomaly-window { display: grid; gap: 24px; }
.anomaly-report, .anomaly-list { display: grid; gap: 12px; }
.anomaly-report h3, .anomaly-list h3 { margin: 0; font-size: 15px; }
.anomaly-item { padding: 14px 0; border-top: 1px solid var(--td-component-stroke); }
.anomaly-item__head, .anomaly-item__actions, .anomaly-item__decisions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.anomaly-item__head { justify-content: space-between; }
.anomaly-item p { margin: 8px 0; white-space: pre-wrap; }
.anomaly-item__meta, .anomaly-item__proposal, .anomaly-item__result { color: var(--td-text-color-secondary); font-size: 12px; margin-top: 8px; }
.anomaly-item__actions, .anomaly-item__decisions { margin-top: 10px; }
</style>
