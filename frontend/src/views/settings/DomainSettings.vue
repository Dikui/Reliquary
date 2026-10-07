<template>
  <section class="domain-settings">
    <header class="domain-header">
      <div><h2>{{ t('knowledgeDomains.title') }}</h2><p>{{ t('knowledgeDomains.description') }}</p></div>
      <t-button v-if="canEdit && !draft" theme="primary" @click="startCreate">{{ t('knowledgeDomains.create') }}</t-button>
    </header>
    <t-alert v-if="error" theme="error" :message="error" role="alert" />
    <t-alert v-if="!canEdit" theme="info" :message="t('knowledgeDomains.readOnly')" />

    <template v-if="draft">
      <div class="editor-heading"><h3>{{ selectedId ? t('knowledgeDomains.edit') : t('knowledgeDomains.create') }}</h3><t-button variant="text" :disabled="saving" @click="closeEditor">{{ t('common.back') }}</t-button></div>
      <t-select v-if="selectedId" v-model="editorTab" class="domain-mobile-nav" :options="editorTabOptions" :aria-label="t('knowledgeDomains.edit')" :popup-props="{ zIndex: 6200 }" />
      <t-tabs v-model="editorTab" class="domain-editor-tabs">
        <t-tab-panel value="template" :label="t('knowledgeDomains.template')">
          <form novalidate class="domain-form" @submit.prevent="saveDomain">
            <label><span>{{ t('knowledgeDomains.name') }}</span><t-input v-model="draft.name" :disabled="!canEdit || saving" :maxlength="255" /></label>
            <label><span>{{ t('knowledgeDomains.domainDescription') }}</span><t-textarea v-model="draft.description" :disabled="!canEdit || saving" :autosize="{ minRows: 2, maxRows: 6 }" /></label>
            <t-alert v-if="selectedDomain?.is_builtin" theme="info" :message="t('knowledgeDomains.builtinHint')" />
            <DomainTemplateEditor v-model="draft" :disabled="!canEdit || saving" />
            <ModelTierEditor :model-value="draft.model_tiers || {}" scope="domain" :before-navigate="confirmLeave" :disabled="!canEdit || saving" @update:model-value="draft.model_tiers = $event" />
            <p v-if="editorError" class="inline-error" role="alert">{{ editorError }}</p>
            <div class="domain-actions"><t-button variant="outline" :disabled="saving" @click="closeEditor">{{ t('common.cancel') }}</t-button><t-button v-if="canEdit" theme="primary" type="submit" :loading="saving">{{ t('common.save') }}</t-button></div>
          </form>
        </t-tab-panel>
        <t-tab-panel v-if="selectedId" value="consolidation" :label="t('knowledgeDomains.consolidation')">
          <div class="consolidation-header"><p>{{ t('knowledgeDomains.consolidationHint') }}</p><t-button variant="outline" :loading="openFieldsLoading" @click="loadOpenFields">{{ t('common.refresh') }}</t-button></div>
          <t-alert v-if="openFieldsError" theme="error" :message="openFieldsError" />
          <t-table row-key="name" :data="pagedOpenFields" :columns="openFieldColumns" :loading="openFieldsLoading" :empty="t('knowledgeDomains.noOpenFields')">
            <template #name="{ row }"><code>{{ row.name }}</code></template>
            <template #example="{ row }"><div v-for="fact in row.example_facts || []" :key="fact.id" class="open-field-example"><strong>{{ formatValue(fact.value) }}{{ fact.unit ? ` ${fact.unit}` : '' }}</strong><q v-if="fact.quote">{{ fact.quote }}</q></div></template>
            <template #actions="{ row }"><div class="table-actions" v-if="canEdit"><t-button variant="text" :disabled="!selectedDomain?.fields.length" @click="startMapping(row.name)">{{ t('knowledgeDomains.map') }}</t-button><t-button variant="text" @click="startNewField(row.name)">{{ t('knowledgeDomains.newField') }}</t-button></div></template>
          </t-table>
          <t-pagination v-if="openFields.length > pageSize" v-model="openPage" :total="openFields.length" :page-size="pageSize" :show-page-size="false" />
        </t-tab-panel>
      </t-tabs>
    </template>
    <template v-else>
      <div class="domain-list-toolbar"><t-input v-model="search" clearable :placeholder="t('knowledgeDomains.search')" :aria-label="t('knowledgeDomains.search')" /><t-button variant="outline" :loading="loading" @click="loadDomains">{{ t('common.refresh') }}</t-button></div>
      <t-table row-key="id" :data="pagedDomains" :columns="domainColumns" :loading="loading" :empty="search ? t('knowledgeDomains.noResults') : t('knowledgeDomains.empty')">
        <template #name="{ row }"><div class="domain-name"><t-button variant="text" @click="openDomain(row)">{{ row.name }}</t-button><t-tag v-if="row.is_builtin" size="small" variant="light">{{ t('knowledgeDomains.builtin') }}</t-tag></div></template>
        <template #types="{ row }"><span>{{ (row.entity_types || []).join('、') || t('knowledgeDomains.allTypes') }}</span></template>
        <template #actions="{ row }"><div class="table-actions"><t-button variant="text" @click="openDomain(row)">{{ canEdit ? t('common.edit') : t('knowledgeDomains.view') }}</t-button><t-tooltip v-if="canEdit" :content="row.is_builtin ? t('knowledgeDomains.builtinHint') : t('knowledgeDomains.deleteHint')"><t-button theme="danger" variant="text" :disabled="row.is_builtin || deletingId === row.id" :loading="deletingId === row.id" @click="confirmRemove(row)">{{ t('common.delete') }}</t-button></t-tooltip></div></template>
      </t-table>
      <t-pagination v-if="filteredDomains.length > pageSize" v-model="page" :total="filteredDomains.length" :page-size="pageSize" :show-page-size="false" />
    </template>

    <t-dialog v-model:visible="mappingVisible" :header="t('knowledgeDomains.mapTitle', { name: activeOpenField })" :footer="false" :close-on-overlay-click="false" :z-index="6100" width="min(480px, 95vw)">
      <div class="mapping-form"><p>{{ t('knowledgeDomains.consolidationHint') }}</p><label><span>{{ t('knowledgeDomains.targetField') }}</span><t-select v-model="mappingTarget" filterable :options="mappingOptions" :disabled="mappingSaving" :popup-props="{ zIndex: 6200 }" /></label><p v-if="mappingError" class="inline-error" role="alert">{{ mappingError }}</p><div class="domain-actions"><t-button variant="outline" :disabled="mappingSaving" @click="mappingVisible = false">{{ t('common.cancel') }}</t-button><t-button theme="primary" :loading="mappingSaving" :disabled="!mappingTarget" @click="saveMapping">{{ t('knowledgeDomains.map') }}</t-button></div></div>
    </t-dialog>
    <t-dialog v-model:visible="newFieldVisible" :header="t('knowledgeDomains.newFieldTitle', { name: activeOpenField })" :footer="false" :close-on-overlay-click="false" :z-index="6100" width="min(660px, 95vw)">
      <div class="mapping-form"><p>{{ t('knowledgeDomains.newFieldHint') }}</p><DomainFieldEditor v-model="newField" :entity-types="selectedDomain?.entity_types || []" :unit-families="selectedDomain?.unit_families || []" :disabled="mappingSaving" /><p v-if="mappingError" class="inline-error" role="alert">{{ mappingError }}</p><div class="domain-actions"><t-button variant="outline" :disabled="mappingSaving" @click="newFieldVisible = false">{{ t('common.cancel') }}</t-button><t-button theme="primary" :loading="mappingSaving" :disabled="!newField.key.trim()" @click="saveNewField">{{ t('knowledgeDomains.newField') }}</t-button></div></div>
    </t-dialog>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useConfirmDelete } from '@/components/settings/useConfirmDelete'
import { createDomainField, createKnowledgeDomain, deleteKnowledgeDomain, getKnowledgeDomain, listDomainOpenFields, listKnowledgeDomains, mapDomainOpenField, updateKnowledgeDomain, type DomainEnvelope, type DomainOpenField, type KnowledgeDomain, type KnowledgeDomainInput } from '@/api/knowledge-domains'
import ModelTierEditor from '@/components/ModelTierEditor.vue'
import DomainFieldEditor from './DomainFieldEditor.vue'
import DomainTemplateEditor from './DomainTemplateEditor.vue'
import { copyDomainInput, emptyDomainField, emptyKnowledgeDomain, templateFieldFromOpenField, validateDomainInput } from './domainTemplate'
import { domainErrorMessage } from './domainErrors'
const { t } = useI18n()
const auth = useAuthStore()
const confirmDelete = useConfirmDelete()
const canEdit = computed(() => auth.hasRole('admin'))
const domains = ref<KnowledgeDomain[]>([])
const loading = ref(false)
const error = ref('')
const editorError = ref('')
const search = ref('')
const page = ref(1)
const pageSize = 10
const selectedId = ref('')
const selectedDomain = computed(() => domains.value.find(domain => domain.id === selectedId.value))
const draft = ref<KnowledgeDomainInput | null>(null)
const savedDraft = ref('')
const editorTab = ref('template')
const editorTabOptions = computed(() => [
  { value: 'template', label: t('knowledgeDomains.template') },
  { value: 'consolidation', label: t('knowledgeDomains.consolidation') },
])
const saving = ref(false)
const deletingId = ref('')
const openFields = ref<DomainOpenField[]>([])
const openFieldsLoading = ref(false)
const openFieldsError = ref('')
const openPage = ref(1)
const mappingVisible = ref(false)
const newFieldVisible = ref(false)
const mappingTarget = ref('')
const mappingSaving = ref(false)
const mappingError = ref('')
const activeOpenField = ref('')
const newField = ref(emptyDomainField())
let listRequest = 0
let fieldsRequest = 0
const filteredDomains = computed(() => domains.value.filter(domain => `${domain.name} ${domain.description}`.toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase())))
const pagedDomains = computed(() => filteredDomains.value.slice((page.value - 1) * pageSize, page.value * pageSize))
const pagedOpenFields = computed(() => openFields.value.slice((openPage.value - 1) * pageSize, openPage.value * pageSize))
const mappingOptions = computed(() => (selectedDomain.value?.fields || []).map(field => ({ value: field.key, label: `${field.label || field.key} (${field.key})` })))
const domainColumns = computed(() => [{ colKey: 'name', title: t('knowledgeDomains.name'), width: 180 }, { colKey: 'description', title: t('knowledgeDomains.domainDescription') }, { colKey: 'types', title: t('knowledgeDomains.entityTypes') }, { colKey: 'actions', title: t('knowledgeDomains.actions'), width: 160 }])
const openFieldColumns = computed(() => [{ colKey: 'name', title: t('knowledgeDomains.openField'), width: 160 }, { colKey: 'count', title: t('knowledgeDomains.count'), width: 90 }, { colKey: 'example', title: t('knowledgeDomains.examples') }, ...(canEdit.value ? [{ colKey: 'actions', title: t('knowledgeDomains.actions'), width: 180 }] : [])])
function data<T>(response: DomainEnvelope<T>): T { if (!response.success) throw new Error(response.error || response.message || t('knowledgeDomains.failed')); return response.data }
function errorMessage(cause: unknown): string {
  return domainErrorMessage(cause, t('knowledgeDomains.failed'), count => t('knowledgeDomains.deleteReferenced', { count }, count))
}
function formatValue(value: unknown): string { return typeof value === 'string' ? value : JSON.stringify(value) || t('knowledgeWorkbench.emptyValue') }
async function loadDomains() {
  const request = ++listRequest
  loading.value = true
  error.value = ''
  try { const result = data(await listKnowledgeDomains()); if (request === listRequest) { domains.value = result || []; page.value = Math.min(page.value, Math.max(1, Math.ceil(filteredDomains.value.length / pageSize))) } }
  catch (cause) { if (request === listRequest) error.value = errorMessage(cause) }
  finally { if (request === listRequest) loading.value = false }
}
function startCreate() { if (!canEdit.value) return; selectedId.value = ''; draft.value = emptyKnowledgeDomain(); savedDraft.value = JSON.stringify(draft.value); editorTab.value = 'template'; editorError.value = '' }
function openDomain(domain: KnowledgeDomain) { selectedId.value = domain.id; draft.value = copyDomainInput(domain); savedDraft.value = JSON.stringify(draft.value); editorTab.value = 'template'; editorError.value = ''; openFields.value = []; openPage.value = 1 }
function discardEditor() { fieldsRequest++; draft.value = null; selectedId.value = ''; editorError.value = '' }
function confirmLeave(): Promise<boolean> {
  if (saving.value) return Promise.resolve(false)
  if (!canEdit.value || !draft.value || JSON.stringify(draft.value) === savedDraft.value) return Promise.resolve(true)
  return new Promise(resolve => {
    const dialog = confirmDelete({ title: t('knowledgeDomains.discardTitle'), body: t('knowledgeDomains.discardHint'), confirmText: t('knowledgeDomains.discard'), onConfirm: () => { discardEditor(); resolve(true) } })
    dialog.update({ onCancel: () => { dialog.hide(); resolve(false) }, onClose: () => resolve(false) })
  })
}
async function closeEditor() { if (await confirmLeave()) discardEditor() }
defineExpose({ confirmLeave })
async function saveDomain() {
  if (!draft.value || !canEdit.value || saving.value) return
  const invalid = validateDomainInput(draft.value)
  if (invalid) { editorError.value = t(`knowledgeDomains.validation.${invalid}`); return }
  saving.value = true; editorError.value = ''
  try { const input = copyDomainInput(draft.value); data(selectedId.value ? await updateKnowledgeDomain(selectedId.value, input) : await createKnowledgeDomain(input)); await loadDomains(); draft.value = null; selectedId.value = ''; MessagePlugin.success(t('knowledgeDomains.saved')) }
  catch (cause) { editorError.value = errorMessage(cause) }
  finally { saving.value = false }
}
function confirmRemove(domain: KnowledgeDomain) {
  if (!canEdit.value || domain.is_builtin || deletingId.value) return
  confirmDelete({ title: t('knowledgeDomains.deleteTitle'), body: t('knowledgeDomains.deleteConfirm', { name: domain.name }), onConfirm: async () => { deletingId.value = domain.id; error.value = ''; try { data(await deleteKnowledgeDomain(domain.id)); await loadDomains(); MessagePlugin.success(t('knowledgeDomains.deleted')) } catch (cause) { error.value = errorMessage(cause) } finally { deletingId.value = '' } } })
}
async function loadOpenFields() {
  if (!selectedId.value) return
  const id = selectedId.value, request = ++fieldsRequest
  openFieldsLoading.value = true; openFieldsError.value = ''
  try { const result = data(await listDomainOpenFields(id)); if (request === fieldsRequest && id === selectedId.value) { openFields.value = result || []; openPage.value = Math.min(openPage.value, Math.max(1, Math.ceil(openFields.value.length / pageSize))) } }
  catch (cause) { if (request === fieldsRequest) openFieldsError.value = errorMessage(cause) }
  finally { if (request === fieldsRequest) openFieldsLoading.value = false }
}
function startMapping(name: string) { if (!canEdit.value) return; activeOpenField.value = name; mappingTarget.value = ''; mappingError.value = ''; mappingVisible.value = true }
function startNewField(name: string) { if (!canEdit.value) return; activeOpenField.value = name; newField.value = emptyDomainField(name); newField.value.aliases = [name]; mappingError.value = ''; newFieldVisible.value = true }
async function refreshSelectedDomain() { const updated = data(await getKnowledgeDomain(selectedId.value)); domains.value = domains.value.map(domain => domain.id === updated.id ? updated : domain); draft.value = copyDomainInput(updated); savedDraft.value = JSON.stringify(draft.value); await loadOpenFields() }
async function saveMapping() {
  if (!canEdit.value || !mappingTarget.value || mappingSaving.value) return
  mappingSaving.value = true; mappingError.value = ''
  try { data(await mapDomainOpenField(selectedId.value, activeOpenField.value, mappingTarget.value)); await refreshSelectedDomain(); mappingVisible.value = false; MessagePlugin.success(t('knowledgeDomains.mapped')) }
  catch (cause) { mappingError.value = errorMessage(cause) }
  finally { mappingSaving.value = false }
}
async function saveNewField() {
  if (!canEdit.value || !selectedDomain.value || mappingSaving.value) return
  const field = templateFieldFromOpenField(newField.value, activeOpenField.value)
  const invalid = validateDomainInput({ ...copyDomainInput(selectedDomain.value), fields: [...selectedDomain.value.fields, field] })
  if (invalid) { mappingError.value = t(`knowledgeDomains.validation.${invalid}`); return }
  mappingSaving.value = true; mappingError.value = ''
  try { data(await createDomainField(selectedId.value, field)); await refreshSelectedDomain(); newFieldVisible.value = false; MessagePlugin.success(t('knowledgeDomains.fieldCreated')) }
  catch (cause) { mappingError.value = errorMessage(cause) }
  finally { mappingSaving.value = false }
}
watch(search, () => { page.value = 1 })
watch(editorTab, tab => { if (tab === 'consolidation') { if (JSON.stringify(draft.value) !== savedDraft.value) { editorTab.value = 'template'; editorError.value = t('knowledgeDomains.saveBeforeConsolidation') } else void loadOpenFields() } })
onMounted(loadDomains)
onBeforeUnmount(() => { listRequest++; fieldsRequest++ })
</script>
<style scoped lang="less">
.domain-settings { color: var(--td-text-color-primary); display: flex; flex-direction: column; gap: 18px; min-width: 0; }
.domain-mobile-nav { display: none; }
.domain-header, .editor-heading, .domain-list-toolbar, .consolidation-header, .domain-name, .table-actions { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
.domain-header h2 { margin: 0; font-size: 20px; }
.domain-header p, .consolidation-header p { color: var(--td-text-color-secondary); line-height: 1.6; }
.domain-header > div, .domain-name { min-width: 0; }
.domain-list-toolbar :deep(.t-input__wrap) { max-width: 340px; }
.domain-form, .mapping-form { display: flex; flex-direction: column; gap: 18px; padding-top: 18px; }
.domain-form > label, .mapping-form > label { display: flex; flex-direction: column; gap: 6px; }
.domain-actions { display: flex; justify-content: flex-end; gap: 10px; padding: 14px 0; }
.inline-error { margin: 0; color: var(--td-error-color); min-height: 22px; }
:deep(.t-table) { min-height: 320px; }
.open-field-example { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; overflow-wrap: anywhere; }
.open-field-example q { color: var(--td-text-color-secondary); font-size: 12px; }
code { overflow-wrap: anywhere; }
:deep(textarea) { resize: none; }
@media (max-width: 600px) {
  .domain-header, .consolidation-header { align-items: flex-start; flex-direction: column; }
  .domain-list-toolbar { flex-wrap: wrap; }
  .domain-mobile-nav { display: block; }
  .domain-editor-tabs > :deep(.t-tabs__header) { display: none; }
  .domain-actions { flex-wrap: wrap; }
  :deep(.t-table__content) { overflow-x: auto; }
}
</style>
