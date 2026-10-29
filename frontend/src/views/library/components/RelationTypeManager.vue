<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
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
const libraryOptions = computed(() => libraries.value.map(library => ({ value: library.id, label: library.display_name || library.name })))
const relationOptions = computed(() => types.value.filter(row => row.enabled).map(row => ({ value: row.name, label: row.name })))
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
  const dialog = DialogPlugin.confirm({
    header: t('ikb18.relationTypes'), body: t('ikb18.relationTypesUnsaved'),
    confirmBtn: t('common.discard'), cancelBtn: t('common.cancel'),
    onConfirm: () => { reset(); dialog.destroy(); action() },
    onCancel: () => dialog.destroy(), onClose: () => dialog.destroy(),
  })
}
function reset() { types.value = JSON.parse(saved.value); error.value = '' }
function close() { afterDiscard(() => emit('close')) }
function switchLibrary(id: string) { if (!loading.value) afterDiscard(() => { void load(id) }) }
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
    emit('changed'); MessagePlugin.success(t('ikb18.relationTypesSaved'))
  } catch (cause) { error.value = errorMessage(cause) }
  finally { saving.value = false }
}
function merge() {
  if (!loaded.value || loading.value || saving.value || dirty.value || !fromType.value || !toType.value || fromType.value === toType.value) return
  const id = libraryId.value, from = fromType.value, to = toType.value
  const dialog = DialogPlugin.confirm({
    header: t('ikb18.merge'), body: `${from} → ${to}. ${t('ikb18.mergeHint')}`,
    confirmBtn: t('ikb18.mergeConfirm'), closeOnOverlayClick: false,
    onCancel: () => dialog.destroy(), onClose: () => dialog.destroy(),
    onConfirm: async () => {
      if (saving.value) return
      saving.value = true; error.value = ''
      dialog.update({ confirmBtn: { content: t('ikb18.mergeConfirm'), loading: true }, closeBtn: false, closeOnEscKeydown: false, cancelBtn: { content: t('common.cancel'), disabled: true } })
      try {
        const response = await mergeLibraryRelationTypes(id, from, to)
        if (!response.success) throw new Error(response.message)
        dialog.destroy(); emit('changed'); MessagePlugin.success(t('ikb18.relationTypesMerged'))
        await load(id)
      } catch (cause) {
        error.value = errorMessage(cause)
        dialog.update({ body: error.value, confirmBtn: { content: t('common.retry'), loading: false }, closeBtn: true, closeOnEscKeydown: true, cancelBtn: { content: t('common.cancel'), disabled: false } })
      } finally { saving.value = false }
    },
  })
}
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value || saving.value) { event.preventDefault(); event.returnValue = '' } }
onMounted(() => { void load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { sequence++; window.removeEventListener('beforeunload', beforeUnload) })
</script>

<template>
  <t-dialog :visible="true" :header="t('ikb18.manageRelationTypes')" :footer="false" :close-btn="!saving" :close-on-esc-keydown="!saving" :close-on-overlay-click="false" width="min(720px, 95vw)" @close="close">
    <div class="relation-type-manager">
      <p class="relation-type-hint">{{ t('ikb18.relationTypesHint') }}</p>
      <t-select :value="libraryId" :options="libraryOptions" :disabled="loading || saving" :loading="loading" :placeholder="t('ikb18.library')" :aria-label="t('ikb18.library')" @change="switchLibrary($event as string)" />
      <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button v-if="!loaded" variant="text" :loading="loading" @click="load()">{{ t('common.retry') }}</t-button></template></t-alert>
      <t-loading :loading="loading" show-overlay>
        <p v-if="!loading && !libraries.length && !error" role="status">{{ t('entityGraph.noLibraries') }}</p>
        <form v-if="loaded" novalidate @submit.prevent="save">
          <fieldset :disabled="saving || loading">
            <p v-if="!types.length" class="relation-type-hint" role="status">{{ t('ikb18.noRelationTypes') }}</p>
            <div v-for="(row, index) in types" :key="index" class="relation-type-row">
              <t-input v-model="row.name" :disabled="saving || loading" :aria-label="t('ikb18.name')" :maxlength="100" />
              <t-checkbox v-model="row.enabled" :disabled="saving || loading">{{ t('ikb18.enabled') }}</t-checkbox>
              <div class="relation-type-actions">
                <t-button variant="text" :disabled="saving || loading || index === 0" @click="moveType(types, index, -1)">{{ t('ikb18.up') }}</t-button>
                <t-button variant="text" :disabled="saving || loading || index === types.length - 1" @click="moveType(types, index, 1)">{{ t('ikb18.down') }}</t-button>
                <t-button variant="text" theme="danger" :disabled="saving || loading" @click="types.splice(index, 1)">{{ t('ikb18.remove') }}</t-button>
              </div>
            </div>
            <t-button variant="outline" :disabled="saving || loading" @click="types.push({ name: '', enabled: true, position: types.length })">{{ t('ikb18.add') }}</t-button>
            <div class="relation-type-actions relation-type-save">
              <t-button type="submit" :loading="saving" :disabled="loading || !dirty">{{ t('common.save') }}</t-button>
              <t-button variant="outline" :disabled="saving || loading || !dirty" @click="reset">{{ t('common.cancel') }}</t-button>
            </div>
            <section v-if="relationOptions.length > 1">
              <h3>{{ t('ikb18.merge') }}</h3><p class="relation-type-hint">{{ t('ikb18.mergeHint') }}</p>
              <div class="relation-type-merge">
                <t-select v-model="fromType" :options="relationOptions" :disabled="saving || loading || dirty" :placeholder="t('ikb18.from')" :aria-label="t('ikb18.from')" />
                <t-select v-model="toType" :options="relationOptions.filter(row => row.value !== fromType)" :disabled="saving || loading || dirty" :placeholder="t('ikb18.to')" :aria-label="t('ikb18.to')" />
                <t-button :disabled="saving || loading || dirty || !fromType || !toType || fromType === toType" @click="merge">{{ t('ikb18.mergeConfirm') }}</t-button>
              </div>
            </section>
          </fieldset>
        </form>
      </t-loading>
    </div>
  </t-dialog>
</template>

<style scoped>
.relation-type-manager { display: grid; gap: 16px; color: var(--td-text-color-primary); }
.relation-type-manager fieldset { border: 0; padding: 0; margin: 0; min-width: 0; }
.relation-type-hint { color: var(--td-text-color-secondary); line-height: 1.7; margin: 0 0 12px; }
.relation-type-row, .relation-type-actions, .relation-type-merge { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.relation-type-row { margin-bottom: 12px; }
.relation-type-row > :deep(.t-input__wrap), .relation-type-merge > :deep(.t-select__wrap) { flex: 1; min-width: 160px; }
.relation-type-save { margin-block: 16px; }
.relation-type-manager h3 { font-size: 16px; margin-block: 24px 12px; }
@media (max-width: 600px) { .relation-type-row > :deep(.t-input__wrap), .relation-type-merge > :deep(.t-select__wrap) { flex-basis: 100%; min-width: 0; } }
</style>
