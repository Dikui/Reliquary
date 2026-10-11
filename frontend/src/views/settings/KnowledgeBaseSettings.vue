<template>
  <section class="kb-settings">
    <div class="section-header"><h2>{{ $t('settings.knowledgeSettings') }}</h2></div>
    <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button v-if="!formData" variant="text" :loading="loading" @click="load">{{ $t('common.retry') }}</t-button></template></t-alert>
    <t-loading :loading="loading" show-overlay>
      <t-tabs v-model="currentSection"><t-tab-panel v-for="tab in tabs" :key="tab" :value="tab" :label="$t(tab === 'parser' ? 'settings.parserEngine' : `knowledgeEditor.sidebar.${tab}`)" /></t-tabs>
      <div class="kb-settings-body">
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

                  <ModelTierEditor v-if="formData" v-model="formData.modelTiers" :all-models="allModels" :before-navigate="confirmLeave" :disabled="saving" @resolved="resolvedModelTiers = $event" />
                  <EmbeddingRebuild v-if="formData && currentSection === 'models'" :model-id="formData.modelTiers.embedding || ''" :models="allModels" :disabled="dirty || saving" />
                </div>

                <!-- 解析引擎 -->
                <div v-if="formData && currentSection === 'parser'" class="section">
                  <KBParserSettings
                    :parser-engine-rules="formData.chunkingConfig.parserEngineRules"
                    @update:parser-engine-rules="handleParserEngineRulesUpdate"
                  />
                </div>

                <!-- 存储引擎 -->

                <!-- 分块设置 -->
                <div  v-show="currentSection === 'chunking'" class="section">
                  <KBChunkingSettings
                    v-if="formData"
                    :config="formData.chunkingConfig"
                    @update:config="handleChunkingConfigUpdate"
                  />
                </div>

                <!-- 高级设置 -->
                <div  v-show="currentSection === 'advanced'" class="section">
                  <KBAdvancedSettings

                    v-if="formData"

                    :table-metadata-instructions="formData.chunkingConfig.tableMetadataInstructions"
                    @update:table-metadata-instructions="(value: string) => { if (formData) formData.chunkingConfig.tableMetadataInstructions = value }"
                  />
                </div>

      </div>
    </t-loading>
    <div class="kb-settings-actions"><t-button variant="outline" :disabled="!dirty || saving || loading" @click="reset">{{ $t('common.cancel') }}</t-button><t-button theme="primary" :loading="saving" :disabled="!dirty || loading" @click="handleSubmit">{{ $t('common.save') }}</t-button></div>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { getKnowledgeBase, listEntryFiles, updateKnowledgeBase } from '@/api/knowledge-base'
import { useResourcesStore } from '@/stores/resources'
import { useUIStore } from '@/stores/ui'
import { changedSettings } from '@/utils/settingsPatch'
import { copyModelTiers } from '@/utils/modelTiers'
import type { ResolvedModelTiers } from '@/types/modelTiers'
import ModelTierEditor from '@/components/ModelTierEditor.vue'
import EmbeddingRebuild from '@/components/EmbeddingRebuild.vue'
import SafeTextarea from '@/components/SafeTextarea.vue'
import KBParserSettings from '@/views/knowledge-base/settings/KBParserSettings.vue'
import KBChunkingSettings from '@/views/knowledge-base/settings/KBChunkingSettings.vue'
import KBAdvancedSettings from '@/views/knowledge-base/settings/KBAdvancedSettings.vue'
const { t } = useI18n()
const props = defineProps<{ initialSection?: string }>()
const resources = useResourcesStore(), uiStore = useUIStore()
const tabs = ['basic', 'models', 'parser', 'chunking', 'advanced']
const initialSection = props.initialSection || uiStore.settingsInitialSubSection || 'basic'
const currentSection = ref(tabs.includes(initialSection) ? initialSection : 'basic')
const formData = ref<any>(null), saved = ref(''), loading = ref(false), saving = ref(false), error = ref('')
const activeKbId = ref(''), hasFiles = ref(false), initialIndexingStrategy = ref<any>(null)
const loadedKnowledgeBase = ref<any>(null)
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
    const [response] = await Promise.all([getKnowledgeBase(), resources.ensureModels()])
    if (request !== generation) return
    const kb = response?.data
    if (!kb) throw new Error(t('knowledgeEditor.messages.loadDataFailed'))
    loadedKnowledgeBase.value = kb
    activeKbId.value = kb.id
    const files: any = await listEntryFiles(kb.id, { page: 1, page_size: 1 })
    if (request !== generation) return
    hasFiles.value = (files?.total || 0) > 0
    formData.value = {

      modelTiers: copyModelTiers({ light: kb.summary_model_id || '', entity_resolution: kb.summary_model_id || '', generation: kb.summary_model_id || '', embedding: kb.embedding_model_id || '', ...kb.model_tiers }),
      name: kb.name || '',
      description: kb.description || '',

      chunkingConfig: {
        chunkSize: kb.chunking_config?.chunk_size || 512,
        // Fallback only used when the loaded KB has no chunk_overlap stored.
        // Aligned with chunker.DefaultChunkOverlap on the backend.
        chunkOverlap: kb.chunking_config?.chunk_overlap ?? 80,
        separators: kb.chunking_config?.separators || ['\n\n', '\n', '。', '！', '？', ';', '；'],
        parserEngineRules: kb.chunking_config?.parser_engine_rules || undefined,
        enableParentChild: kb.chunking_config?.enable_parent_child,
        parentChunkSize: kb.chunking_config?.parent_chunk_size || 4096,
        childChunkSize: kb.chunking_config?.child_chunk_size || 384,
        // Existing KBs without strategy field render as empty (= legacy behavior).
        // The user has to actively pick a value to opt in to the new tiers.
        strategy: kb.chunking_config?.strategy || '',
        tokenLimit: kb.chunking_config?.token_limit || 0,
        languages: kb.chunking_config?.languages || [],
        tableMetadataInstructions: kb.chunking_config?.table_metadata_instructions || ''
      },
      indexingStrategy: {
        vectorEnabled: kb.indexing_strategy?.vector_enabled ?? true,
        keywordEnabled: kb.indexing_strategy?.keyword_enabled ?? true,
      },
    }

    initialIndexingStrategy.value = { ...formData.value.indexingStrategy }
    saved.value = JSON.stringify(formData.value)
  } catch (cause: any) { if (request === generation) error.value = cause.message || t('knowledgeEditor.messages.loadDataFailed') }
  finally { if (request === generation) loading.value = false }
}
function handleChunkingConfigUpdate(value: any) { formData.value.chunkingConfig = { ...value } }
function handleParserEngineRulesUpdate(value: any) { formData.value.chunkingConfig.parserEngineRules = value }
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
      table_metadata_instructions: value.chunkingConfig.tableMetadataInstructions || '',
      parser_engine_rules: value.chunkingConfig.parserEngineRules || []
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
  if (!formData.value.modelTiers.generation) { error.value = t('knowledgeEditor.messages.summaryRequired'); currentSection.value = 'models'; return }
  saving.value = true; error.value = ''
  try {
    const data = buildSubmitData()
    const changes = changedSettings(buildSubmitData(JSON.parse(saved.value)), data)
    const original = loadedKnowledgeBase.value
    const config: Record<string, unknown> = {}
    for (const field of ['model_tiers', 'chunking_config', 'indexing_strategy']) {
      if (changes[field]) config[field] = { ...original[field], ...changes[field] }
    }
    const result: any = await updateKnowledgeBase({ name: data.name, description: data.description, ...(Object.keys(config).length ? { config } : {}) })
    if (!result.success) throw new Error(result.message || t('common.operationFailed'))
    for (const [field, value] of Object.entries(config)) original[field] = value
    original.name = data.name; original.description = data.description
    saved.value = JSON.stringify(formData.value)
    resources.invalidateKnowledgeBaseDetail()
    window.dispatchEvent(new CustomEvent('reliquary:kb-settings-saved'))
    MessagePlugin.success(t('knowledgeEditor.messages.updateSuccess'))
  } catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
  finally { saving.value = false }
}
function reset() { if (saved.value && !saving.value) formData.value = JSON.parse(saved.value) }
function confirmLeave(): Promise<boolean> {
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
.kb-settings-body { padding-top: 24px; min-height: 300px; }
.section-title, .section-header h2 { font-size: 20px; margin: 0 0 8px; }
.section-desc, .form-tip { color: var(--td-text-color-secondary); line-height: 1.6; font-size: 13px; }
.form-item { margin-bottom: 22px; }
.form-label { display: block; margin-bottom: 8px; font-weight: 500; }
.indexing-check-item { cursor: pointer; }
.is-locked .indexing-check-item { cursor: default; }
.kb-settings-actions { display: flex; justify-content: flex-end; gap: 12px; padding-top: 24px; }
// 多模态配置内联样式（与子组件 KBAdvancedSettings 一致）

</style>
