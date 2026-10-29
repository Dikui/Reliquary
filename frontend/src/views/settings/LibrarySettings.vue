<template>
  <section class="library-settings">
    <nav class="library-settings-tabs" :aria-label="t('settings.librarySettings')">
      <button v-for="tab in tabs" :key="tab" type="button" :aria-pressed="currentSection === tab" @click="selectTab(tab)">{{ t(tab === 'catalog' ? 'ikb18.catalog' : 'ikb18.sharedEmbedding') }}</button>
    </nav>
    <LibraryCatalogSettings v-if="currentSection === 'catalog'" ref="catalogRef" @changed="load" />
    <div v-else>
      <p v-if="error" class="library-settings-error" role="alert">{{ error }} <Button v-if="!formData" @click="load">{{ t('common.retry') }}</Button></p>
      <p v-if="loading" role="status">{{ t('common.loading') }}</p>
      <template v-if="formData">
        <ModelTierEditor v-model="formData.modelTiers" :tiers="['embedding']" scope="space" :all-models="allModels" :before-navigate="confirmLeave" :disabled="saving" @resolved="resolvedModelTiers = $event" />
        <EmbeddingRebuild :model-id="formData.modelTiers.embedding || ''" :models="allModels" :disabled="dirty || saving" />
        <div class="library-settings-actions"><Button :disabled="!dirty || saving || loading" @click="reset">{{ t('common.discard') }}</Button><Button variant="primary" :loading="saving" :disabled="!dirty || loading" @click="handleSubmit">{{ t('common.save') }}</Button></div>
      </template>
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, confirm, toast } from '@/components/ui'
import { getLibrary, listEntryFiles, updateLibrary } from '@/api/library'
import { useResourcesStore } from '@/stores/resources'
import { useUIStore } from '@/stores/ui'
import { changedSettings } from '@/utils/settingsPatch'
import { copyModelTiers } from '@/utils/modelTiers'
import type { ResolvedModelTiers } from '@/types/modelTiers'
import ModelTierEditor from '@/components/ModelTierEditor.vue'
import EmbeddingRebuild from '@/components/EmbeddingRebuild.vue'
import LibraryCatalogSettings from './LibraryCatalogSettings.vue'

const { t } = useI18n()
const props = defineProps<{ initialSection?: string }>()
const resources = useResourcesStore(), uiStore = useUIStore()
const tabs = ['catalog', 'models']
const initialSection = props.initialSection || uiStore.settingsInitialSubSection || 'catalog'
const currentSection = ref(tabs.includes(initialSection) ? initialSection : 'catalog')
const catalogRef = ref<{ confirmLeave?: () => Promise<boolean> }>()
async function selectTab(tab: string) {
  if (tab === currentSection.value || !await confirmLeave()) return
  currentSection.value = tab
}
watch(() => props.initialSection, section => { currentSection.value = section && tabs.includes(section) ? section : 'catalog' })
const formData = ref<any>(null), saved = ref(''), loading = ref(false), saving = ref(false), error = ref('')
const activeLibraryId = ref(''), hasFiles = ref(false), initialIndexingStrategy = ref<any>(null)
const loadedLibrary = ref<any>(null)
const resolvedModelTiers = ref<ResolvedModelTiers>({})
const allModels = computed(() => resources.allModels)
const dirty = computed(() => !!formData.value && JSON.stringify(formData.value) !== saved.value)
const needsEmbedding = computed(() => formData.value?.indexingStrategy?.vectorEnabled || formData.value?.indexingStrategy?.keywordEnabled)
const isIndexingLocked = computed(() => hasFiles.value)
let generation = 0
async function load() {
  const request = ++generation
  loading.value = true; error.value = ''
  try {
    const [response] = await Promise.all([getLibrary(), resources.ensureModels()])
    if (request !== generation) return
    if (response?.success === false) throw new Error(response.message || t('libraryEditor.messages.loadDataFailed'))
    const library = response?.data
    if (!library) { formData.value = null; saved.value = ''; return }
    loadedLibrary.value = library
    activeLibraryId.value = library.id
    const files: any = await listEntryFiles(library.id, { page: 1, page_size: 1 })
    if (request !== generation) return
    hasFiles.value = (files?.total || 0) > 0
    formData.value = {

      modelTiers: copyModelTiers({ light: library.summary_model_id || '', entity_resolution: library.summary_model_id || '', generation: library.summary_model_id || '', embedding: library.embedding_model_id || '', ...library.model_tiers }),
      name: library.name || '',
      description: library.description || '',

      chunkingConfig: {
        chunkSize: library.chunking_config?.chunk_size || 512,
        // Fallback only used when the loaded Library has no chunk_overlap stored.
        // Aligned with chunker.DefaultChunkOverlap on the backend.
        chunkOverlap: library.chunking_config?.chunk_overlap ?? 80,
        separators: library.chunking_config?.separators || ['\n\n', '\n', '。', '！', '？', ';', '；'],
        enableParentChild: library.chunking_config?.enable_parent_child,
        parentChunkSize: library.chunking_config?.parent_chunk_size || 4096,
        childChunkSize: library.chunking_config?.child_chunk_size || 384,
        // Existing Libraries without strategy field render as empty (= legacy behavior).
        // The user has to actively pick a value to opt in to the new tiers.
        strategy: library.chunking_config?.strategy || '',
        tokenLimit: library.chunking_config?.token_limit || 0,
        languages: library.chunking_config?.languages || [],
      },
      indexingStrategy: {
        vectorEnabled: library.indexing_strategy?.vector_enabled ?? true,
        keywordEnabled: library.indexing_strategy?.keyword_enabled ?? true,
      },
    }

    initialIndexingStrategy.value = { ...formData.value.indexingStrategy }
    saved.value = JSON.stringify(formData.value)
  } catch (cause: any) { if (request === generation) error.value = cause.message || t('libraryEditor.messages.loadDataFailed') }
  finally { if (request === generation) loading.value = false }
}
function handleChunkingConfigUpdate(value: any) { formData.value.chunkingConfig = { ...value } }
function toggleVectorIndexing() { if (!isIndexingLocked.value) { const enabled = !formData.value.indexingStrategy.vectorEnabled; formData.value.indexingStrategy = { vectorEnabled: enabled, keywordEnabled: enabled } } }
const buildSubmitData = (value = formData.value) => {
  if (!value) return null

  const data: any = {
    model_tiers: copyModelTiers(value.modelTiers),
    name: value.name,
    description: value.description,
    chunking_config: {
      chunk_size: value.chunkingConfig.chunkSize,
      chunk_overlap: value.chunkingConfig.chunkOverlap,
      separators: value.chunkingConfig.separators,
      enable_parent_child: value.chunkingConfig.enableParentChild,
      parent_chunk_size: value.chunkingConfig.parentChunkSize,
      child_chunk_size: value.chunkingConfig.childChunkSize,
      // Adaptive chunking fields are always sent (empty/zero values
      // included) so the user can clear them — backend uses pointer DTOs
      // to distinguish "not in payload" from "explicitly empty".
      strategy: value.chunkingConfig.strategy ?? '',
      token_limit: value.chunkingConfig.tokenLimit ?? 0,
      languages: value.chunkingConfig.languages ?? [],
    },
    embedding_model_id: value.modelTiers.embedding || '',
    summary_model_id: value.modelTiers.generation || '',
    indexing_strategy: { vector_enabled: value.indexingStrategy.vectorEnabled, keyword_enabled: value.indexingStrategy.keywordEnabled },
  }

  return data
}

async function handleSubmit() {
  if (saving.value || loading.value || !formData.value || !dirty.value) return
  if (!formData.value.name.trim()) { error.value = t('libraryEditor.messages.nameRequired'); currentSection.value = 'basic'; return }
  if (needsEmbedding.value && !formData.value.modelTiers.embedding) { error.value = t('libraryEditor.indexing.embeddingRequired'); currentSection.value = 'models'; return }
  saving.value = true; error.value = ''
  try {
    const data = buildSubmitData()
    const changes = changedSettings(buildSubmitData(JSON.parse(saved.value)), data)
    const original = loadedLibrary.value
    const config: Record<string, unknown> = {}
    for (const field of ['model_tiers', 'chunking_config', 'indexing_strategy']) {
      if (changes[field]) config[field] = { ...original[field], ...changes[field] }
    }
    const result: any = await updateLibrary({ name: data.name, description: data.description, ...(Object.keys(config).length ? { config } : {}) })
    if (!result.success) throw new Error(result.message || t('common.operationFailed'))
    for (const [field, value] of Object.entries(config)) original[field] = value
    original.name = data.name; original.description = data.description
    saved.value = JSON.stringify(formData.value)
    resources.invalidateLibraryDetail()
    window.dispatchEvent(new CustomEvent('reliquary:library-settings-saved'))
    toast.success(t('libraryEditor.messages.updateSuccess'))
  } catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
  finally { saving.value = false }
}
function reset() { if (saved.value && !saving.value) formData.value = JSON.parse(saved.value) }
async function confirmLeave(): Promise<boolean> {
  if (catalogRef.value?.confirmLeave && !await catalogRef.value.confirmLeave()) return false
  if (saving.value) return Promise.resolve(false)
  if (!dirty.value) return Promise.resolve(true)
  return new Promise(resolve => { const dialog = confirm({ header: t('common.discardTitle'), body: t('common.discardHint'), confirmBtn: t('common.discard'), cancelBtn: t('common.cancel'), onConfirm: () => { reset(); dialog.destroy(); resolve(true) }, onCancel: () => { dialog.destroy(); resolve(false) }, onClose: () => { dialog.destroy(); resolve(false) } }) })
}
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
defineExpose({ confirmLeave })
onMounted(() => { void load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { generation++; window.removeEventListener('beforeunload', beforeUnload) })
</script>
<style scoped>
.library-settings-tabs { display: flex; flex-wrap: wrap; gap: var(--space-2); margin-bottom: var(--space-4); }
.library-settings-tabs button { padding: var(--space-1) var(--space-3); border: 1px solid var(--border); border-radius: 0; background: var(--bg); color: var(--text); font: inherit; cursor: pointer; }
.library-settings-tabs button[aria-pressed='true'] { background: var(--text); color: var(--bg); }
.library-settings-tabs button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.library-settings-actions { display: flex; justify-content: flex-end; gap: var(--space-3); position: sticky; bottom: 0; background: var(--bg); padding: var(--space-3) 0; border-top: 2px solid var(--rule); }
.library-settings-error { color: var(--danger); background: var(--danger-soft); padding: var(--space-3); }
</style>
