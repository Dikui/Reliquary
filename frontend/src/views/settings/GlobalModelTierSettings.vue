<template>
  <section class="global-model-tier-settings">
    <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button variant="text" :loading="loading" @click="load">{{ t('modelTiers.retry') }}</t-button></template></t-alert>
    <t-loading :loading="loading" show-overlay><form novalidate @submit.prevent="save"><ModelTierEditor v-model="draft" scope="global" :before-navigate="confirmLeave" :disabled="loading || saving" @refresh="load" /><div class="tier-actions"><t-button variant="outline" :disabled="!dirty || saving || loading" @click="reset">{{ t('common.cancel') }}</t-button><t-button theme="primary" type="submit" :loading="saving" :disabled="!dirty || loading">{{ t('common.save') }}</t-button></div></form></t-loading>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { getModelTierSettings, updateGlobalModelTiers, type ModelTierConfig } from '@/api/model-tiers'
import { copyModelTiers } from '@/utils/modelTiers'
import { useConfirmDelete } from '@/components/settings/useConfirmDelete'
import ModelTierEditor from '@/components/ModelTierEditor.vue'
const { t } = useI18n()
const confirmDelete = useConfirmDelete()
const draft = ref<ModelTierConfig>(copyModelTiers()), saved = ref<ModelTierConfig>(copyModelTiers())
const loading = ref(false), saving = ref(false), error = ref('')
const dirty = computed(() => JSON.stringify(copyModelTiers(draft.value)) !== JSON.stringify(saved.value))
let request = 0
async function load() {
  if (saving.value) return
  if (dirty.value) { confirmDelete({ title: t('knowledgeDomains.discardTitle'), body: t('knowledgeDomains.discardHint'), confirmText: t('knowledgeDomains.discard'), onConfirm: async () => { reset(); await load() } }); return }
  const current = ++request
  loading.value = true; error.value = ''
  try { const response = await getModelTierSettings(); if (!response.success) throw new Error(response.error || response.message || t('modelTiers.loadFailed')); if (current === request) { saved.value = copyModelTiers(response.data?.overrides); reset() } }
  catch (cause) { if (current === request) error.value = cause instanceof Error ? cause.message : t('modelTiers.loadFailed') }
  finally { if (current === request) loading.value = false }
}
function reset() { if (!saving.value) draft.value = copyModelTiers(saved.value) }
async function save() {
  if (saving.value || loading.value || !dirty.value) return
  const current = request
  saving.value = true; error.value = ''
  try { const response = await updateGlobalModelTiers(copyModelTiers(draft.value)); if (!response.success) throw new Error(response.error || response.message || t('modelTiers.saveFailed')); if (current !== request) return; saved.value = copyModelTiers(response.data?.overrides || draft.value); draft.value = copyModelTiers(saved.value); MessagePlugin.success(t('modelTiers.saved')) }
  catch (cause) { error.value = cause instanceof Error ? cause.message : t('modelTiers.saveFailed') }
  finally { saving.value = false }
}
function confirmLeave(): Promise<boolean> { if (saving.value) return Promise.resolve(false); if (!dirty.value) return Promise.resolve(!saving.value); return new Promise(resolve => { const dialog = confirmDelete({ title: t('knowledgeDomains.discardTitle'), body: t('knowledgeDomains.discardHint'), confirmText: t('knowledgeDomains.discard'), onConfirm: () => { reset(); resolve(true) } }); dialog.update({ onCancel: () => { dialog.hide(); resolve(false) }, onClose: () => resolve(false) }) }) }
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
defineExpose({ confirmLeave })
onMounted(() => { void load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { request++; window.removeEventListener('beforeunload', beforeUnload) })
</script>
<style scoped lang="less">
.global-model-tier-settings { margin-top: 28px; padding-top: 22px; border-top: 1px solid var(--td-component-stroke); min-height: 480px; }
.tier-actions { display: flex; justify-content: flex-end; gap: 10px; padding-top: 20px; }
</style>
