<template>
  <section class="library-settings">
    <div class="section-header"><h2>{{ $t('settings.librarySettings') }}</h2></div>
    <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button v-if="!formData" variant="text" :loading="loading" @click="load">{{ $t('common.retry') }}</t-button></template></t-alert>
    <t-loading :loading="loading" show-overlay>
      <t-tabs v-model="currentSection"><t-tab-panel v-for="tab in tabs" :key="tab" :value="tab" :label="tab === 'catalog' ? $t('ikb18.catalog') : $t('ikb18.sharedEmbedding')" /></t-tabs>
      <div class="library-settings-body">
        <LibraryCatalogSettings ref="catalogRef" v-show="currentSection === 'catalog'" @changed="load" />
                <!-- 基本信息 -->
                <div v-show="currentSection === 'basic'" class="section">
                  <div v-if="formData" class="section-content">
                    <div class="section-header">
                      <h3 class="section-title">{{ $t('knowledgeEditor.basic.title') }}</h3>
                      <p class="section-desc">{{ $t('knowledgeEditor.basic.description') }}</p>
                    </div>
                    <div class="section-body">

                      <!-- 索引策略 (紧跟类型选择) -->
                      <div  class="form-item">
                        <label class="form-label required">{{ $t('knowledgeEditor.indexing.title') }}</label>
                        <p class="form-tip">{{ $t('knowledgeEditor.indexing.description') }}</p>
                        <div class="indexing-checks" :class="{ 'is-locked': isIndexingLocked }">
                          <div
                            class="indexing-check-item"
                            :class="{ 'is-checked': formData.indexingStrategy.vectorEnabled, 'is-disabled': isIndexingLocked }"
                            @click="toggleVectorIndexing"
                          >
                            <t-checkbox
                              :checked="formData.indexingStrategy.vectorEnabled"
                              :disabled="isIndexingLocked"
                              class="indexing-check-box"
                            >{{ $t('knowledgeEditor.indexing.searchTitle') }}</t-checkbox>
                            <p class="indexing-check-desc">{{ $t('knowledgeEditor.indexing.searchDesc') }}</p>
                          </div>

                        </div>
                        <p v-if="isIndexingLocked" class="form-tip locked-tip">
                          {{ $t('knowledgeEditor.indexing.lockedTip') }}
                        </p>
                      </div>
                      <div class="form-item">
                        <label class="form-label required">{{ $t('knowledgeEditor.basic.nameLabel') }}</label>
                        <t-input
                          v-model="formData.name"
                          :placeholder="$t('knowledgeEditor.basic.namePlaceholder')"
                          :maxlength="50"
                        />
                      </div>
                      <div class="form-item">
                        <label class="form-label">{{ $t('knowledgeEditor.basic.descriptionLabel') }}</label>
                        <SafeTextarea
                          v-model="formData.description"
                          :placeholder="$t('knowledgeEditor.basic.descriptionPlaceholder')"
                          :maxlength="200"
                          :autosize="{ minRows: 3, maxRows: 6 }"
                        />
                      </div>

                    </div>
                  </div>
                </div>

                <!-- 模型配置 -->
                <div v-show="currentSection === 'models'" class="section">

                  <ModelTierEditor v-if="formData" v-model="formData.modelTiers" :tiers="['embedding']" scope="space" :all-models="allModels" :before-navigate="confirmLeave" :disabled="saving" @resolved="resolvedModelTiers = $event" />
                  <EmbeddingRebuild v-if="formData && currentSection === 'models'" :model-id="formData.modelTiers.embedding || ''" :models="allModels" :disabled="dirty || saving" />
                </div>


      </div>
    </t-loading>
    <div v-if="currentSection !== 'catalog'" class="library-settings-actions"><t-button variant="outline" :disabled="!dirty || saving || loading" @click="reset">{{ $t('common.cancel') }}</t-button><t-button theme="primary" :loading="saving" :disabled="!dirty || loading" @click="handleSubmit">{{ $t('common.save') }}</t-button></div>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { getLibrary, listEntryFiles, updateLibrary } from '@/api/library'
import { useResourcesStore } from '@/stores/resources'
import { useUIStore } from '@/stores/ui'
import { changedSettings } from '@/utils/settingsPatch'
import { copyModelTiers } from '@/utils/modelTiers'
import type { ResolvedModelTiers } from '@/types/modelTiers'
import ModelTierEditor from '@/components/ModelTierEditor.vue'
import EmbeddingRebuild from '@/components/EmbeddingRebuild.vue'
import LibraryCatalogSettings from './LibraryCatalogSettings.vue'
import SafeTextarea from '@/components/SafeTextarea.vue'

const { t } = useI18n()
const props = defineProps<{ initialSection?: string }>()
const resources = useResourcesStore(), uiStore = useUIStore()
const tabs = ['catalog', 'models']
const initialSection = props.initialSection || uiStore.settingsInitialSubSection || 'catalog'
const currentSection = ref(tabs.includes(initialSection) ? initialSection : 'catalog')
const catalogRef = ref<{ confirmLeave?: () => Promise<boolean> }>()
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
    if (response?.success === false) throw new Error(response.message || t('knowledgeEditor.messages.loadDataFailed'))
    const library = response?.data
    if (!library) { formData.value = null; saved.value = ''; return }
    loadedLibrary.value = library
    activeLibraryId.value = library.id
    const files: any = await listEntryFiles(library.id, { page: 1, page_size: 1 })
    if (request !== generation) return
    hasFiles.value = (files?.total || 0) > 0
    formData.value = {

      modelTiers: copyModelTiers({ light: library.summary_model_id || '', generation: library.summary_model_id || '', embedding: library.embedding_model_id || '', ...library.model_tiers }),
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
  } catch (cause: any) { if (request === generation) error.value = cause.message || t('knowledgeEditor.messages.loadDataFailed') }
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
  if (!formData.value.name.trim()) { error.value = t('knowledgeEditor.messages.nameRequired'); currentSection.value = 'basic'; return }
  if (needsEmbedding.value && !formData.value.modelTiers.embedding) { error.value = t('knowledgeEditor.indexing.embeddingRequired'); currentSection.value = 'models'; return }
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
    MessagePlugin.success(t('knowledgeEditor.messages.updateSuccess'))
  } catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
  finally { saving.value = false }
}
function reset() { if (saved.value && !saving.value) formData.value = JSON.parse(saved.value) }
async function confirmLeave(): Promise<boolean> {
  if (catalogRef.value?.confirmLeave && !await catalogRef.value.confirmLeave()) return false
  if (saving.value) return Promise.resolve(false)
  if (!dirty.value) return Promise.resolve(true)
  return new Promise(resolve => { const dialog = DialogPlugin.confirm({ header: t('common.discardTitle'), body: t('common.discardHint'), confirmBtn: t('common.discard'), cancelBtn: t('common.cancel'), onConfirm: () => { reset(); dialog.destroy(); resolve(true) }, onCancel: () => { dialog.destroy(); resolve(false) }, onClose: () => { dialog.destroy(); resolve(false) } }) })
}
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
defineExpose({ confirmLeave })
onMounted(() => { void load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { generation++; window.removeEventListener('beforeunload', beforeUnload) })
</script>
<style scoped lang="less">
@import '../../assets/settings-layout.less';
.library-settings-body { padding-top: 24px; min-height: 300px; }
.section-title, .section-header h2 { font-size: 20px; margin: 0 0 8px; }
.section-desc, .form-tip { color: var(--td-text-color-secondary); line-height: 1.6; font-size: 13px; }
.form-item { margin-bottom: 22px; }
.form-label { display: block; margin-bottom: 8px; font-weight: 500; }
.indexing-check-item { cursor: pointer; }
.is-locked .indexing-check-item { cursor: default; }
.library-settings-actions { display: flex; justify-content: flex-end; gap: 12px; padding-top: 24px; }
// 库设置的内联样式

</style>
