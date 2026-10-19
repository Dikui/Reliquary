<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin } from 'tdesign-vue-next'
import { listLibraries, getCatalogLibrary, createCatalogLibrary, saveCatalogLibrary, deleteCatalogLibrary, mergeLibraryRelationTypes, type LibraryCatalogItem, type LibraryTemplate } from '@/api/library'
import { LIBRARY_TEMPLATES, validDirectory, validLibraryName, validTypes, librarySettingsPayload, moveType, catalogDescription } from '@/utils/libraryCatalogForm'
import SafeTextarea from '@/components/SafeTextarea.vue'
import ModelTierEditor from '@/components/ModelTierEditor.vue'
import { copyModelTiers } from '@/utils/modelTiers'
import type { ModelTier } from '@/types/modelTiers'
import LibraryChunkingSettings from '@/views/library/settings/LibraryChunkingSettings.vue'
const { t, locale } = useI18n()
const emit = defineEmits<{ changed: [] }>()
const libraries = ref<LibraryCatalogItem[]>([]), draft = ref<LibraryCatalogItem | null>(null), saved = ref(''), error = ref(''), loading = ref(false), saving = ref(false)
const creating = ref(false), newLibrary = ref({ template: 'external_timely' as LibraryTemplate, directory: '', name: '', description: '' })
const fromType = ref(''), toType = ref('')
let sequence = 0
const dirty = computed(() => !!draft.value && JSON.stringify(draft.value) !== saved.value)
const systemRoleName = computed(() => {
  switch (draft.value?.system_role) {
    case 'official': return t('ikb18.systemOfficial')
    case 'media': return t('ikb18.systemMedia')
    case 'references': return t('ikb18.systemReferences')
    case 'personal': return t('ikb18.systemPersonal')
    case 'ai-output': return t('ikb18.systemAIOutput')
    default: return t('ikb18.custom')
  }
})
const relationOptions = computed(() => (draft.value?.relation_types || []).filter(row => row.enabled).map(row => ({ value: row.name, label: row.name })))
const hasGraph = computed(() => draft.value?.processing_model_tiers?.includes('entity_resolution') || false)
const modelTiers = computed<ModelTier[]>(() => [...(draft.value?.processing_model_tiers || []), 'rerank'])
const typeSections = computed<Array<'content_types' | 'relation_types'>>(() => draft.value?.template === 'generated' ? [] : hasGraph.value ? ['content_types', 'relation_types'] : ['content_types'])
const chunkingConfig = computed(() => {
  const c = draft.value?.chunking_config
  return c ? { chunkSize: c.chunk_size, chunkOverlap: c.chunk_overlap, separators: c.separators, enableParentChild: c.enable_parent_child, parentChunkSize: c.parent_chunk_size, childChunkSize: c.child_chunk_size, strategy: c.strategy, tokenLimit: c.token_limit, languages: c.languages } : null
})
function updateChunking(c: { chunkSize: number; chunkOverlap: number; separators: string[]; enableParentChild: boolean; parentChunkSize: number; childChunkSize: number; strategy?: string; tokenLimit?: number; languages?: string[] }) {
  if (!draft.value || saving.value || loading.value) return
  draft.value.chunking_config = { ...draft.value.chunking_config, chunk_size: c.chunkSize, chunk_overlap: c.chunkOverlap, separators: c.separators, enable_parent_child: c.enableParentChild, parent_chunk_size: c.parentChunkSize, child_chunk_size: c.childChunkSize, strategy: c.strategy, token_limit: c.tokenLimit, languages: c.languages }
}
async function load(id?: string) {
  const current = ++sequence; loading.value = true; error.value = ''
  try {
    const catalog = await listLibraries(); if (!catalog.success) throw new Error(catalog.message)
    if (current !== sequence) return
    libraries.value = catalog.data || []
    const selected = id || draft.value?.id || libraries.value[0]?.id
    if (!selected) { draft.value = null; return }
    const response = await getCatalogLibrary(selected); if (!response.success) throw new Error(response.message)
    if (current !== sequence) return
    draft.value = { ...response.data, model_tiers: copyModelTiers({ ...response.data.model_tiers, generation: response.data.model_tiers?.generation || response.data.summary_model_id || '' }), description: catalogDescription(response.data, t('ikb18.systemMediaDescription')), content_types: response.data.content_types || [], relation_types: response.data.relation_types || [] }
    saved.value = JSON.stringify(draft.value); fromType.value = ''; toType.value = ''
  } catch (cause: any) { if (current === sequence) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === sequence) loading.value = false }
}
function switchLibrary(id: string) {
  if (saving.value || loading.value) return
  if (!dirty.value) { void load(id); return }
  const dialog = DialogPlugin.confirm({ header: t('ikb18.catalog'), body: t('ikb18.unsaved'), onConfirm: () => { dialog.destroy(); void load(id) } })
}
async function save() {
  if (!draft.value || saving.value) return
  if (!validLibraryName(draft.value.name)) { error.value = t('ikb18.invalidName'); return }
  if (!validTypes(draft.value.content_types) || !validTypes(draft.value.relation_types)) { error.value = t('ikb18.invalidTypes'); return }
  saving.value = true; error.value = ''
  try { const response = await saveCatalogLibrary(draft.value.id, librarySettingsPayload(draft.value)); if (!response.success) throw new Error(response.message); await load(draft.value.id); emit('changed') }
  catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
  finally { saving.value = false }
}
async function create() {
  if (saving.value) return
  if (!validDirectory(newLibrary.value.directory)) { error.value = t('ikb18.invalidDirectory'); return }
  if (!validLibraryName(newLibrary.value.name)) { error.value = t('ikb18.invalidName'); return }
  saving.value = true; error.value = ''
  try { const response = await createCatalogLibrary({ ...newLibrary.value, name: newLibrary.value.name.trim() }); if (!response.success) throw new Error(response.message); creating.value = false; await load(response.data.id) }
  catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
  finally { saving.value = false }
}
function confirmAction(header: string, body: string, action: () => Promise<void>) {
  const dialog = DialogPlugin.confirm({ header, body, closeOnOverlayClick: false, onConfirm: async () => {
    if (saving.value) return
    saving.value = true; error.value = ''; dialog.update({ confirmBtn: { content: t('common.confirm'), loading: true }, closeBtn: false, closeOnEscKeydown: false, cancelBtn: { content: t('common.cancel'), disabled: true } })
    try { await action(); dialog.destroy() } catch (cause: any) { error.value = cause.message || t('common.operationFailed'); dialog.update({ body: error.value, confirmBtn: { content: t('common.retry'), loading: false }, closeBtn: true, closeOnEscKeydown: true, cancelBtn: { content: t('common.cancel'), disabled: false } }) }
    finally { saving.value = false }
  } })
}
function merge() {
  if (!draft.value || !fromType.value || !toType.value || fromType.value === toType.value || dirty.value) return
  const id = draft.value.id, from = fromType.value, to = toType.value
  confirmAction(t('ikb18.merge'), `${from} → ${to}. ${t('ikb18.mergeHint')}`, async () => { const result = await mergeLibraryRelationTypes(id, from, to); if (!result.success) throw new Error(result.message); await load(id) })
}
function removeLibrary() {
  if (!draft.value || draft.value.system_role || draft.value.entry_count) return
  const id = draft.value.id
  confirmAction(t('ikb18.deleteLibrary'), t('ikb18.deleteConfirm'), async () => { const result = await deleteCatalogLibrary(id); if (!result.success) throw new Error(result.message); draft.value = null; await load() })
}
function confirmLeave(): Promise<boolean> {
  if (saving.value) return Promise.resolve(false)
  if (!dirty.value) return Promise.resolve(true)
  return new Promise(resolve => { const dialog = DialogPlugin.confirm({ header: t('ikb18.catalog'), body: t('ikb18.unsaved'), onConfirm: () => { draft.value = JSON.parse(saved.value); dialog.destroy(); resolve(true) }, onCancel: () => { dialog.destroy(); resolve(false) }, onClose: () => { dialog.destroy(); resolve(false) } }) })
}
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
watch(locale, async () => { const current = sequence; try { const response = await listLibraries(); if (current === sequence && response.success) libraries.value = response.data || [] } catch { /* Preserve edited settings when localized names cannot be reloaded. */ } })
defineExpose({ confirmLeave })
onMounted(() => { void load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { sequence++; window.removeEventListener('beforeunload', beforeUnload) })
</script>
<template>
  <section class="library-catalog">
    <div class="catalog-toolbar"><t-select :value="draft?.id" :options="libraries.map(item => ({ value: item.id, label: item.display_name || item.name }))" :disabled="saving || loading" :aria-label="t('ikb18.catalog')" @change="switchLibrary($event as string)" /><t-button :disabled="saving || dirty" @click="creating = true; error = ''">{{ t('ikb18.create') }}</t-button></div>
    <t-alert v-if="error" theme="error" :message="error"><template #operation><t-button v-if="!draft" variant="text" @click="load()">{{ t('common.retry') }}</t-button></template></t-alert>
    <t-loading :loading="loading" show-overlay>
      <form v-if="draft" class="catalog-form" novalidate @submit.prevent="save">
        <fieldset :disabled="saving || loading">
          <dl class="readonly-fields"><dt>{{ t('ikb18.template') }}</dt><dd>{{ t(`ikb18.${draft.template}`) }}</dd><dt>{{ t('ikb18.directory') }}</dt><dd><code>{{ draft.directory }}/</code></dd><dt>{{ t('ikb18.systemRole') }}</dt><dd>{{ systemRoleName }}</dd></dl>
          <p>{{ t(`ikb18.${draft.template}Hint`) }}</p>
          <label>{{ t('ikb18.name') }}<t-input :disabled="saving || loading" v-model="draft.name" :maxlength="255" :aria-label="t('ikb18.name')" /></label>
          <label>{{ t('ikb18.description') }}<SafeTextarea :disabled="saving || loading" v-model="draft.description" :autosize="{ minRows: 2, maxRows: 8 }" /></label>
          <template v-if="draft.template !== 'generated'">
            <label>{{ t('ikb18.review') }}<SafeTextarea :disabled="saving || loading" v-model="draft.review_criteria" :aria-label="t('ikb18.review')" :autosize="{ minRows: 3, maxRows: 12 }" /></label>
            <t-button variant="text" :disabled="saving || loading || !draft.default_review_criteria || draft.review_criteria === draft.default_review_criteria" @click="draft.review_criteria = draft.default_review_criteria">{{ t('ikb18.useRecommendedReview') }}</t-button>
          </template>
          <p v-else>{{ draft.default_review_criteria }}</p>
          <p>{{ t(hasGraph ? 'ikb18.processingWithGraph' : draft.template === 'generated' ? 'ikb18.indexOnly' : 'ikb18.processingWithoutGraph') }}</p>
          <ModelTierEditor v-model="draft.model_tiers" :tiers="modelTiers" :before-navigate="confirmLeave" :disabled="saving || loading" />
          <div v-if="chunkingConfig" :key="draft.id" :inert="saving || loading"><p>{{ t('ikb18.parentChildFixed') }}</p><LibraryChunkingSettings :config="chunkingConfig" embedded parent-child-locked @update:config="updateChunking" /></div>
          <label v-if="draft.template === 'external_timely'">{{ t('ikb18.evidence') }}<t-select :disabled="saving || loading" v-model="draft.source_evidence_rule" :options="[{ value: 'link_required', label: t('ikb18.linkRequired') }, { value: 'link_or_publisher_date', label: t('ikb18.linkOrDate') }]" /></label>
          <label class="toggle-label">{{ t('ikb18.feed') }}<t-switch :disabled="saving || loading" v-model="draft.feed_enabled" :aria-label="t('ikb18.feed')" /></label>
          <section v-for="kind in typeSections" :key="kind">
            <h3>{{ t(kind === 'content_types' ? 'ikb18.contentTypes' : 'ikb18.relationTypes') }}</h3>
            <div v-for="(row, index) in draft[kind]" :key="index" class="type-row">
              <t-input :disabled="saving || loading" v-model="row.name" :aria-label="t('ikb18.name')" :maxlength="100" />
              <t-checkbox :disabled="saving || loading" v-model="row.enabled">{{ t('ikb18.enabled') }}</t-checkbox>
              <t-checkbox :disabled="saving || loading" v-if="hasGraph && 'extract_graph' in row" v-model="row.extract_graph">{{ t('ikb18.graph') }}</t-checkbox>
              <div class="type-actions"><t-button variant="text" :disabled="index === 0" @click="moveType(draft![kind], index, -1)">{{ t('ikb18.up') }}</t-button><t-button variant="text" :disabled="index === draft[kind].length - 1" @click="moveType(draft![kind], index, 1)">{{ t('ikb18.down') }}</t-button><t-button variant="text" theme="danger" @click="draft![kind].splice(index, 1)">{{ t('ikb18.remove') }}</t-button></div>
            </div>
            <t-button variant="outline" @click="kind === 'content_types' ? draft!.content_types.push({ name: '', position: draft!.content_types.length, enabled: true, extract_graph: false }) : draft!.relation_types.push({ name: '', position: draft!.relation_types.length, enabled: true })">{{ t('ikb18.add') }}</t-button>
          </section>
          <div class="catalog-actions"><t-button type="submit" :loading="saving" :disabled="!dirty">{{ t('common.save') }}</t-button><t-button variant="outline" :disabled="!dirty" @click="draft = JSON.parse(saved); error = ''">{{ t('common.cancel') }}</t-button></div>
          <section v-if="hasGraph"><h3>{{ t('ikb18.merge') }}</h3><p>{{ t('ikb18.mergeHint') }}</p><div class="catalog-toolbar"><t-select :disabled="saving || loading" v-model="fromType" :options="relationOptions" :placeholder="t('ikb18.from')" :aria-label="t('ikb18.from')" /><t-select :disabled="saving || loading" v-model="toType" :options="relationOptions.filter(row => row.value !== fromType)" :placeholder="t('ikb18.to')" :aria-label="t('ikb18.to')" /><t-button :disabled="dirty || !fromType || !toType || fromType === toType" @click="merge">{{ t('ikb18.mergeConfirm') }}</t-button></div></section>
          <p>{{ t('ikb18.deleteHint') }}</p><t-button v-if="!draft.system_role" theme="danger" variant="outline" :disabled="!!draft.entry_count" @click="removeLibrary">{{ t('ikb18.deleteLibrary') }}</t-button>
        </fieldset>
      </form>
    </t-loading>
    <t-dialog v-model:visible="creating" :header="t('ikb18.create')" :footer="false" :close-btn="!saving" :close-on-esc-keydown="!saving" :close-on-overlay-click="false" width="min(560px, 95vw)">
      <form class="catalog-form" novalidate @submit.prevent="create"><fieldset :disabled="saving">
        <label>{{ t('ikb18.template') }}<t-select :disabled="saving || loading" v-model="newLibrary.template" :options="LIBRARY_TEMPLATES.map(value => ({ value, label: t(`ikb18.${value}`) }))" /></label><p>{{ t(`ikb18.${newLibrary.template}Hint`) }}</p>
        <dl class="readonly-fields"><template v-for="value in LIBRARY_TEMPLATES" :key="value"><dt>{{ t(`ikb18.${value}`) }}</dt><dd>{{ t(`ikb18.${value}Hint`) }}</dd></template></dl>
        <label>{{ t('ikb18.directory') }}<t-input :disabled="saving || loading" v-model="newLibrary.directory" :maxlength="100" /></label><p>{{ t('ikb18.invalidDirectory') }}</p>
        <label>{{ t('ikb18.name') }}<t-input :disabled="saving || loading" v-model="newLibrary.name" :maxlength="255" /></label>
        <label>{{ t('ikb18.description') }}<SafeTextarea :disabled="saving || loading" v-model="newLibrary.description" :autosize="{ minRows: 2, maxRows: 6 }" /></label>
        <t-alert v-if="error" theme="error" :message="error" /><div class="catalog-actions"><t-button type="submit" :loading="saving">{{ t('common.create') }}</t-button><t-button variant="outline" @click="creating = false">{{ t('common.cancel') }}</t-button></div>
      </fieldset></form>
    </t-dialog>
  </section>
</template>
<style scoped>
.library-catalog { color: var(--td-text-color-primary); }
.catalog-toolbar, .catalog-actions, .type-actions { display: flex; gap: 12px; flex-wrap: wrap; align-items: center; margin-block: 16px; }
.catalog-toolbar > :deep(.t-select__wrap) { min-width: 160px; flex: 1; }
.catalog-form fieldset { border: 0; padding: 0; margin: 0; min-width: 0; }
.catalog-form label { display: grid; gap: 8px; margin-block: 16px; }
.catalog-form p { color: var(--td-text-color-secondary); line-height: 1.7; }
.readonly-fields { display: grid; grid-template-columns: auto 1fr; gap: 12px; }
.readonly-fields dd { margin: 0; overflow-wrap: anywhere; }
.type-row { display: flex; gap: 12px; flex-wrap: wrap; align-items: center; padding-block: 8px; }
.type-row > :deep(.t-input__wrap) { flex: 1; min-width: 160px; }
.toggle-label { grid-template-columns: auto 1fr; align-items: center; }
@media (max-width: 720px) { .catalog-toolbar { flex-direction: column; align-items: stretch; } .type-row { padding-block: 12px; border-bottom: 1px solid var(--td-component-stroke); } }
</style>
