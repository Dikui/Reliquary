<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { deleteDomain, listDomains, type Domain } from '@/api/domains'
import DomainEditor from './DomainEditor.vue'

const { t } = useI18n()
const domains = ref<Domain[]>([]), loading = ref(false), error = ref('')
const editorVisible = ref(false), editing = ref<Domain | null>(null)
const deleting = ref<Domain | null>(null), deleteBusy = ref(false), deleteError = ref('')
let request = 0, active = true
async function load() {
  const current = ++request
  loading.value = true; error.value = ''
  try {
    const response = await listDomains()
    if (!response.success) throw new Error(response.error || t('domains.requestFailed'))
    if (current === request) domains.value = response.data
  } catch (cause) { if (current === request) error.value = cause instanceof Error ? cause.message : t('domains.requestFailed') }
  finally { if (current === request) loading.value = false }
}
function edit(domain: Domain | null) { editing.value = domain; editorVisible.value = true }
async function saved() { editorVisible.value = false; await load() }
function askDelete(domain: Domain) { deleting.value = domain; deleteError.value = '' }
async function confirmDelete() {
  if (!deleting.value || deleteBusy.value) return
  deleteBusy.value = true; deleteError.value = ''
  try {
    const response = await deleteDomain(deleting.value.id)
    if (!active) return
    if (!response.success) throw new Error(response.error || t('domains.requestFailed'))
    deleting.value = null; MessagePlugin.success(t('domains.deleted')); await load()
  } catch (cause) { if (active) deleteError.value = cause instanceof Error ? cause.message : t('domains.requestFailed') }
  finally { if (active) deleteBusy.value = false }
}
onMounted(() => { void load() })
onBeforeUnmount(() => { active = false; request++ })
</script>

<template>
  <section class="domains-manager">
    <header><div><h2>{{ t('domains.title') }}</h2><p>{{ t('domains.description') }}</p></div><t-button @click="edit(null)">{{ t('domains.create') }}</t-button></header>
    <p v-if="error" role="alert">{{ error }} <t-button variant="text" :loading="loading" @click="load">{{ t('common.retry') }}</t-button></p>
    <t-loading :loading="loading" show-overlay>
      <t-empty v-if="!domains.length && !loading && !error" :description="t('domains.empty')" />
      <ul><li v-for="domain in domains" :key="domain.id"><div><h3>{{ domain.name }}</h3><p>{{ domain.definition }}</p></div><div class="domains-manager__actions"><t-button variant="outline" @click="edit(domain)">{{ t('domains.edit') }}</t-button><t-button theme="danger" variant="text" @click="askDelete(domain)">{{ t('domains.delete') }}</t-button></div></li></ul>
    </t-loading>
    <DomainEditor v-if="editorVisible" :domain="editing" @close="editorVisible = false" @saved="saved" />
    <t-dialog :visible="!!deleting" :header="t('domains.delete')" :footer="false" :close-on-overlay-click="false" :close-btn="!deleteBusy" :close-on-esc-keydown="!deleteBusy" width="min(480px, 95vw)" @close="!deleteBusy && (deleting = null)">
      <p>{{ t('domains.deleteHint', { name: deleting?.name }) }}</p>
      <p v-if="deleteError" role="alert">{{ deleteError }}</p>
      <div class="domains-manager__actions"><t-button variant="outline" :disabled="deleteBusy" @click="deleting = null">{{ t('common.cancel') }}</t-button><t-button theme="danger" :loading="deleteBusy" @click="confirmDelete">{{ t('domains.delete') }}</t-button></div>
    </t-dialog>
  </section>
</template>

<style scoped>
.domains-manager { padding: 24px; color: var(--td-text-color-primary); }
header, li { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; }
h2 { font-size: 20px; margin: 0 0 8px; }
h3 { margin: 0 0 8px; font-size: 16px; }
p { color: var(--td-text-color-secondary); line-height: 1.65; overflow-wrap: anywhere; }
ul { list-style: none; padding: 0; min-height: 120px; }
li { padding: 20px 0; border-bottom: 1px solid var(--td-component-stroke); }
li > div { min-width: 0; overflow-wrap: anywhere; }
.domains-manager__actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; flex-shrink: 0; }
p[role="alert"] { color: var(--td-error-color); }
@media (max-width: 720px) { .domains-manager { padding: 16px; } header, li { flex-direction: column; gap: 12px; } }
</style>
