<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { listEntities, setEntryEntities, type Entity } from '@/api/entities'

const props = defineProps<{ entryId: string; entities: Pick<Entity, 'id' | 'canonical_name' | 'entity_type'>[]; manual?: boolean }>()
const emit = defineEmits<{ saved: [value: { entities: Entity[]; entities_manual: boolean }] }>()
const { t } = useI18n()
const visible = ref(false), loading = ref(false), saving = ref(false), error = ref('')
const choices = ref<Entity[]>([]), selected = ref<string[]>([]), hasMore = ref(false)
let request = 0
const options = computed(() => {
  const items = new Map([...props.entities, ...choices.value].map(entity => [entity.id, entity]))
  return [...items.values()].map(entity => ({ value: entity.id, label: entity.canonical_name }))
})
async function load(more = false) {
  const current = ++request
  loading.value = true; error.value = ''
  try {
    const response = await listEntities({ limit: 100, offset: more ? choices.value.length : 0 })
    if (!response.success) throw new Error(response.error || t('entityMaintenance.requestFailed'))
    if (current === request) { choices.value = more ? [...choices.value, ...response.data] : response.data; hasMore.value = response.data.length === 100 }
  } catch (cause) { if (current === request) error.value = cause instanceof Error ? cause.message : t('entityMaintenance.requestFailed') }
  finally { if (current === request) loading.value = false }
}
function open() { selected.value = props.entities.map(entity => entity.id); visible.value = true; void load() }
async function save() {
  if (saving.value || selected.value.length > 3) return
  const id = props.entryId
  saving.value = true; error.value = ''
  try {
    const response = await setEntryEntities(id, selected.value)
    if (id !== props.entryId) return
    if (!response.success) throw new Error(response.error || t('entityMaintenance.requestFailed'))
    emit('saved', { entities: response.data.entities, entities_manual: response.data.entities_manual })
    visible.value = false; MessagePlugin.success(t('entityMaintenance.saved'))
  } catch (cause) { if (id === props.entryId) error.value = cause instanceof Error ? cause.message : t('entityMaintenance.requestFailed') }
  finally { if (id === props.entryId) saving.value = false }
}
watch(() => props.entryId, () => { request++; visible.value = false; saving.value = false; choices.value = []; error.value = '' })
onBeforeUnmount(() => { request++ })
</script>

<template>
  <div class="entry-entities-editor">
    <t-tag v-if="manual" size="small" variant="light-outline">{{ t('entityMaintenance.manual') }}</t-tag>
    <t-button size="small" variant="outline" @click="open">{{ t('entityMaintenance.editEntry') }}</t-button>
  </div>
  <t-dialog v-model:visible="visible" :header="t('entityMaintenance.editEntry')" :footer="false" :close-on-overlay-click="false" :close-btn="!saving" :close-on-esc-keydown="!saving" width="min(520px, 95vw)">
    <p>{{ t('entityMaintenance.entryHint') }}</p>
    <t-select v-model="selected" multiple filterable clearable :options="options" :loading="loading" :disabled="saving" :max="3" :aria-label="t('library.articleEntities')" />
    <t-button v-if="hasMore" variant="text" :loading="loading" :disabled="saving" @click="load(true)">{{ t('entityMaintenance.loadMore') }}</t-button>
    <p v-if="error" role="alert">{{ error }} <t-button variant="text" :disabled="saving" @click="load()">{{ t('common.retry') }}</t-button></p>
    <div class="entry-entities-editor__actions"><t-button variant="outline" :disabled="saving" @click="visible = false">{{ t('common.cancel') }}</t-button><t-button :loading="saving" :disabled="loading || !!error || selected.length > 3" @click="save">{{ t('common.save') }}</t-button></div>
  </t-dialog>
</template>

<style scoped>
.entry-entities-editor { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-top: 12px; }
.entry-entities-editor__actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
p[role="alert"] { color: var(--td-error-color); overflow-wrap: anywhere; }
</style>
