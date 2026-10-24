<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { createEntity, updateEntity, ENTITY_TYPES, type Entity, type EntityType } from '@/api/entities'

const props = defineProps<{ entity: Entity | null }>()
const emit = defineEmits<{ close: []; saved: [entity: Entity] }>()
const { t } = useI18n()
const name = ref(props.entity?.canonical_name || '')
const kind = ref<EntityType>(props.entity?.entity_type || 'organization')
const aliases = ref(props.entity?.aliases?.join('\n') || '')
const saving = ref(false), composing = ref(false), error = ref(''), discardVisible = ref(false)
const options = computed(() => ENTITY_TYPES.map(value => ({ value, label: t(value === 'person' ? 'entityMaintenance.person' : 'entityMaintenance.organization') })))
const dirty = computed(() => name.value !== (props.entity?.canonical_name || '') || kind.value !== (props.entity?.entity_type || 'organization') || aliases.value !== (props.entity?.aliases?.join('\n') || ''))
function close() { if (saving.value) return; if (dirty.value) discardVisible.value = true; else emit('close') }
let resolveNavigation: ((leave: boolean) => void) | undefined
function guardNavigation() {
  if (saving.value) return false
  if (!dirty.value) return true
  discardVisible.value = true
  return new Promise<boolean>(resolve => { resolveNavigation?.(false); resolveNavigation = resolve })
}
function cancelDiscard() { discardVisible.value = false; resolveNavigation?.(false); resolveNavigation = undefined }
function discardChanges() { resolveNavigation?.(true); resolveNavigation = undefined; emit('close') }
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
onBeforeRouteLeave(guardNavigation)
onBeforeRouteUpdate(guardNavigation)
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onBeforeUnmount(() => { window.removeEventListener('beforeunload', beforeUnload); resolveNavigation?.(false) })

async function save() {
  if (saving.value || composing.value) return
  if (!name.value.trim()) { error.value = t('entityMaintenance.nameRequired'); document.getElementById('entity-canonical-name')?.focus(); return }
  saving.value = true; error.value = ''
  try {
    const input = { canonical_name: name.value.trim(), entity_type: kind.value, aliases: aliases.value.split('\n').map(value => value.trim()).filter(Boolean) }
    const response = props.entity ? await updateEntity(props.entity.id, input) : await createEntity(input)
    if (!response.success) throw new Error(response.error || t('entityMaintenance.requestFailed'))
    MessagePlugin.success(t('entityMaintenance.saved'))
    emit('saved', response.data)
  } catch (cause) { error.value = cause instanceof Error ? cause.message : t('entityMaintenance.requestFailed') }
  finally { saving.value = false }
}
</script>

<template>
  <t-dialog :visible="true" :header="t(entity ? 'entityMaintenance.edit' : 'entityMaintenance.create')" :footer="false" :close-on-overlay-click="false" :close-btn="!saving" :close-on-esc-keydown="!saving" width="min(520px, 95vw)" @close="close">
    <form class="entity-editor" novalidate @submit.prevent="save">
      <label for="entity-canonical-name">{{ t('entityMaintenance.name') }}</label>
      <input id="entity-canonical-name" class="entity-editor__control" type="text" v-model="name" @compositionstart="composing = true" @compositionend="composing = false" :maxlength="512" :disabled="saving" :aria-invalid="!!error" :aria-describedby="error ? 'entity-editor-error' : undefined" />
      <label for="entity-type">{{ t('entityMaintenance.type') }}</label>
      <select id="entity-type" v-model="kind" class="entity-editor__control" :disabled="saving">
        <option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option>
      </select>
      <label for="entity-aliases">{{ t('entityMaintenance.aliases') }}</label>
      <textarea id="entity-aliases" v-model="aliases" class="entity-editor__control" rows="3" @compositionstart="composing = true" @compositionend="composing = false" :disabled="saving" />
      <p v-if="error" id="entity-editor-error" role="alert">{{ error }}</p>
      <div class="entity-editor__actions"><t-button variant="outline" :disabled="saving" @click="close">{{ t('common.cancel') }}</t-button><t-button type="submit" :loading="saving">{{ t('common.save') }}</t-button></div>
    </form>
  </t-dialog>
  <t-dialog v-model:visible="discardVisible" @close="cancelDiscard" :header="t('entityMaintenance.discardTitle')" :footer="false" width="min(420px, 95vw)">
    <p>{{ t('entityMaintenance.discardHint') }}</p>
    <div class="entity-editor__actions"><t-button variant="outline" @click="cancelDiscard">{{ t('common.cancel') }}</t-button><t-button theme="danger" @click="discardChanges">{{ t('entityMaintenance.discard') }}</t-button></div>
  </t-dialog>
</template>

<style scoped>
.entity-editor { display: grid; gap: 12px; max-height: 70vh; overflow-y: auto; padding: 2px; }
.entity-editor__control {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  padding: 8px 12px;
  border: 1px solid var(--td-border-level-2-color);
  border-radius: var(--td-radius-default);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  font: inherit;
}
.entity-editor__control:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.entity-editor__control:disabled { background: var(--td-bg-color-component-disabled); color: var(--td-text-color-disabled); }
textarea.entity-editor__control { resize: vertical; min-height: 80px; }
.entity-editor__actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
.entity-editor p { color: var(--td-error-color); overflow-wrap: anywhere; }
</style>
