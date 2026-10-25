<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { useDialogDraft } from '@/composables/useDialogDraft'

type Option = { value: string; label: string }
const props = defineProps<{
  entryId: string; initial: Option[]; manual?: boolean; title: string; hint: string; label: string
  loadOptions: (offset: number) => Promise<{ options: Option[]; hasMore: boolean }>
  saveChoice: (id: string, ids: string[]) => Promise<void>
}>()
const { t } = useI18n()
const visible = ref(false), loading = ref(false), saving = ref(false)
const loadError = ref(''), saveError = ref('')
const choices = ref<Option[]>([]), selected = ref<string[]>([]), hasMore = ref(false)
let request = 0, alive = true
const options = computed(() => [...new Map([...props.initial, ...choices.value].map(item => [item.value, item])).values()])
const dirty = computed(() => visible.value && selected.value.slice().sort().join(',') !== props.initial.map(item => item.value).sort().join(','))
const { discardVisible, close, cancelDiscard, discardChanges } = useDialogDraft(dirty, saving, () => { visible.value = false })
async function load(more = false) {
  if (more && loading.value) return
  const current = ++request
  loading.value = true; loadError.value = ''
  try {
    const result = await props.loadOptions(more ? choices.value.length : 0)
    if (current === request) { choices.value = more ? [...choices.value, ...result.options] : result.options; hasMore.value = result.hasMore }
  } catch (cause) { if (current === request) loadError.value = cause instanceof Error ? cause.message : t('domains.requestFailed') }
  finally { if (current === request) loading.value = false }
}
function open() { selected.value = props.initial.map(item => item.value); saveError.value = ''; visible.value = true; void load() }
async function save() {
  if (saving.value || selected.value.length > 3) return
  const id = props.entryId
  saving.value = true; saveError.value = ''
  try {
    await props.saveChoice(id, [...selected.value])
    if (!alive || id !== props.entryId) return
    visible.value = false; MessagePlugin.success(t('domains.choiceSaved'))
  } catch (cause) { if (alive && id === props.entryId) saveError.value = cause instanceof Error ? cause.message : t('domains.requestFailed') }
  finally { if (alive && id === props.entryId) saving.value = false }
}
watch(() => props.entryId, () => { request++; visible.value = false; saving.value = false; choices.value = []; loadError.value = ''; saveError.value = '' })
onBeforeUnmount(() => { alive = false; request++ })
</script>

<template>
  <div class="entry-associations-editor">
    <t-tag v-if="manual" size="small" variant="light-outline">{{ t('entityMaintenance.manual') }}</t-tag>
    <t-button size="small" variant="outline" @click="open">{{ title }}</t-button>
  </div>
  <t-dialog :visible="visible" :header="title" :footer="false" :close-on-overlay-click="false" :close-btn="!saving" :close-on-esc-keydown="!saving" width="min(520px, 95vw)" @close="close">
    <p>{{ hint }}</p>
    <t-select v-model="selected" multiple filterable clearable :options="options" :loading="loading" :disabled="saving" :max="3" :aria-label="label" />
    <t-button v-if="hasMore" variant="text" :loading="loading" :disabled="saving" @click="load(true)">{{ t('entityMaintenance.loadMore') }}</t-button>
    <p v-if="loadError" role="alert">{{ loadError }} <t-button variant="text" :disabled="saving" @click="load()">{{ t('common.retry') }}</t-button></p>
    <p v-if="saveError" role="alert">{{ saveError }}</p>
    <div class="entry-associations-editor__actions"><t-button variant="outline" :disabled="saving" @click="close">{{ t('common.cancel') }}</t-button><t-button :loading="saving" :disabled="loading || !!loadError || selected.length > 3" @click="save">{{ t('common.save') }}</t-button></div>
  </t-dialog>
  <t-dialog v-model:visible="discardVisible" :header="t('domains.discardTitle')" :footer="false" width="min(420px, 95vw)" @close="cancelDiscard">
    <p>{{ t('domains.discardHint') }}</p>
    <div class="entry-associations-editor__actions"><t-button variant="outline" @click="cancelDiscard">{{ t('common.cancel') }}</t-button><t-button theme="danger" @click="discardChanges">{{ t('domains.discard') }}</t-button></div>
  </t-dialog>
</template>

<style scoped>
.entry-associations-editor { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-top: 12px; }
.entry-associations-editor__actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
p[role="alert"] { color: var(--td-error-color); overflow-wrap: anywhere; }
</style>
