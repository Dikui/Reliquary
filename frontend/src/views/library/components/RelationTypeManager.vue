<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Checkbox, Dialog, Select, confirm, toast } from '@/components/ui'
import { getCatalogLibrary, listLibraries, mergeLibraryRelationTypes, saveCatalogLibrary, type LibraryCatalogItem, type LibraryRelationType } from '@/api/library'
import { moveType, validTypes } from '@/utils/libraryCatalogForm'

const props = defineProps<{ initialLibraryId?: string }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t } = useI18n()
const libraries = ref<LibraryCatalogItem[]>([]), libraryId = ref(props.initialLibraryId || '')
const types = ref<LibraryRelationType[]>([]), saved = ref('[]')
const loading = ref(false), loaded = ref(false), saving = ref(false), error = ref('')
const fromType = ref(''), toType = ref('')
const dirty = computed(() => JSON.stringify(types.value) !== saved.value)
const relationOptions = computed(() => types.value.filter(row => row.enabled).map(row => row.name))
let sequence = 0
function errorMessage(cause: unknown) { return (cause as { message?: string })?.message || t('common.operationFailed') }

async function load(id = libraryId.value) {
  const current = ++sequence
  loading.value = true; loaded.value = false; error.value = ''
  try {
    const catalog = await listLibraries()
    if (!catalog.success) throw new Error(catalog.message)
    if (current !== sequence) return
    libraries.value = catalog.data || []
    libraryId.value = libraries.value.some(library => library.id === id) ? id : libraries.value[0]?.id || ''
    types.value = []; saved.value = '[]'; fromType.value = ''; toType.value = ''
    if (!libraryId.value) return
    const response = await getCatalogLibrary(libraryId.value)
    if (!response.success) throw new Error(response.message)
    if (current !== sequence) return
    types.value = response.data.relation_types || []
    saved.value = JSON.stringify(types.value); loaded.value = true
  } catch (cause) {
    if (current === sequence) error.value = errorMessage(cause)
  } finally { if (current === sequence) loading.value = false }
}
function afterDiscard(action: () => void) {
  if (saving.value) return
  if (!dirty.value) { action(); return }
  confirm({ header: t('ikb18.relationTypes'), body: t('ikb18.relationTypesUnsaved'), confirmBtn: t('common.discard'), cancelBtn: t('common.cancel'), onConfirm: () => { reset(); action() } })
}
function reset() { types.value = JSON.parse(saved.value); error.value = '' }
function close() { afterDiscard(() => emit('close')) }
function switchLibrary(id: string) { if (!loading.value && id !== libraryId.value) afterDiscard(() => { void load(id) }) }
async function save() {
  if (!loaded.value || loading.value || saving.value || !dirty.value) return
  if (!validTypes(types.value)) { error.value = t('ikb18.invalidTypes'); return }
  saving.value = true; error.value = ''
  try {
    const rows = types.value.map((row, position) => ({ ...row, name: row.name.trim(), position }))
    const response = await saveCatalogLibrary(libraryId.value, { relation_types: rows })
    if (!response.success) throw new Error(response.message)
    types.value = rows; saved.value = JSON.stringify(rows)
    fromType.value = ''; toType.value = ''
    emit('changed'); toast.success(t('ikb18.relationTypesSaved'))
  } catch (cause) { error.value = errorMessage(cause) }
  finally { saving.value = false }
}
function merge() {
  if (!loaded.value || loading.value || saving.value || dirty.value || !fromType.value || !toType.value || fromType.value === toType.value) return
  const id = libraryId.value, from = fromType.value, to = toType.value
  confirm({ header: t('ikb18.merge'), body: `${from} → ${to}. ${t('ikb18.mergeHint')}`, confirmBtn: t('ikb18.mergeConfirm'), closeOnOverlayClick: false, onConfirm: async ({ dialog }) => {
    if (saving.value) return
    saving.value = true; error.value = ''
    try {
      const response = await mergeLibraryRelationTypes(id, from, to)
      if (!response.success) throw new Error(response.message)
      emit('changed'); toast.success(t('ikb18.relationTypesMerged'))
      void load(id)
    } catch (cause) {
      error.value = errorMessage(cause)
      dialog.update({ body: error.value, confirmBtn: t('common.retry') }); throw cause
    } finally { saving.value = false }
  } })
}
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value || saving.value) { event.preventDefault(); event.returnValue = '' } }
onMounted(() => { void load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { sequence++; window.removeEventListener('beforeunload', beforeUnload) })
</script>

<template>
  <Dialog :model-value="true" :title="t('ikb18.manageRelationTypes')" :description="t('ikb18.relationTypesHint')" content-class="relation-type-dialog" :closable="!saving" :close-on-esc-keydown="!saving" :close-on-overlay-click="false" @update:model-value="!$event && close()">
    <div class="relation-type-manager" :aria-busy="loading">
      <label class="relation-type-library">{{ t('entityPage.library') }}<Select :model-value="libraryId" :disabled="loading || saving" @update:model-value="switchLibrary"><option v-for="library in libraries" :key="library.id" :value="library.id">{{ library.display_name || library.name }}</option></Select></label>
      <p v-if="error" class="relation-type-error" role="alert">{{ error }} <Button v-if="!loaded" variant="ghost" :loading="loading" @click="load()">{{ t('common.retry') }}</Button></p>
      <p v-if="loading" class="relation-type-hint" role="status">{{ t('common.loading') }}</p>
      <p v-else-if="!libraries.length && !error" role="status">{{ t('entityGraph.noLibraries') }}</p>
      <form v-if="loaded" novalidate @submit.prevent="save">
        <fieldset :disabled="saving || loading">
          <p v-if="!types.length" class="relation-type-hint" role="status">{{ t('ikb18.noRelationTypes') }}</p>
          <div v-for="(row, index) in types" :key="index" class="relation-type-row">
            <input v-model="row.name" class="rq-control" :aria-label="t('ikb18.name')" maxlength="100" required>
            <label class="relation-type-check"><Checkbox v-model="row.enabled" />{{ t('ikb18.enabled') }}</label>
            <Button :disabled="index === 0" @click="moveType(types, index, -1)">{{ t('ikb18.up') }}</Button>
            <Button :disabled="index === types.length - 1" @click="moveType(types, index, 1)">{{ t('ikb18.down') }}</Button>
            <Button variant="ghost" @click="types.splice(index, 1)">{{ t('ikb18.remove') }}</Button>
          </div>
          <Button @click="types.push({ name: '', enabled: true, position: types.length })">{{ t('ikb18.add') }}</Button>
          <div class="relation-type-actions">
            <Button type="submit" variant="primary" :loading="saving" :disabled="!dirty">{{ t('common.save') }}</Button>
            <Button :disabled="!dirty" @click="reset">{{ t('common.cancel') }}</Button>
          </div>
          <section v-if="relationOptions.length > 1" class="relation-type-merge">
            <h3>{{ t('ikb18.merge') }}</h3><p class="relation-type-hint">{{ t('ikb18.mergeHint') }}</p>
            <div class="relation-type-pair">
              <label>{{ t('ikb18.from') }}<Select v-model="fromType" :disabled="dirty"><option value="" /><option v-for="name in relationOptions" :key="name" :value="name">{{ name }}</option></Select></label>
              <label>{{ t('ikb18.to') }}<Select v-model="toType" :disabled="dirty"><option value="" /><option v-for="name in relationOptions.filter(item => item !== fromType)" :key="name" :value="name">{{ name }}</option></Select></label>
            </div>
            <Button :disabled="dirty || !fromType || !toType || fromType === toType" @click="merge">{{ t('ikb18.mergeConfirm') }}</Button>
          </section>
        </fieldset>
      </form>
    </div>
  </Dialog>
</template>

<style scoped>
.relation-type-manager { display: grid; gap: var(--space-4); }
.relation-type-manager fieldset { border: 0; padding: 0; margin: 0; min-width: 0; display: grid; gap: var(--space-3); justify-items: start; }
.relation-type-library, .relation-type-pair label { display: grid; gap: var(--space-1); font-size: var(--text-sm); }
.relation-type-hint { color: var(--muted); font-size: var(--type-note-size); line-height: var(--leading); margin: 0; }
.relation-type-error { color: var(--danger); margin: 0; }
.relation-type-row { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); width: 100%; }
.relation-type-row > input { flex: 1 1 160px; min-width: 0; }
.relation-type-check { display: flex; align-items: center; gap: var(--space-1); font-size: var(--text-sm); }
.relation-type-actions { display: flex; gap: var(--space-2); }
.relation-type-merge { display: grid; justify-items: start; gap: var(--space-3); width: 100%; padding-top: var(--space-3); border-top: 1px solid var(--border); }
.relation-type-merge h3 { font-size: var(--type-subsection-size); font-weight: var(--type-subsection-weight); margin: 0; }
.relation-type-pair { width: 100%; display: grid; grid-template-columns: repeat(auto-fit, minmax(min(160px, 100%), 1fr)); gap: var(--space-3); }
</style>

<style>
/* Dialog content is portaled, so its width cannot be set from the scoped block. */
.relation-type-dialog { width: min(720px, calc(100vw - 32px)); }
</style>
