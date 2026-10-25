<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { createDomain, updateDomain, type Domain } from '@/api/domains'
import { useDialogDraft } from '@/composables/useDialogDraft'

const props = defineProps<{ domain: Domain | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const name = ref(props.domain?.name || ''), definition = ref(props.domain?.definition || '')
const saving = ref(false), composing = ref(false), error = ref(''), invalidField = ref('')
const dirty = computed(() => name.value !== (props.domain?.name || '') || definition.value !== (props.domain?.definition || ''))
const { discardVisible, close, cancelDiscard, discardChanges } = useDialogDraft(dirty, saving, () => emit('close'))
async function save() {
  if (saving.value || composing.value) return
  invalidField.value = !name.value.trim() ? 'domain-name' : !definition.value.trim() ? 'domain-definition' : ''
  if (invalidField.value) { error.value = t('domains.fieldsRequired'); document.getElementById(invalidField.value)?.focus(); return }
  saving.value = true; error.value = ''
  try {
    const input = { name: name.value.trim(), definition: definition.value.trim() }
    const response = props.domain ? await updateDomain(props.domain.id, input) : await createDomain(input)
    if (!response.success) throw new Error(response.error || t('domains.requestFailed'))
    MessagePlugin.success(t('domains.saved')); emit('saved')
  } catch (cause) { error.value = cause instanceof Error ? cause.message : t('domains.requestFailed') }
  finally { saving.value = false }
}
</script>

<template>
  <t-dialog :visible="true" :header="t(domain ? 'domains.edit' : 'domains.create')" :footer="false" :close-on-overlay-click="false" :close-btn="!saving" :close-on-esc-keydown="!saving" width="min(520px, 95vw)" @close="close">
    <form class="domain-editor" novalidate @submit.prevent="save">
      <label for="domain-name">{{ t('domains.name') }}</label>
      <input id="domain-name" v-model="name" type="text" maxlength="128" :disabled="saving" :aria-invalid="invalidField === 'domain-name'" :aria-describedby="error ? 'domain-editor-error' : undefined" @compositionstart="composing = true" @compositionend="composing = false" />
      <label for="domain-definition">{{ t('domains.definition') }}</label>
      <textarea style="resize: none" id="domain-definition" v-model="definition" rows="5" maxlength="2000" :disabled="saving" :aria-invalid="invalidField === 'domain-definition'" :aria-describedby="error ? 'domain-editor-error' : undefined" @compositionstart="composing = true" @compositionend="composing = false" />
      <p v-if="error" id="domain-editor-error" role="alert">{{ error }}</p>
      <div class="domain-editor__actions"><t-button variant="outline" :disabled="saving" @click="close">{{ t('common.cancel') }}</t-button><t-button type="submit" :loading="saving">{{ t('common.save') }}</t-button></div>
    </form>
  </t-dialog>
  <t-dialog v-model:visible="discardVisible" :header="t('domains.discardTitle')" :footer="false" width="min(420px, 95vw)" @close="cancelDiscard">
    <p>{{ t('domains.discardHint') }}</p>
    <div class="domain-editor__actions"><t-button variant="outline" @click="cancelDiscard">{{ t('common.cancel') }}</t-button><t-button theme="danger" @click="discardChanges">{{ t('domains.discard') }}</t-button></div>
  </t-dialog>
</template>

<style scoped>
.domain-editor { display: grid; gap: 12px; max-height: 70vh; overflow-y: auto; padding: 2px; }
.domain-editor input, .domain-editor textarea { box-sizing: border-box; width: 100%; min-width: 0; padding: 8px 12px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: var(--td-bg-color-container); color: var(--td-text-color-primary); font: inherit; }
.domain-editor input:focus-visible, .domain-editor textarea:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.domain-editor input:disabled, .domain-editor textarea:disabled { background: var(--td-bg-color-component-disabled); color: var(--td-text-color-disabled); }
.domain-editor textarea { min-height: 120px; }
.domain-editor__actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
p[role="alert"] { color: var(--td-error-color); overflow-wrap: anywhere; }
</style>
