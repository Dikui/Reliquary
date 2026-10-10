<template>
  <section class="merge-suggestions">
    <div class="merge-suggestions-heading"><div><h3>{{ t('mergeSuggestions.title') }}</h3><p>{{ t('mergeSuggestions.description') }}</p></div><t-select v-model="status" :options="statusOptions" :disabled="!!decidingId" :aria-label="t('mergeSuggestions.statusFilter')" /></div>
    <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button variant="text" :loading="loading" @click="load(false)">{{ t('common.refresh') }}</t-button></template></t-alert>
    <t-loading :loading="loading" show-overlay>
      <div class="merge-suggestions-list">
        <t-empty v-if="!items.length && !error" :description="t(`mergeSuggestions.empty.${status}`)" />
        <article v-for="item in items" :key="item.id" class="merge-suggestion">
          <div class="merge-suggestion-heading"><strong>{{ t('mergeSuggestions.pair', { source: item.new_entity_name || item.new_entity_id, target: item.candidate_entity_name || item.candidate_entity_id }) }}</strong><t-tag :theme="item.status === 'pending' ? 'warning' : item.status === 'accepted' ? 'success' : 'default'" variant="light">{{ t(`mergeSuggestions.status.${item.status}`) }}</t-tag></div>
          <p>{{ item.reason || t('mergeSuggestions.noReason') }}</p>
          <div class="merge-suggestion-actions"><t-button v-if="item.knowledge_id" variant="text" @click="emit('open-source', item.knowledge_id)">{{ t('knowledgeWorkbench.entities.openArticle') }}</t-button><template v-if="item.status === 'pending'"><t-button variant="outline" :disabled="!!decidingId" :loading="decidingId === item.id && decision === 'reject'" @click="decide(item, 'reject')">{{ t('mergeSuggestions.reject') }}</t-button><t-button theme="primary" variant="outline" :disabled="!!decidingId" :loading="decidingId === item.id && decision === 'accept'" @click="confirmAccept(item)">{{ t('mergeSuggestions.accept') }}</t-button></template></div>
        </article>
      </div>
    </t-loading>
    <div class="merge-suggestions-footer"><span role="status">{{ t('mergeSuggestions.loaded', { count: items.length }) }}</span><t-button v-if="hasMore" variant="outline" :disabled="!!decidingId" :loading="loading" @click="load(true)">{{ t('knowledgeWorkbench.loadMore') }}</t-button></div>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { acceptEntityMergeSuggestion, listEntityMergeSuggestions, rejectEntityMergeSuggestion, type EntityMergeSuggestion } from '@/api/entities'
import { useConfirmDelete } from '@/components/settings/useConfirmDelete'
const emit = defineEmits<{ 'open-source': [id: string]; changed: [] }>()
const { t } = useI18n(), confirmDelete = useConfirmDelete()
const status = ref<'pending' | 'accepted' | 'rejected'>('pending')
const statusOptions = computed(() => ['pending', 'accepted', 'rejected'].map(value => ({ value, label: t(`mergeSuggestions.status.${value}`) })))
const items = ref<EntityMergeSuggestion[]>([]), loading = ref(false), error = ref(''), hasMore = ref(false), decidingId = ref('')
const decision = ref<'accept' | 'reject'>('accept')
const pageSize = 20
let request = 0
async function load(more = false) {
  if (more && loading.value) return
  const current = ++request, offset = more ? items.value.length : 0
  loading.value = true; error.value = ''
  try { const response = await listEntityMergeSuggestions({ status: status.value, limit: pageSize, offset }); if (!response.success) throw new Error(response.error || t('knowledgeWorkbench.requestFailed')); if (current === request) { const page = response.data || []; items.value = more ? [...items.value, ...page] : page; hasMore.value = page.length === pageSize } }
  catch (cause) { if (current === request) error.value = cause instanceof Error ? cause.message : t('knowledgeWorkbench.requestFailed') }
  finally { if (current === request) loading.value = false }
}
function confirmAccept(item: EntityMergeSuggestion) { if (decidingId.value || item.status !== 'pending') return; confirmDelete({ title: t('mergeSuggestions.confirmTitle'), body: t('mergeSuggestions.confirmBody', { source: item.new_entity_name || item.new_entity_id, target: item.candidate_entity_name || item.candidate_entity_id }), confirmText: t('mergeSuggestions.accept'), onConfirm: () => decide(item, 'accept') }) }
async function decide(item: EntityMergeSuggestion, action: 'accept' | 'reject') {
  if (decidingId.value || item.status !== 'pending') return
  const current = request
  decidingId.value = item.id; decision.value = action; error.value = ''
  try { const response = await (action === 'accept' ? acceptEntityMergeSuggestion(item.id) : rejectEntityMergeSuggestion(item.id)); if (!response.success) throw new Error(response.error || t('knowledgeWorkbench.requestFailed')); if (current === request) { MessagePlugin.success(t(`mergeSuggestions.feedback.${action}`)); await load(false); emit('changed') } }
  catch (cause) { if (current === request) error.value = cause instanceof Error ? cause.message : t('knowledgeWorkbench.requestFailed') }
  finally { decidingId.value = '' }
}
function refresh() { return load(false) }
defineExpose({ refresh })
watch(status, () => { request++; items.value = []; hasMore.value = false; void load(false) }, { immediate: true })
onBeforeUnmount(() => { request++ })
</script>
<style scoped lang="less">
.merge-suggestions { display: flex; flex-direction: column; gap: 16px; }
.merge-suggestions-heading, .merge-suggestion-heading, .merge-suggestions-footer { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
.merge-suggestions-heading h3 { margin: 0; font-size: 16px; }
.merge-suggestions-heading p { color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.6; }
.merge-suggestions-heading :deep(.t-select__wrap) { width: 180px; flex-shrink: 0; }
.merge-suggestions-list { min-height: 240px; }
.merge-suggestion { padding: 16px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); margin-bottom: 12px; overflow-wrap: anywhere; }
.merge-suggestion p { color: var(--td-text-color-secondary); line-height: 1.6; }
.merge-suggestion blockquote { border-left: 3px solid var(--td-component-stroke); margin: 12px 0; padding-left: 12px; font-size: 13px; color: var(--td-text-color-secondary); }
.merge-suggestion-actions { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: 8px; }
.merge-suggestions-footer { color: var(--td-text-color-secondary); font-size: 13px; align-items: center; }
@media (max-width: 600px) { .merge-suggestions-heading, .merge-suggestion-heading { flex-direction: column; } }
</style>
