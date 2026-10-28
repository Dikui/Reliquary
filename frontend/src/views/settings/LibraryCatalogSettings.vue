<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Dialog, LibMark, Tag, confirm, toast } from '@/components/ui'
import { CONTENT_TYPES, type ContentType } from '@/types/entryClassification'
import { listLibraries, getCatalogLibrary, createCatalogLibrary, saveCatalogLibrary, deleteCatalogLibrary, mergeLibraryRelationTypes, type LibraryCatalogItem, type LibraryColor } from '@/api/library'
import { validDirectory, validLibraryName, validTypes, librarySettingsPayload, moveType, catalogDescription, fillParentChunkDefaults, libraryNumberErrors, type LibraryNumberField } from '@/utils/libraryCatalogForm'
import ModelTierEditor from '@/components/ModelTierEditor.vue'
import { copyModelTiers } from '@/utils/modelTiers'
import type { ModelTier } from '@/types/modelTiers'
const { t, locale } = useI18n()
const emit = defineEmits<{ changed: [] }>()
const libraries = ref<LibraryCatalogItem[]>([]), draft = ref<LibraryCatalogItem | null>(null), saved = ref(''), error = ref(''), loading = ref(false), saving = ref(false)
const errorNotice = ref<HTMLElement>()
const catalogForm = ref<HTMLFormElement>()
const validationAttempted = ref(false)
const numberErrors = computed(() => validationAttempted.value && draft.value?.chunking_config ? libraryNumberErrors(draft.value.chunking_config) : {})
function numberError(field: LibraryNumberField) {
  const issue = numberErrors.value[field]
  return issue ? t(issue.reason === 'required' ? 'settingsPage.numberRequired' : 'settingsPage.numberRange', issue) : ''
}
function toggleParentChild() {
  if (draft.value?.chunking_config?.enable_parent_child) fillParentChunkDefaults(draft.value.chunking_config)
}
watch(error, async message => { if (message) { await nextTick(); errorNotice.value?.scrollIntoView?.({ block: 'nearest' }) } })
const creating = ref(false), newLibrary = ref({ directory: '', name: '', description: '' })
const fromType = ref(''), toType = ref(''), newContentType = ref(''), newSeparator = ref('')
const colors: LibraryColor[] = [1, 2, 3, 4, 5, 6, 7, 8]
const colorNames = ['red', 'blue', 'yellow', 'black', 'green', 'orange', 'purple', 'gray']
const contentTypeName = (name: string) => CONTENT_TYPES.includes(name as ContentType) ? t(`library.contentTypes.${name}`) : name
const modifiedCount = computed(() => {
  if (!draft.value || !saved.value) return 0
  const previous = JSON.parse(saved.value) as LibraryCatalogItem
  return (Object.keys(librarySettingsPayload(draft.value)) as Array<keyof LibraryCatalogItem>).filter(key => JSON.stringify(draft.value![key]) !== JSON.stringify(previous[key])).length
})
function reset() { if (saved.value && !saving.value) { draft.value = JSON.parse(saved.value); error.value = ''; newContentType.value = ''; validationAttempted.value = false } }
function addContentType() {
  const name = newContentType.value.trim()
  if (!draft.value || !name || saving.value) return
  if (draft.value.content_types.some(row => row.name.trim() === name) || Array.from(name).length > 100) { error.value = t('ikb18.invalidTypes'); return }
  draft.value.content_types.push({ name, position: draft.value.content_types.length, enabled: true, extract_graph: false }); newContentType.value = ''
}
function addSeparator() {
  const value = newSeparator.value.replaceAll('\\n', '\n')
  if (draft.value && value && !draft.value.chunking_config.separators.includes(value)) draft.value.chunking_config.separators.push(value)
  newSeparator.value = ''
}
function notifySaved(library?: LibraryCatalogItem) {
  if (library) libraries.value = libraries.value.map(item => item.id === library.id ? { ...item, name: library.name, display_name: library.name, color: library.color } : item)
  if (typeof window !== 'undefined') window.dispatchEvent(new CustomEvent('reliquary:library-settings-saved', { detail: library ? { id: library.id, name: library.name, color: library.color } : undefined }))
  emit('changed')
}
let sequence = 0
const dirty = computed(() => !!draft.value && JSON.stringify(draft.value) !== saved.value)
const relationOptions = computed(() => (draft.value?.relation_types || []).filter(row => row.enabled).map(row => ({ value: row.name, label: row.name })))
const modelTiers = computed<ModelTier[]>(() => [...new Set([...(draft.value?.processing_model_tiers || []), 'rerank' as const])])
async function load(id?: string) {
  const current = ++sequence; loading.value = true; error.value = ''
  try {
    const catalog = await listLibraries(); if (!catalog.success) throw new Error(catalog.message)
    if (current !== sequence) return
    libraries.value = catalog.data || []
    const selected = id || draft.value?.id || libraries.value[0]?.id
    if (!selected) { draft.value = null; saved.value = ''; return }
    const response = await getCatalogLibrary(selected); if (!response.success) throw new Error(response.message)
    if (current !== sequence) return
    draft.value = { ...response.data, model_tiers: copyModelTiers({ ...response.data.model_tiers, generation: response.data.model_tiers?.generation || response.data.summary_model_id || '' }), description: catalogDescription(response.data, t('ikb18.systemMediaDescription')), chunking_config: response.data.chunking_config ? { ...response.data.chunking_config, strategy: response.data.chunking_config.strategy ?? '', token_limit: response.data.chunking_config.token_limit ?? 0, languages: response.data.chunking_config.languages ?? [] } : response.data.chunking_config, content_types: response.data.content_types || [], relation_types: response.data.relation_types || [] }
    toggleParentChild()
    validationAttempted.value = false
    saved.value = JSON.stringify(draft.value); fromType.value = ''; toType.value = ''; newContentType.value = ''
  } catch (cause: any) { if (current === sequence) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === sequence) loading.value = false }
}
function switchLibrary(id: string) {
  if (saving.value || loading.value) return
  if (!dirty.value) { void load(id); return }
  const dialog = confirm({ header: t('ikb18.catalog'), body: t('ikb18.unsaved'), onConfirm: () => { dialog.destroy(); void load(id) } })
}
async function save() {
  if (!draft.value || saving.value || loading.value || !dirty.value) return
  error.value = ''
  validationAttempted.value = true
  if (Object.keys(numberErrors.value).length) {
    await nextTick()
    catalogForm.value?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
    return
  }
  if (!validLibraryName(draft.value.name)) { error.value = t('ikb18.invalidName'); return }
  if (!validTypes(draft.value.content_types) || !validTypes(draft.value.relation_types)) { error.value = t('ikb18.invalidTypes'); return }
  saving.value = true; error.value = ''
  try {
    const payload = librarySettingsPayload(draft.value)
    const response = await saveCatalogLibrary(draft.value.id, payload)
    if (!response.success) throw new Error(response.message)
    Object.assign(draft.value, payload)
    saved.value = JSON.stringify(draft.value)
    notifySaved(draft.value)
    toast.success(t('knowledgeEditor.messages.updateSuccess'))
  }
  catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
  finally { saving.value = false }
}
async function create() {
  if (saving.value) return
  if (!validDirectory(newLibrary.value.directory)) { error.value = t('ikb18.invalidDirectory'); return }
  if (!validLibraryName(newLibrary.value.name)) { error.value = t('ikb18.invalidName'); return }
  saving.value = true; error.value = ''
  try { const response = await createCatalogLibrary({ ...newLibrary.value, name: newLibrary.value.name.trim() }); if (!response.success) throw new Error(response.message); creating.value = false; newLibrary.value = { directory: '', name: '', description: '' }; notifySaved(); await load(response.data.id) }
  catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
  finally { saving.value = false }
}
function confirmAction(header: string, body: string, action: () => Promise<void>, danger = false) {
  confirm({ header, body, theme: danger ? 'danger' : 'default', closeOnOverlayClick: false, onConfirm: async ({ dialog }) => {
    if (saving.value) return
    saving.value = true; error.value = ''
    try { await action() }
    catch (cause: any) { error.value = cause.message || t('common.operationFailed'); dialog.update({ body: error.value, confirmBtn: t('common.retry') }); throw cause }
    finally { saving.value = false }
  } })
}
function merge() {
  if (!draft.value || !fromType.value || !toType.value || fromType.value === toType.value || dirty.value) return
  const id = draft.value.id, from = fromType.value, to = toType.value
  confirmAction(t('ikb18.merge'), `${from} → ${to}. ${t('ikb18.mergeHint')}`, async () => { const result = await mergeLibraryRelationTypes(id, from, to); if (!result.success) throw new Error(result.message); await load(id) })
}
function removeLibrary() {
  if (!draft.value || draft.value.entry_count !== 0 || saving.value || loading.value) return
  const id = draft.value.id
  confirmAction(t('ikb18.deleteLibrary'), t('ikb18.deleteConfirm'), async () => { const result = await deleteCatalogLibrary(id); if (!result.success) throw new Error(result.message); draft.value = null; saved.value = ''; notifySaved(); await load() }, true)
}
function confirmLeave(): Promise<boolean> {
  if (saving.value) return Promise.resolve(false)
  if (!dirty.value) return Promise.resolve(true)
  return new Promise(resolve => { const dialog = confirm({ header: t('ikb18.catalog'), body: t('ikb18.unsaved'), onConfirm: () => { draft.value = JSON.parse(saved.value); dialog.destroy(); resolve(true) }, onCancel: () => { dialog.destroy(); resolve(false) }, onClose: () => { dialog.destroy(); resolve(false) } }) })
}
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
watch(locale, async () => { const current = sequence; try { const response = await listLibraries(); if (current === sequence && response.success) libraries.value = response.data || [] } catch { /* Preserve edited settings when localized names cannot be reloaded. */ } })
defineExpose({ confirmLeave })
onMounted(() => { void load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { sequence++; window.removeEventListener('beforeunload', beforeUnload) })
</script>
<template>
  <div class="library-catalog">
    <div class="catalog-toolbar" :aria-label="t('settingsPage.chooseLibrary')">
      <button v-for="library in libraries" :key="library.id" type="button" class="library-choice" :aria-pressed="draft?.id === library.id" :disabled="saving || loading" @click="switchLibrary(library.id)">
        <LibMark :color="library.color" />{{ library.display_name || library.name }}
      </button>
      <Button :disabled="saving || loading || dirty" @click="creating = true; error = ''">{{ t('ikb18.create') }}</Button>
    </div>
    <div v-if="error" ref="errorNotice" class="catalog-error" role="alert">{{ error }} <Button v-if="!draft" @click="load()">{{ t('common.retry') }}</Button></div>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <p v-else-if="!libraries.length" class="catalog-hint">{{ t('libraryNavigation.empty') }}</p>
    <form v-if="draft" ref="catalogForm" class="catalog-form" novalidate @submit.prevent="save">
      <fieldset :disabled="saving || loading">
        <section class="catalog-section">
          <header><h2>{{ t('knowledgeEditor.basic.title') }}</h2><p>{{ t('settingsPage.basicHint') }}</p></header>
          <div class="catalog-controls">
            <div class="catalog-pair">
              <label>{{ t('ikb18.name') }}<input v-model="draft.name" name="library-name" maxlength="255" required></label>
              <label>{{ t('ikb18.directory') }}<input :value="`${draft.directory}/`" readonly></label>
            </div>
            <fieldset class="catalog-colors"><legend>{{ t('settingsPage.color') }}</legend><div class="color-options">
              <label v-for="(color, index) in colors" :key="color" class="color-choice">
                <input v-model="draft.color" type="radio" name="library-color" :value="color" :aria-label="t(`settingsPage.colors.${colorNames[index]}`)">
                <LibMark :color="color" />
              </label>
            </div><p class="catalog-hint">{{ t('settingsPage.colorHint') }}</p></fieldset>
            <label>{{ t('ikb18.description') }}<textarea v-model="draft.description" name="library-description" rows="3" /></label>
          </div>
        </section>
        <section class="catalog-section">
          <header><h2>{{ t('modelTiers.title') }}</h2><p>{{ t('ikb18.admissionPolicyHint') }}</p></header>
          <div class="catalog-controls"><ModelTierEditor v-model="draft.model_tiers" :tiers="modelTiers" :before-navigate="confirmLeave" :disabled="saving || loading" /></div>
        </section>
        <section v-if="draft.chunking_config" class="catalog-section">
          <header><h2>{{ t('knowledgeEditor.chunking.title') }}</h2><p>{{ t('settingsPage.chunkHint') }}</p></header>
          <div class="catalog-controls">
            <div class="catalog-pair">
              <label>{{ t('knowledgeEditor.chunking.sizeLabel') }}<input v-model.number="draft.chunking_config.chunk_size" name="chunk-size" type="number" min="100" max="4000" required id="chunk-size" :aria-invalid="!!numberErrors.chunk_size" :aria-describedby="numberErrors.chunk_size ? 'chunk-size-error' : undefined"><span v-if="numberErrors.chunk_size" id="chunk-size-error" class="catalog-field-error">{{ numberError('chunk_size') }}</span></label>
              <label>{{ t('knowledgeEditor.chunking.overlapLabel') }}<input v-model.number="draft.chunking_config.chunk_overlap" name="chunk-overlap" type="number" min="0" max="500" required id="chunk-overlap" :aria-invalid="!!numberErrors.chunk_overlap" :aria-describedby="numberErrors.chunk_overlap ? 'chunk-overlap-error' : undefined"><span v-if="numberErrors.chunk_overlap" id="chunk-overlap-error" class="catalog-field-error">{{ numberError('chunk_overlap') }}</span></label>
            </div>
            <label class="catalog-check"><input v-model="draft.chunking_config.enable_parent_child" name="parent-child" type="checkbox" @change="toggleParentChild">{{ t('knowledgeEditor.chunking.parentChildLabel') }}</label>
            <div v-if="draft.chunking_config.enable_parent_child" class="catalog-pair">
              <label>{{ t('knowledgeEditor.chunking.parentChunkSizeLabel') }}<input v-model.number="draft.chunking_config.parent_chunk_size" type="number" min="512" max="8192" required name="parent-chunk-size" id="parent-chunk-size" :aria-invalid="!!numberErrors.parent_chunk_size" :aria-describedby="numberErrors.parent_chunk_size ? 'parent-chunk-size-error' : undefined"><span v-if="numberErrors.parent_chunk_size" id="parent-chunk-size-error" class="catalog-field-error">{{ numberError('parent_chunk_size') }}</span></label>
              <label>{{ t('knowledgeEditor.chunking.childChunkSizeLabel') }}<input v-model.number="draft.chunking_config.child_chunk_size" type="number" min="64" max="2048" required name="child-chunk-size" id="child-chunk-size" :aria-invalid="!!numberErrors.child_chunk_size" :aria-describedby="numberErrors.child_chunk_size ? 'child-chunk-size-error' : undefined"><span v-if="numberErrors.child_chunk_size" id="child-chunk-size-error" class="catalog-field-error">{{ numberError('child_chunk_size') }}</span></label>
            </div>
            <details><summary>{{ t('knowledgeEditor.chunking.advancedLabel') }}</summary><div class="catalog-controls catalog-advanced">
              <label>{{ t('knowledgeEditor.chunking.strategyLabel') }}<select v-model="draft.chunking_config.strategy"><option value="">{{ t('knowledgeEditor.chunking.strategyPlaceholder') }}</option><option v-for="strategy in ['auto', 'heading', 'heuristic', 'legacy']" :key="strategy" :value="strategy">{{ t(`knowledgeEditor.chunking.strategies.${strategy}.label`) }}</option></select></label>
              <label>{{ t('knowledgeEditor.chunking.tokenLimitLabel') }}<input v-model.number="draft.chunking_config.token_limit" type="number" min="0" max="8192" :disabled="draft.chunking_config.strategy === 'legacy'" name="token-limit" id="token-limit" :aria-invalid="!!numberErrors.token_limit" :aria-describedby="numberErrors.token_limit ? 'token-limit-error' : undefined"><span v-if="numberErrors.token_limit" id="token-limit-error" class="catalog-field-error">{{ numberError('token_limit') }}</span></label>
              <fieldset><legend>{{ t('knowledgeEditor.chunking.languagesLabel') }}</legend><label v-for="language in ['de', 'en', 'zh']" :key="language" class="catalog-check"><input v-model="draft.chunking_config.languages" type="checkbox" :value="language" :disabled="draft.chunking_config.strategy === 'legacy'">{{ t(`knowledgeEditor.chunking.languageOptions.${language}`) }}</label></fieldset>
              <fieldset><legend>{{ t('knowledgeEditor.chunking.separatorsLabel') }}</legend><div class="catalog-types"><Tag v-for="(separator, index) in draft.chunking_config.separators" :key="index"><code>{{ JSON.stringify(separator) }}</code><button type="button" :aria-label="`${t('ikb18.remove')} ${JSON.stringify(separator)}`" @click="draft.chunking_config.separators.splice(index, 1)">×</button></Tag></div><div class="catalog-inline"><input v-model="newSeparator" :aria-label="t('knowledgeEditor.chunking.separatorsLabel')" @keydown.enter.prevent="addSeparator"><Button :disabled="!newSeparator" @click="addSeparator">{{ t('ikb18.add') }}</Button></div></fieldset>
            </div></details>
          </div>
        </section>
        <section class="catalog-section">
          <header><h2>{{ t('ikb18.contentTypes') }}</h2><p>{{ t('settingsPage.typesHint') }}</p></header>
          <div class="catalog-controls">
            <div class="catalog-types"><Tag v-for="(row, index) in draft.content_types" :key="index"><span :class="{ 'type-disabled': !row.enabled }">{{ contentTypeName(row.name) }}</span><button type="button" :aria-label="`${t('ikb18.remove')} ${contentTypeName(row.name)}`" @click="draft.content_types.splice(index, 1)">×</button></Tag></div>
            <div class="catalog-inline"><input v-model="newContentType" :placeholder="t('settingsPage.addType')" :aria-label="t('settingsPage.addType')" maxlength="100" @keydown.enter.prevent="addContentType"><Button :disabled="!newContentType.trim()" @click="addContentType">{{ t('ikb18.add') }}</Button></div>
            <details v-if="draft.content_types.length"><summary>{{ t('settingsPage.manageTypes') }}</summary><div v-for="(row, index) in draft.content_types" :key="index" class="type-row"><input v-model="row.name" :aria-label="t('ikb18.name')" maxlength="100" required><label class="catalog-check"><input v-model="row.enabled" type="checkbox">{{ t('ikb18.enabled') }}</label><Button :disabled="index === 0" @click="moveType(draft.content_types, index, -1)">{{ t('ikb18.up') }}</Button><Button :disabled="index === draft.content_types.length - 1" @click="moveType(draft.content_types, index, 1)">{{ t('ikb18.down') }}</Button></div></details>
          </div>
        </section>
        <section class="catalog-section">
          <header><h2>{{ t('ikb18.feed') }}</h2></header>
          <label class="catalog-check"><input v-model="draft.feed_enabled" name="library-feed" type="checkbox">{{ t('settingsPage.feedHint') }}</label>
        </section>
        <section class="catalog-section">
          <header><h2>{{ t('ikb18.relationTypes') }}</h2></header>
          <div class="catalog-controls">
            <div v-for="(row, index) in draft.relation_types" :key="index" class="type-row"><input v-model="row.name" :aria-label="t('ikb18.name')" maxlength="100" required><label class="catalog-check"><input v-model="row.enabled" type="checkbox">{{ t('ikb18.enabled') }}</label><Button :disabled="index === 0" @click="moveType(draft.relation_types, index, -1)">{{ t('ikb18.up') }}</Button><Button :disabled="index === draft.relation_types.length - 1" @click="moveType(draft.relation_types, index, 1)">{{ t('ikb18.down') }}</Button><Button variant="ghost" @click="draft.relation_types.splice(index, 1)">{{ t('ikb18.remove') }}</Button></div>
            <Button @click="draft.relation_types.push({ name: '', position: draft.relation_types.length, enabled: true })">{{ t('ikb18.add') }}</Button>
            <details v-if="relationOptions.length > 1"><summary>{{ t('ikb18.merge') }}</summary><p class="catalog-hint">{{ t('ikb18.mergeHint') }}</p><div class="catalog-pair"><label>{{ t('ikb18.from') }}<select v-model="fromType"><option value="" /> <option v-for="row in relationOptions" :key="row.value" :value="row.value">{{ row.label }}</option></select></label><label>{{ t('ikb18.to') }}<select v-model="toType"><option value="" /><option v-for="row in relationOptions.filter(item => item.value !== fromType)" :key="row.value" :value="row.value">{{ row.label }}</option></select></label></div><Button :disabled="dirty || !fromType || !toType || fromType === toType" @click="merge">{{ t('ikb18.mergeConfirm') }}</Button></details>
          </div>
        </section>
        <section class="catalog-section">
          <header><h2>{{ t('ikb18.deleteLibrary') }}</h2><p>{{ t('ikb18.deleteHint') }}</p></header>
          <div class="catalog-controls"><Button variant="danger" :disabled="draft.entry_count !== 0" @click="removeLibrary">{{ t('ikb18.deleteLibrary') }}</Button><p v-if="draft.entry_count" class="catalog-hint">{{ t('settingsPage.notEmpty', { count: draft.entry_count }) }}</p></div>
        </section>
      </fieldset>
      <div class="catalog-savebar"><span class="catalog-hint">{{ t(dirty ? 'settingsPage.unsaved' : 'settingsPage.saved', { count: modifiedCount }) }}</span><div class="catalog-inline"><Button :disabled="!dirty || saving || loading" @click="reset">{{ t('common.discard') }}</Button><Button type="submit" variant="primary" :loading="saving" :disabled="!dirty || loading">{{ t('common.save') }}</Button></div></div>
    </form>
    <Dialog v-model="creating" :title="t('ikb18.create')" :closable="!saving" :close-on-esc-keydown="!saving" :close-on-overlay-click="false">
      <form class="catalog-form" novalidate @submit.prevent="create"><fieldset class="catalog-controls" :disabled="saving">
        <label>{{ t('ikb18.directory') }}<input v-model="newLibrary.directory" maxlength="100" required></label><p class="catalog-hint">{{ t('ikb18.invalidDirectory') }}</p>
        <label>{{ t('ikb18.name') }}<input v-model="newLibrary.name" maxlength="255" required></label>
        <label>{{ t('ikb18.description') }}<textarea v-model="newLibrary.description" rows="3" /></label>
        <p v-if="error" class="catalog-error" role="alert">{{ error }}</p><div class="catalog-inline"><Button type="submit" variant="primary" :loading="saving">{{ t('common.create') }}</Button><Button :disabled="saving" @click="creating = false">{{ t('common.cancel') }}</Button></div>
      </fieldset></form>
    </Dialog>
  </div>
</template>
<style scoped>
.library-catalog { color: var(--text); }
.catalog-toolbar, .catalog-inline, .catalog-types, .color-options { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
.catalog-toolbar { margin-bottom: var(--space-5); }
.library-choice { display: inline-flex; align-items: center; gap: var(--space-2); padding: var(--space-1) var(--space-3); background: var(--bg); color: var(--text); border: 1px solid var(--border); border-radius: 0; font: inherit; cursor: pointer; overflow-wrap: anywhere; }
.library-choice[aria-pressed='true'] { background: var(--text); color: var(--bg); border-color: var(--rule); font-weight: 700; }
.catalog-form fieldset { min-width: 0; padding: 0; margin: 0; border: 0; }
.catalog-section { display: grid; grid-template-columns: 224px minmax(0, 1fr); gap: var(--space-6); padding-top: var(--space-4); margin-bottom: var(--space-5); border-top: 2px solid var(--rule); }
.catalog-section h2 { font-size: var(--text-md); font-weight: 800; margin: 0 0 var(--space-1); }
.catalog-section header p, .catalog-hint { font-size: var(--text-sm); color: var(--muted); line-height: var(--leading); margin: 0; }
.catalog-controls { display: grid; gap: var(--space-4); min-width: 0; align-content: start; }
.catalog-pair { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(160px, 100%), 1fr)); gap: var(--space-3); }
.catalog-form label:not(.catalog-check):not(.color-choice) { display: grid; gap: var(--space-2); min-width: 0; font-size: var(--text-sm); }
.catalog-form input:not([type='checkbox']):not([type='radio']), .catalog-form textarea, .catalog-form select { box-sizing: border-box; width: 100%; min-width: 0; min-height: var(--control); border: 1px solid var(--border); border-radius: 0; padding: var(--space-2) var(--space-3); color: var(--text); background: var(--bg); font: inherit; }
.catalog-form input[readonly] { background: var(--surface-soft); color: var(--muted); font-family: var(--mono); }
.catalog-form textarea { resize: vertical; }
.catalog-field-error { color: var(--danger); font-size: var(--text-sm); }
.catalog-form input[aria-invalid='true'] { border-color: var(--danger); }
.catalog-form input[type='number'] { font-family: var(--mono); }
.catalog-check { display: flex; gap: var(--space-2); align-items: center; font-size: var(--text-sm); }
.catalog-check input { accent-color: var(--text); width: 16px; height: 16px; }
.catalog-colors legend { margin-bottom: var(--space-2); font-size: var(--text-sm); }
.color-choice { position: relative; display: grid; place-items: center; width: 36px; height: 36px; border: 2px solid transparent; cursor: pointer; }
.color-choice input { position: absolute; width: 100%; height: 100%; margin: 0; opacity: 0; cursor: pointer; }
.color-choice :deep(.rq-lib-mark) { width: 24px; height: 24px; }
.color-choice:has(input:checked) { border-color: var(--rule); }
.color-choice:has(input:focus-visible), .library-choice:focus-visible, .catalog-form :is(input, textarea, select, summary, button):focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.catalog-colors .catalog-hint { margin-top: var(--space-2); }
.catalog-types :deep(.rq-tag) { display: inline-flex; align-items: center; gap: var(--space-2); font-size: var(--text-sm); }
.catalog-types button { border: 0; background: transparent; color: inherit; font: inherit; font-weight: 800; cursor: pointer; padding: 0; }
.type-disabled { color: var(--muted); text-decoration: line-through; }
.catalog-inline > input { flex: 1 1 144px; }
.type-row { display: flex; align-items: center; gap: var(--space-2); flex-wrap: wrap; margin-top: var(--space-3); }
.type-row > input { flex: 1 1 160px; }
.catalog-form summary { cursor: pointer; font-size: var(--text-sm); text-decoration: underline; }
.catalog-advanced { margin-top: var(--space-3); }
.catalog-error { padding: var(--space-3) var(--space-4); background: var(--danger-soft); border-top: 2px solid var(--danger); color: var(--danger); margin-bottom: var(--space-4); }
.catalog-savebar { position: sticky; bottom: 0; z-index: 2; display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3); padding: var(--space-3) 0; background: var(--bg); border-top: 2px solid var(--rule); }
.catalog-form :disabled, .library-choice:disabled { cursor: default; opacity: 0.5; }
@container settings-content (max-width: 720px) { .catalog-section { grid-template-columns: minmax(0, 1fr); gap: var(--space-4); } }
@media (max-width: 600px) { .catalog-section { grid-template-columns: minmax(0, 1fr); gap: var(--space-4); } }
</style>
