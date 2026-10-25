<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import ModalDialog from '@/components/ModalDialog.vue'
import { getStory, listStories, mergeFacts, mergeStories, moveFact, splitFact, type CorrectionAction, type Story, type StoryFact } from '@/api/stories'

const props = defineProps<{ action: CorrectionAction; story: Story; fact?: StoryFact }>()
const emit = defineEmits<{ cancel: []; saved: [target: { storyID: string; factID?: string }] }>()
const { t, locale } = useI18n()
const actionLabel = computed(() => ({
  move: t('groupingCorrection.move'), split: t('groupingCorrection.split'),
  mergeFacts: t('groupingCorrection.mergeFacts'), mergeStories: t('groupingCorrection.mergeStories'),
})[props.action])
const stories = ref<Story[]>([]), total = ref(0), targetStory = ref(''), targetFact = ref(''), title = ref(props.fact?.title || '')
const targetField = ref<HTMLElement | null>(null)
const popupStyle = (trigger: HTMLElement) => ({
  width: `${Math.max(0, Math.min(trigger.getBoundingClientRect().width, window.innerWidth - 32))}px`,
  maxWidth: 'calc(100vw - 32px)', boxSizing: 'border-box',
})
const targetPopup = {
  attach: () => targetField.value?.closest<HTMLElement>('[role="dialog"]') || document.body,
  overlayClassName: 'grouping-target-popup', overlayStyle: popupStyle, overlayInnerStyle: popupStyle,
}
const facts = ref<StoryFact[]>([]), loading = ref(false), factsLoading = ref(false), busy = ref(false)
const listError = ref(''), factError = ref(''), actionError = ref('')
let listVersion = 0, factVersion = 0, active = true
const merging = computed(() => props.action === 'mergeFacts' || props.action === 'mergeStories')
const storyOptions = computed(() => stories.value.filter(item => props.action === 'mergeFacts' || item.id !== props.story.id).map(item => ({ label: `${item.title} · ${new Date(item.first_fact_at).toLocaleString(locale.value)}`, value: item.id })))
const factOptions = computed(() => facts.value.filter(item => item.id !== props.fact?.id).map(item => ({ label: `${item.title} · ${item.subject} ${item.action} ${item.object} · ${item.occurred_at || t('stories.unknownTime')}`, value: item.id })))
const valid = computed(() => props.action === 'split'
  ? title.value.trim().length > 0 && Array.from(title.value.trim()).length <= 255 && !/[\r\n\0]/.test(title.value)
  : !!targetStory.value && (props.action !== 'mergeFacts' || !!targetFact.value))
async function loadStories(more = false) {
  if (loading.value) return
  const ticket = ++listVersion
  loading.value = true; listError.value = ''
  try {
    const response = await listStories(50, more ? stories.value.length : 0)
    if (!response.success) throw new Error()
    if (active && ticket === listVersion) {
      stories.value = more ? [...stories.value, ...response.data.items] : response.data.items
      total.value = response.data.total
    }
  } catch { if (active && ticket === listVersion) listError.value = t('groupingCorrection.loadFailed') }
  finally { if (active && ticket === listVersion) loading.value = false }
}
async function loadFacts() {
  const ticket = ++factVersion, id = targetStory.value
  facts.value = []; targetFact.value = ''; factError.value = ''
  if (props.action !== 'mergeFacts' || !id) { factsLoading.value = false; return }
  factsLoading.value = true
  try {
    const response = await getStory(id)
    if (!response.success) throw new Error()
    if (active && ticket === factVersion) facts.value = response.data.facts
  } catch { if (active && ticket === factVersion) factError.value = t('groupingCorrection.loadFailed') }
  finally { if (active && ticket === factVersion) factsLoading.value = false }
}
async function save() {
  if (busy.value) return
  if (!valid.value) { actionError.value = t('groupingCorrection.required'); return }
  busy.value = true; actionError.value = ''
  try {
    let target: { storyID: string; factID?: string }
    if (props.action === 'mergeStories') {
      const response = await mergeStories(props.story.id, targetStory.value)
      if (!response.success) throw new Error()
      target = { storyID: response.data.story.id }
    } else {
      if (!props.fact) throw new Error()
      const response = props.action === 'move' ? await moveFact(props.fact.id, targetStory.value)
        : props.action === 'split' ? await splitFact(props.fact.id, title.value.trim())
          : await mergeFacts(props.fact.id, targetFact.value)
      if (!response.success) throw new Error()
      target = { storyID: response.data.story_id, factID: response.data.id }
    }
    if (active) { MessagePlugin.success(t('groupingCorrection.saved')); emit('saved', target) }
  } catch { if (active) actionError.value = t('groupingCorrection.failed') }
  finally { if (active) busy.value = false }
}
watch(targetStory, () => { void loadFacts() })
onMounted(() => { if (props.action !== 'split') void loadStories() })
onBeforeUnmount(() => { active = false; listVersion++; factVersion++ })
function cancel() { if (!busy.value) emit('cancel') }
</script>

<template>
  <ModalDialog :visible="true" :header="actionLabel" :busy="busy" :footer="false" width="min(560px, 95vw)" @close="cancel">
    <div class="correction" :aria-busy="busy">
      <p class="origin">{{ fact?.title || story.title }}</p>
      <p>{{ t('groupingCorrection.hint') }}</p>
      <p v-if="merging">{{ t('groupingCorrection.mergeHint') }}</p>
      <label v-if="action === 'split'" class="field"><span>{{ t('groupingCorrection.title') }}</span><t-input v-model="title" :disabled="busy" :maxlength="255" :aria-label="t('groupingCorrection.title')" /></label>
      <template v-else>
        <label ref="targetField" class="field"><span>{{ t('groupingCorrection.storyTarget') }}</span><t-select v-model="targetStory" :options="storyOptions" :popup-props="targetPopup" :loading="loading" :disabled="busy" :aria-label="t('groupingCorrection.storyTarget')" /></label>
        <p v-if="listError" role="alert">{{ listError }} <t-button variant="text" :disabled="busy" @click="loadStories(stories.length > 0)">{{ t('common.retry') }}</t-button></p>
        <t-button v-if="stories.length < total" variant="text" :loading="loading" :disabled="busy" @click="loadStories(true)">{{ t('common.loadMore') }}</t-button>
        <p v-if="!loading && !listError && !storyOptions.length">{{ t('groupingCorrection.empty') }}</p>
        <template v-if="action === 'mergeFacts' && targetStory">
          <label class="field"><span>{{ t('groupingCorrection.factTarget') }}</span><t-select v-model="targetFact" :options="factOptions" :popup-props="targetPopup" :loading="factsLoading" :disabled="busy || factsLoading" :aria-label="t('groupingCorrection.factTarget')" /></label>
          <p v-if="factError" role="alert">{{ factError }} <t-button variant="text" :disabled="busy" @click="loadFacts">{{ t('common.retry') }}</t-button></p>
          <p v-if="!factsLoading && !factError && !factOptions.length">{{ t('groupingCorrection.empty') }}</p>
        </template>
      </template>
      <p v-if="actionError" role="alert">{{ actionError }}</p>
      <div class="actions"><t-button variant="outline" :disabled="busy" @click="cancel">{{ t('common.cancel') }}</t-button><t-button :theme="merging ? 'danger' : 'primary'" :loading="busy" :disabled="busy || !valid || factsLoading" @click="save">{{ actionLabel }}</t-button></div>
    </div>
  </ModalDialog>
</template>

<style scoped>
.correction { color: var(--td-text-color-primary); max-height: 65dvh; overflow: auto; }
p { line-height: 1.65; overflow-wrap: anywhere; }
.origin { font-weight: 600; }
.field { display: grid; min-width: 0; gap: 8px; margin: 16px 0; }
.actions { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: 12px; margin-top: 20px; }
p[role="alert"] { color: var(--td-error-color); }
</style>

<style>
.grouping-target-popup .t-select-option { height: auto; min-height: var(--td-comp-size-s); }
.grouping-target-popup .t-select-option > * { min-width: 0; max-width: 100%; white-space: normal; overflow-wrap: anywhere; }
</style>
