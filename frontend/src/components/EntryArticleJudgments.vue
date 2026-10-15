<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { correctEntryContentType, correctEntryTopics, type EntryJudgment } from '@/api/entry-judgments'
import { listTopics, type Topic } from '@/api/topics'
import { getEntryDetails } from '@/api/library'
import EntryStarToggle from './EntryStarToggle.vue'

const props = defineProps<{ entry: EntryJudgment; disabled?: boolean }>()
const emit = defineEmits<{ updated: [value: EntryJudgment]; 'busy-change': [value: boolean] }>()
const { t } = useI18n()
const typeDraft = ref<string>(''), topicDraft = ref<string[]>([])
const topics = ref<Topic[]>([]), optionsLoading = ref(false), optionsError = ref('')
const typeSaving = ref(false), topicsSaving = ref(false), typeError = ref(''), topicsError = ref('')
const typeOptions = computed(() => (props.entry.library_content_types || []).filter(row => row.enabled).map(row => ({ value: row.name, label: row.name })))
const topicOptions = computed(() => topics.value.map(topic => ({ value: topic.id, label: topic.name })))
const busy = computed(() => props.disabled || typeSaving.value || topicsSaving.value)
watch([typeSaving, topicsSaving], () => emit('busy-change', typeSaving.value || topicsSaving.value))
onUnmounted(() => { optionsGeneration++; emit('busy-change', false) })
let optionsGeneration = 0
async function loadTopics() {
  const generation = ++optionsGeneration
  optionsLoading.value = true
  optionsError.value = ''
  try {
    const response = await listTopics()
    if (!response.success) throw new Error(response.message)
    if (generation === optionsGeneration) topics.value = response.data || []
  } catch { if (generation === optionsGeneration) optionsError.value = t('articleJudgments.topicsLoadFailed') }
  finally { if (generation === optionsGeneration) optionsLoading.value = false }
}
watch(() => props.entry.id, () => {
  void loadTopics()
}, { immediate: true })
watch(() => props.entry.content_type, value => { typeDraft.value = value || '' }, { immediate: true })
watch(() => props.entry.topics, value => { topicDraft.value = (value || []).map(topic => topic.id) }, { immediate: true, deep: true })
async function saveType() {
  if (!typeDraft.value || busy.value) return
  const id = props.entry.id, type = typeDraft.value
  typeSaving.value = true
  typeError.value = ''
  try {
    const response = await correctEntryContentType(id, type)
    if (!response.success) throw new Error(response.message)
    if (props.entry.id !== id) return
    emit('updated', { ...response.data, id })
    const message = 'articleJudgments.contentTypeSaved'
    MessagePlugin.success(t(message))
  } catch (cause: any) {
    if (props.entry.id === id) typeError.value = cause?.message || t('articleJudgments.contentTypeFailed')
  } finally { typeSaving.value = false }
}
async function saveTopics() {
  if (busy.value || optionsLoading.value || optionsError.value) return
  const id = props.entry.id
  topicsSaving.value = true
  topicsError.value = ''
  try {
    const response = await correctEntryTopics(id, [...topicDraft.value])
    if (!response.success) throw new Error(response.message)
    const detail = await getEntryDetails(id)
    if (props.entry.id !== id) return
    emit('updated', { ...detail.data, id })
    MessagePlugin.success(t('articleJudgments.topicsSaved'))
  } catch { if (props.entry.id === id) topicsError.value = t('articleJudgments.topicsFailed') }
  finally { topicsSaving.value = false }
}
</script>

<template>
  <section class="article-judgments" :aria-label="t('articleJudgments.title')">
    <div class="judgment-star"><span>{{ t('articleJudgments.starLabel') }}</span><EntryStarToggle :entry="entry" :disabled="busy" @updated="emit('updated', $event)" /></div>
    <div class="judgment-row">
      <label class="judgment-field"><span>{{ t('articleJudgments.contentType') }}</span><t-select v-model="typeDraft" :options="typeOptions" :disabled="busy" :aria-label="t('articleJudgments.contentType')" /></label>
      <t-button size="small" variant="outline" :loading="typeSaving" :disabled="busy || !typeDraft || typeDraft === entry.content_type" @click="saveType()">{{ t('articleJudgments.saveContentType') }}</t-button>
    </div>
    <p class="judgment-hint">{{ t('articleJudgments.contentTypeHint') }}</p>
    <p v-if="typeError" role="alert" class="judgment-error">{{ typeError }}</p>
    <div class="judgment-row">
      <label class="judgment-field"><span>{{ t('articleJudgments.topics') }}</span><t-select v-model="topicDraft" multiple clearable filterable :options="topicOptions" :loading="optionsLoading" :disabled="busy || Boolean(optionsError)" :aria-label="t('articleJudgments.topics')" :placeholder="t('articleJudgments.noTopics')" /></label>
      <t-button size="small" variant="outline" :loading="topicsSaving" :disabled="busy || optionsLoading || Boolean(optionsError)" @click="saveTopics">{{ t('articleJudgments.saveTopics') }}</t-button>
    </div>
    <div v-if="optionsError" class="judgment-error" role="alert">{{ optionsError }} <t-button size="small" variant="text" @click="loadTopics">{{ t('common.retry') }}</t-button></div>
    <p v-if="topicsError" role="alert" class="judgment-error">{{ topicsError }}</p>
  </section>
</template>

<style scoped lang="less">
.article-judgments { display: grid; gap: 12px; padding-top: 16px; margin-top: 16px; border-top: 1px solid var(--td-component-stroke); }
.judgment-star, .judgment-row { display: flex; align-items: center; gap: 12px; }
.judgment-row { align-items: flex-end; }
.judgment-field { display: flex; flex-direction: column; gap: 6px; flex: 1; min-width: 0; }
.judgment-star, .judgment-field { color: var(--td-text-color-secondary); font-size: 13px; }
.judgment-hint, .judgment-error { margin: 0; font-size: 12px; line-height: 1.7; overflow-wrap: anywhere; }
.judgment-hint { color: var(--td-text-color-secondary); }
.judgment-error { color: var(--td-error-color); }
@media (max-width: 560px) { .judgment-row { flex-direction: column; align-items: stretch; } }
</style>
