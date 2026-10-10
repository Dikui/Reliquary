<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="visible" class="settings-overlay" @click.self="handleClose">
        <div class="settings-modal">
          <div v-if="loading" class="editor-initializing" role="status" :aria-label="$t('common.loading')">
            <t-loading size="medium" :text="$t('common.loading')" />
          </div>
          <!-- 关闭按钮 -->
          <button class="close-btn" @click="handleClose" :aria-label="$t('general.close')">
            <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor">
              <path d="M15 5L5 15M5 5L15 15" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
            </svg>
          </button>

          <div class="settings-container">
            <!-- 左侧导航 -->
            <div class="settings-sidebar">
              <div class="sidebar-header">
                <h2 class="sidebar-title">{{ editorMode === 'create' ? $t('knowledgeEditor.titleCreate') : $t('knowledgeEditor.titleEdit') }}</h2>
              </div>
              <t-select v-model="currentSection" class="mobile-section-select"
                :aria-label="$t('general.settings')"
                :options="navItems.map(item => ({ value: item.key, label: item.label }))" />
              <div class="settings-nav">
                <template v-for="group in navGroups" :key="group.key">
                  <div class="nav-group-title">{{ group.label }}</div>
                  <div
                    v-for="(item, index) in group.items"
                    :key="index"
                    :class="['nav-item', { 'active': currentSection === item.key }]"
                    @click="currentSection = item.key"
                  >
                    <t-icon :name="item.icon" class="nav-icon" />
                    <span class="nav-label">{{ item.label }}</span>
                    <span v-if="item.badge" class="nav-badge">{{ item.badge }}</span>
                  </div>
                </template>
              </div>
            </div>

            <!-- 右侧内容区域 -->
            <div class="settings-content">
              <div class="content-wrapper">
                <!-- 基本信息 -->
                <div v-show="currentSection === 'basic'" class="section">
                  <div v-if="formData" class="section-content">
                    <div class="section-header">
                      <h3 class="section-title">{{ $t('knowledgeEditor.basic.title') }}</h3>
                      <p class="section-desc">{{ $t('knowledgeEditor.basic.description') }}</p>
                    </div>
                    <div class="section-body">
                      <div v-if="editorMode === 'edit' && activeKbId" class="form-item">
                        <label class="form-label">{{ $t('knowledgeEditor.basic.kbId') }}</label>
                        <p class="form-tip">{{ isPostCreateSession ? $t('knowledgeEditor.postCreateHint.followUpDesc') : $t('knowledgeEditor.basic.kbIdDesc') }}</p>
                        <div class="kb-id-field">
                          <code class="kb-id-value" :title="activeKbId">{{ activeKbId }}</code>
                          <t-tooltip :content="$t('common.copy')" placement="top">
                            <t-button theme="default" size="small" variant="text" class="kb-id-copy"
                              @click="copyKbId">
                              <t-icon name="file-copy" />
                            </t-button>
                          </t-tooltip>
                        </div>
                      </div>

                      <div class="form-item">
                        <label class="form-label required">{{ $t('knowledgeEditor.basic.typeLabel') }}</label>
                        <t-radio-group
                          v-model="formData.type"
                          :disabled="editorMode === 'edit'"
                        >
                          <t-radio-button value="document">{{ $t('knowledgeEditor.basic.typeDocument') }}</t-radio-button>

                        </t-radio-group>
                        <p class="form-tip">{{ $t('knowledgeEditor.basic.typeDescription') }}</p>
                      </div>

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
                  <KBModelConfig
                    ref="modelConfigRef"
                    v-if="formData"
                    :config="formData.modelConfig"
                    :has-files="hasFiles"
                    :rag-enabled="formData.indexingStrategy?.vectorEnabled || formData.indexingStrategy?.keywordEnabled"
                    :all-models="allModels"
                    @update:config="handleModelConfigUpdate"
                  />
                  <ModelTierEditor v-if="formData" v-model="formData.modelTiers" scope="knowledge_base" :all-models="allModels" :legacy-tiers="{ light: formData.modelConfig.llmModelId, entity_resolution: formData.modelConfig.llmModelId, generation: formData.modelConfig.llmModelId, embedding: formData.modelConfig.embeddingModelId }" :disabled="saving" :embedding-locked="hasFiles && kbCreateNeedsEmbedding" @resolved="resolvedModelTiers = $event" />
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

                <!-- 多模态配置 -->
                <div  v-show="currentSection === 'multimodal'" class="section">
                  <div v-if="formData" class="kb-multimodal-settings">
                    <div class="section-header">
                      <h2>{{ $t('knowledgeEditor.multimodal.title') }}</h2>
                      <p class="section-description">{{ $t('knowledgeEditor.multimodal.description') }}</p>
                    </div>

                    <div class="settings-group">
                      <!-- 多模态开关 -->
                      <div class="setting-row">
                        <div class="setting-info">
                          <label>{{ $t('knowledgeEditor.advanced.multimodal.label') }}</label>
                          <p class="desc">{{ $t('knowledgeEditor.advanced.multimodal.description') }}</p>
                        </div>
                        <div class="setting-control">
                          <t-switch
                            v-model="formData.multimodalConfig.enabled"
                            @change="handleMultimodalToggle"
                            size="medium"
                          />
                        </div>
                      </div>

                      <!-- VLLM 模型选择（多模态启用时） -->
                      <div v-if="formData.multimodalConfig.enabled" class="setting-row">
                        <div class="setting-info">
                          <label>{{ $t('knowledgeEditor.advanced.multimodal.vllmLabel') }} <span class="required">*</span></label>
                          <p class="desc">{{ $t('knowledgeEditor.advanced.multimodal.vllmDescription') }}</p>
                        </div>
                        <div class="setting-control">
                          <ModelSelector
                            model-type="VLLM"
                            :selected-model-id="formData.multimodalConfig.vllmModelId"
                            :all-models="allModels"
                            @update:selected-model-id="handleMultimodalVLLMChange"
                            @add-model="handleAddVLLMModel"
                            :placeholder="$t('knowledgeEditor.advanced.multimodal.vllmPlaceholder')"
                          />
                        </div>
                      </div>

                      <div v-if="formData.multimodalConfig.enabled" class="setting-row">
                        <div class="setting-info">
                          <label>{{ $t('knowledgeEditor.advanced.multimodal.descriptionLanguageLabel') }}</label>
                          <p class="desc">{{ $t('knowledgeEditor.advanced.multimodal.descriptionLanguageDescription') }}</p>
                        </div>
                        <div class="setting-control">
                          <t-select v-model="formData.multimodalConfig.descriptionLanguage" clearable
                            :placeholder="$t('knowledgeEditor.advanced.multimodal.descriptionLanguageAuto')">
                            <t-option value="Chinese" :label="$t('language.zhCN')" />
                            <t-option value="English" :label="$t('language.enUS')" />
                            <t-option value="Korean" :label="$t('language.koKR')" />
                            <t-option value="Russian" :label="$t('language.ruRU')" />
                          </t-select>
                        </div>
                      </div>

                      <div v-if="formData.multimodalConfig.enabled" class="setting-row setting-row-vertical">
                        <div class="setting-info">
                          <label>{{ $t('knowledgeEditor.advanced.multimodal.customInstructionsLabel') }}</label>
                          <p class="desc">{{ $t('knowledgeEditor.advanced.multimodal.customInstructionsDescription') }}</p>
                        </div>
                        <div class="setting-control setting-control-full">
                          <SafeTextarea v-model="formData.multimodalConfig.customInstructions"
                            :placeholder="$t('knowledgeEditor.advanced.multimodal.customInstructionsPlaceholder')"
                            :maxlength="4000" :autosize="{ minRows: 3, maxRows: 8 }" />
                        </div>
                      </div>
                    </div>
                  </div>
                </div>

                <!-- 高级设置 -->
                <div  v-show="currentSection === 'advanced'" class="section">
                  <KBAdvancedSettings
                    ref="advancedSettingsRef"
                    v-if="formData"
                    :question-generation="formData.questionGenerationConfig"
                    :rag-enabled="formData.indexingStrategy?.vectorEnabled || formData.indexingStrategy?.keywordEnabled"
                    :all-models="allModels"
                    :table-metadata-instructions="formData.chunkingConfig.tableMetadataInstructions"
                    @update:question-generation="handleQuestionGenerationUpdate"
                    @update:table-metadata-instructions="(value: string) => { if (formData) formData.chunkingConfig.tableMetadataInstructions = value }"
                  />
                </div>

                <!-- 数据源管理（仅编辑模式） -->
                <div v-if="editorMode === 'edit' && activeKbId && currentSection === 'datasource'" class="section">
                  <DataSourceSettings :kb-id="activeKbId" @count="dsCount = $event" />
                </div>
              </div>

              <!-- 保存按钮 -->
              <div class="settings-footer">
                <p v-if="isPostCreateSession" class="settings-footer-note">
                  <t-icon name="check-circle-filled" class="settings-footer-note__icon" />
                  <span>
                    <strong>{{ $t('knowledgeEditor.postCreateHint.title') }}</strong>
                    {{ $t('knowledgeEditor.postCreateHint.footer') }}
                  </span>
                </p>
                <div class="settings-footer-actions">
                  <t-button theme="default" variant="outline" @click="handleClose">
                    {{ $t('common.cancel') }}
                  </t-button>
                  <t-button theme="primary" @click="handleSubmit" :loading="saving"
                    :disabled="loading">
                    {{ saveButtonLabel }}
                  </t-button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>

</template>

<script setup lang="ts">
import SafeTextarea from '@/components/SafeTextarea.vue'
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { KB_EDITOR_FOCUS_SECTION_EVENT } from '@/config/editorNavigation'
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next'
import { createKnowledgeBase, getKnowledgeBaseById, listKnowledgeFiles, updateKnowledgeBase, rebuildKBIndex } from '@/api/knowledge-base'
import { updateKBConfig, type KBModelConfigRequest } from '@/api/initialization'
import { useResourcesStore } from '@/stores/resources'
import { selectInitialModelId } from '@/utils/modelDefaults'
import { copyWithToast } from '@/utils/clipboard'
import { useEditorResourcesStore } from '@/stores/editorResources'
import { useUIStore } from '@/stores/ui'
import ModelTierEditor from '@/components/ModelTierEditor.vue'
import { copyModelTiers } from '@/utils/modelTiers'
import type { ResolvedModelTiers } from '@/types/modelTiers'
import KBModelConfig from './settings/KBModelConfig.vue'
import KBParserSettings from './settings/KBParserSettings.vue'
import KBChunkingSettings from './settings/KBChunkingSettings.vue'
import KBAdvancedSettings from './settings/KBAdvancedSettings.vue'
import ModelSelector from '@/components/ModelSelector.vue'
import DataSourceSettings from './settings/DataSourceSettings.vue'
import { useI18n } from 'vue-i18n'

const uiStore = useUIStore()
const resources = useResourcesStore()
const editorResources = useEditorResourcesStore()
const { t } = useI18n()

// Props
const props = defineProps<{
  visible: boolean
  mode: 'create' | 'edit'
  kbId?: string
  initialType?: 'document'
}>()

// Emits
const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
  (e: 'success', kbId: string): void
}>()

/** 首次保存创建成功后留在弹窗内，继续调整设置。 */
const resolvedModelTiers = ref<ResolvedModelTiers>({})
const savedModelTiers = ref('')
const savedKbId = ref<string | null>(null)
const editorMode = computed(() => (savedKbId.value ? 'edit' : props.mode))
const activeKbId = computed(() => savedKbId.value ?? props.kbId)
const isPostCreateSession = computed(() => !!savedKbId.value)
const saveButtonLabel = computed(() =>
  editorMode.value === 'create'
    ? t('knowledgeEditor.buttons.create')
    : t('knowledgeEditor.buttons.saveAndClose')
)

const copyKbId = async () => {
  await copyWithToast(activeKbId.value, 'common.copied')
}

const currentSection = ref<string>('basic')

const onKbEditorFocusSection = (event: Event) => {
  const section = (event as CustomEvent<{ section?: string }>).detail?.section
  if (section) {
    currentSection.value = section
  }
}

onMounted(() => {
  window.addEventListener(KB_EDITOR_FOCUS_SECTION_EVENT, onKbEditorFocusSection)
})

onBeforeUnmount(() => {
  window.removeEventListener(KB_EDITOR_FOCUS_SECTION_EVENT, onKbEditorFocusSection)
})
const saving = ref(false)
const loading = ref(false)
const allModels = ref<any[]>([])
const hasFiles = ref(false)
const initialIndexingStrategy = ref<any>(null)
const dsCount = ref(0)

// 用户是否在分块设置中手动改过任何值。一旦为 true，就不再根据索引策略自动调整默认分块参数。
const chunkingDirty = ref(false)

// DefaultChunkOverlap on the backend so a freshly created KB uses
// the same numbers whether the editor sets them or the splitter
// falls back to its package defaults.
const DEFAULT_CHUNKING_PRESET = {
  chunkSize: 512,
  chunkOverlap: 80,
  enableParentChild: true,
} as const

const navItems = computed(() => {
  const items: { key: string; icon: string; label: string; badge?: number }[] = [
    { key: 'basic', icon: 'info-circle', label: t('knowledgeEditor.sidebar.basic') },
    { key: 'models', icon: 'control-platform', label: t('knowledgeEditor.sidebar.models') },
  ]

    items.push(
      { key: 'parser', icon: 'file-search', label: t('settings.parserEngine') },
      { key: 'multimodal', icon: 'image', label: t('knowledgeEditor.sidebar.multimodal') },
      { key: 'chunking', icon: 'file-copy', label: t('knowledgeEditor.sidebar.chunking') },
      { key: 'advanced', icon: 'setting', label: t('knowledgeEditor.sidebar.advanced') }
    )
    if (editorMode.value === 'edit' && activeKbId.value) {
      items.push({ key: 'datasource', icon: 'cloud-download', label: t('knowledgeEditor.sidebar.datasource'), badge: dsCount.value || undefined })
    }

  return items
})

// 左侧导航分组
const navGroups = computed(() => {
  const itemMap = new Map(navItems.value.map((item) => [item.key, item]))
  const pickItems = (keys: string[]) =>
    keys.map((key) => itemMap.get(key)).filter(Boolean) as typeof navItems.value
  return [
    {
      key: 'basic',
      label: t('knowledgeEditor.navGroups.basic'),
      items: pickItems(['basic', 'models']),
    },
    {
      key: 'processing',
      label: t('knowledgeEditor.navGroups.processing'),
      items: pickItems(['parser', 'chunking', 'multimodal', 'advanced']),
    },
    {
      key: 'data',
      label: t('knowledgeEditor.navGroups.data'),
      items: pickItems(['datasource']),
    },
    {
      key: 'management',
      label: t('knowledgeEditor.navGroups.management'),
      items: [],
    },
  ].filter((group) => group.items.length > 0)
})

// 模型配置引用
const modelConfigRef = ref<InstanceType<typeof KBModelConfig>>()
const advancedSettingsRef = ref<InstanceType<typeof KBAdvancedSettings>>()

// 表单数据
const formData = ref<any>(null)

const kbCreateNeedsEmbedding = computed(() => {
  if (!formData.value) return false
  const s = formData.value.indexingStrategy
  return Boolean(s?.vectorEnabled || s?.keywordEnabled)
})

const applyDefaultModelsIfEmpty = () => {
  if (!formData.value || editorMode.value !== 'create') return
  const chatModelId = selectInitialModelId(allModels.value, 'KnowledgeQA')
  const embeddingModelId = selectInitialModelId(allModels.value, 'Embedding')
  if (!formData.value.modelConfig.llmModelId && chatModelId) {
    formData.value.modelConfig.llmModelId = chatModelId
  }
  if (!formData.value.modelConfig.embeddingModelId && embeddingModelId) {
    formData.value.modelConfig.embeddingModelId = embeddingModelId
  }
}

watch(
  () => formData.value?.type,
  (newType, oldType) => {
    if (!formData.value) return

  }
)

// 初始化表单数据
const initFormData = (type: 'document' = 'document') => {
  return {
    type,
    name: '',
    description: '',
    modelTiers: copyModelTiers(),
    modelConfig: {
      llmModelId: '',
      embeddingModelId: '',
    },
    chunkingConfig: {
      chunkSize: 512,
      // 80 ≈ 15% of chunkSize — community-recommended sweet spot.
      // Aligned with chunker.DefaultChunkOverlap on the backend.
      chunkOverlap: 80,
      separators: ['\n\n', '\n', '。', '！', '？', ';', '；'],
      parserEngineRules: undefined as any,
      enableParentChild: true,
      parentChunkSize: 4096,
      childChunkSize: 384,
      // New KBs default to the adaptive auto-strategy. User can change in the UI.
      strategy: 'auto' as string,
      tokenLimit: 0,
      languages: [] as string[],
      tableMetadataInstructions: ''
    },
    multimodalConfig: {
      enabled: false,
      vllmModelId: '',
      descriptionLanguage: '',
      customInstructions: ''
    },
    questionGenerationConfig: {
      enabled: true,
      questionCount: 3,
      customInstructions: ''
    },
    indexingStrategy: {
      vectorEnabled: true,
      keywordEnabled: true,
    },
  }
}

// 加载所有模型
const loadAllModels = async (force = false) => {
  try {
    await resources.ensureModels(force)
    allModels.value = resources.allModels || []
  } catch (error) {
    console.error('Failed to load model list:', error)
    MessagePlugin.error(t('knowledgeEditor.messages.loadModelsFailed'))
    allModels.value = []
  }
}

let kbEditorLoadGeneration = 0

const isCurrentKBLoad = (generation: number, kbId: string) => (
  generation === kbEditorLoadGeneration
  && props.visible
  && activeKbId.value === kbId
)

// 加载知识库数据（编辑模式）
const loadKBData = async (
  kbIdOverride?: string,
  generation = kbEditorLoadGeneration,
) => {
  const kbId = kbIdOverride ?? activeKbId.value
  if (editorMode.value !== 'edit' || !kbId) return

  loading.value = true
  try {
    const [kbInfo, filesResult] = await Promise.all([
      getKnowledgeBaseById(kbId),
      listKnowledgeFiles(kbId, { page: 1, page_size: 1 })
    ])

    if (!isCurrentKBLoad(generation, kbId)) return

    if (!kbInfo || !kbInfo.data) {
      throw new Error(t('knowledgeEditor.messages.notFound'))
    }

    const kb = kbInfo.data
    hasFiles.value = (filesResult as any)?.total > 0
    savedModelTiers.value = JSON.stringify(copyModelTiers(kb.model_tiers))

    // 设置表单数据
    const kbType = (kb.type as 'document') || 'document'
    formData.value = {
      type: kbType,
      modelTiers: copyModelTiers(kb.model_tiers),
      name: kb.name || '',
      description: kb.description || '',
      modelConfig: {
        llmModelId: kb.summary_model_id || '',
        embeddingModelId: kb.embedding_model_id || '',
      },
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
      multimodalConfig: {
        enabled: !!kb.vlm_config?.enabled,
        vllmModelId: kb.vlm_config?.model_id || '',
        descriptionLanguage: kb.vlm_config?.description_language || '',
        customInstructions: kb.vlm_config?.custom_instructions || ''
      },
      questionGenerationConfig: {
        enabled: kb.question_generation_config?.enabled,
        questionCount: kb.question_generation_config?.question_count || 3,
        customInstructions: kb.question_generation_config?.custom_instructions || ''
      },
        indexingStrategy: {
        vectorEnabled: kb.indexing_strategy?.vector_enabled ?? true,
        keywordEnabled: kb.indexing_strategy?.keyword_enabled ?? true,
      },
    }
    initialIndexingStrategy.value = { ...formData.value.indexingStrategy }
  } catch (error) {
    if (!isCurrentKBLoad(generation, kbId)) return
    console.error('Failed to load knowledge base data:', error)
    MessagePlugin.error(t('knowledgeEditor.messages.loadDataFailed'))
    handleClose()
  } finally {
    if (isCurrentKBLoad(generation, kbId)) {
      loading.value = false
    }
  }
}

// 处理配置更新
const handleModelConfigUpdate = (config: any) => {
  if (formData.value) {
    formData.value.modelConfig = { ...config }
  }
}

const isIndexingLocked = computed(() => editorMode.value === 'edit' && hasFiles.value)

const toggleVectorIndexing = () => {
  if (!formData.value) return
  if (isIndexingLocked.value) return
  const next = !formData.value.indexingStrategy.vectorEnabled
  formData.value.indexingStrategy.vectorEnabled = next
  formData.value.indexingStrategy.keywordEnabled = next
}

const handleChunkingConfigUpdate = (config: any) => {
  if (formData.value) {
    formData.value.chunkingConfig = { ...config }
    // 用户已经手动触达分块设置，后续索引策略切换不再覆盖这些值
    chunkingDirty.value = true
  }
}

const handleParserEngineRulesUpdate = (rules: any[]) => {
  if (formData.value) {
    formData.value.chunkingConfig.parserEngineRules = rules?.length ? rules : undefined
  }
}

const handleMultimodalToggle = () => {
  if (formData.value && !formData.value.multimodalConfig.enabled) {
    formData.value.multimodalConfig.vllmModelId = ''
  }
}

const handleMultimodalVLLMChange = (modelId: string) => {
  if (formData.value) {
    formData.value.multimodalConfig.vllmModelId = modelId
  }
}

const handleAddVLLMModel = () => {
  uiStore.openSettings('models', 'vllm')
}

const handleQuestionGenerationUpdate = (config: any) => {
  if (formData.value) {
    formData.value.questionGenerationConfig = { ...config }
  }
}

// 验证表单
const validateForm = (): boolean => {
  if (!formData.value) return false


  // 验证基本信息
  if (!formData.value.name || !formData.value.name.trim()) {
    MessagePlugin.warning(t('knowledgeEditor.messages.nameRequired'))
    currentSection.value = 'basic'
    return false
  }

  // 验证索引策略 — 文档类型至少需要开启一种

    const s = formData.value.indexingStrategy
    if (s && !s.vectorEnabled && !s.keywordEnabled) {
      MessagePlugin.warning(t('knowledgeEditor.indexing.atLeastOne'))
      currentSection.value = 'basic'
      return false
    }


  // 验证模型配置 - embedding 模型仅在检索索引启用时必须
  const needsEmbedding = formData.value.indexingStrategy?.vectorEnabled || formData.value.indexingStrategy?.keywordEnabled
  if (needsEmbedding && !formData.value.modelConfig.embeddingModelId && !resolvedModelTiers.value.embedding?.model_id) {
    MessagePlugin.warning(t('knowledgeEditor.indexing.embeddingRequired'))
    currentSection.value = 'models'
    return false
  }

  if (!formData.value.modelConfig.llmModelId && !resolvedModelTiers.value.generation?.model_id) {
    MessagePlugin.warning(t('knowledgeEditor.messages.summaryRequired'))
    currentSection.value = 'models'
    return false
  }

  // 验证多模态配置（如果启用）
  if (formData.value.multimodalConfig.enabled && !formData.value.multimodalConfig.vllmModelId) {
    MessagePlugin.warning(t('knowledgeEditor.messages.multimodalInvalid'))
    currentSection.value = 'multimodal'
    return false
  }


  return true
}

// 构建提交数据
const buildSubmitData = () => {
  if (!formData.value) return null

  const data: any = {
    model_tiers: copyModelTiers(formData.value.modelTiers),
    name: formData.value.name,
    description: formData.value.description,
    type: formData.value.type,
    chunking_config: {
      chunk_size: formData.value.chunkingConfig.chunkSize,
      chunk_overlap: formData.value.chunkingConfig.chunkOverlap,
      separators: formData.value.chunkingConfig.separators,
      enable_parent_child: formData.value.chunkingConfig.enableParentChild,
      parent_chunk_size: formData.value.chunkingConfig.parentChunkSize,
      child_chunk_size: formData.value.chunkingConfig.childChunkSize,
      // Adaptive chunking fields are always sent (empty/zero values
      // included) so the user can clear them — backend uses pointer DTOs
      // to distinguish "not in payload" from "explicitly empty".
      strategy: formData.value.chunkingConfig.strategy ?? '',
      token_limit: formData.value.chunkingConfig.tokenLimit ?? 0,
      languages: formData.value.chunkingConfig.languages ?? [],
      table_metadata_instructions: formData.value.chunkingConfig.tableMetadataInstructions || '',
      ...(formData.value.chunkingConfig.parserEngineRules?.length
        ? { parser_engine_rules: formData.value.chunkingConfig.parserEngineRules }
        : {})
    },
    embedding_model_id: formData.value.modelConfig.embeddingModelId || resolvedModelTiers.value.embedding?.model_id || '',
    summary_model_id: formData.value.modelConfig.llmModelId || resolvedModelTiers.value.generation?.model_id || ''
  }

  // 添加多模态配置
  data.vlm_config = {
    enabled: formData.value.multimodalConfig.enabled,
    model_id: formData.value.multimodalConfig.enabled
      ? (formData.value.multimodalConfig.vllmModelId || '')
      : '',
    description_language: formData.value.multimodalConfig.descriptionLanguage || '',
    custom_instructions: formData.value.multimodalConfig.customInstructions || ''
  }



  // 添加问题生成配置
  if (formData.value.questionGenerationConfig?.enabled) {
    data.question_generation_config = {
      enabled: true,
      question_count: formData.value.questionGenerationConfig.questionCount || 3,
      custom_instructions: formData.value.questionGenerationConfig.customInstructions || ''
    }
  } else {
    data.question_generation_config = {
      enabled: false,
      question_count: 3,
      custom_instructions: formData.value.questionGenerationConfig?.customInstructions || ''
    }
  }



  // Send indexing strategy

    data.indexing_strategy = {
      vector_enabled: formData.value.indexingStrategy?.vectorEnabled ?? true,
      keyword_enabled: formData.value.indexingStrategy?.keywordEnabled ?? true,
    }


  return data
}

// 提交表单
const handleSubmit = async () => {
  if (!validateForm()) {
    return
  }

  // 编辑模式下，若已有文件且存储引擎发生了变化，弹窗确认

  doSubmit()
}

const doSubmit = async () => {
  saving.value = true
  try {
    const data = buildSubmitData()
    if (!data) {
      throw new Error(t('knowledgeEditor.messages.buildDataFailed'))
    }

    if (editorMode.value === 'create') {
      // 创建模式：一次性创建知识库及所有配置
      const result: any = await createKnowledgeBase(data)
      if (!result.success || !result.data?.id) {
        throw new Error(result.message || t('knowledgeEditor.messages.createFailed'))
      }
      const createdKbId = result.data.id as string
      savedKbId.value = createdKbId
      currentSection.value = 'basic'
      await loadKBData(createdKbId)
      MessagePlugin.success(t('knowledgeEditor.messages.createSuccess'))
      emit('success', createdKbId)
    } else {
      // 编辑模式：分别更新基本信息和配置
      const kbId = activeKbId.value
      if (!kbId) {
        throw new Error(t('knowledgeEditor.messages.missingId'))
      }

      const updateConfig: any = {}

      {}
        updateConfig.indexing_strategy = {
          vector_enabled: formData.value.indexingStrategy?.vectorEnabled ?? true,
          keyword_enabled: formData.value.indexingStrategy?.keywordEnabled ?? true,
        }

      await updateKnowledgeBase(kbId, {
        model_tiers: data.model_tiers,
        name: data.name,
        description: data.description,
        config: updateConfig
      })

      // 2. 更新完整配置（模型、分块、多模态、存储引擎等）
      const config: KBModelConfigRequest = {
        llmModelId: data.summary_model_id,
        embeddingModelId: data.embedding_model_id,
        vlm_config: data.vlm_config,
        documentSplitting: {
          chunkSize: data.chunking_config.chunk_size,
          chunkOverlap: data.chunking_config.chunk_overlap,
          separators: data.chunking_config.separators,
          parserEngineRules: data.chunking_config.parser_engine_rules || undefined,
          enableParentChild: data.chunking_config.enable_parent_child,
          parentChunkSize: data.chunking_config.parent_chunk_size || 4096,
          childChunkSize: data.chunking_config.child_chunk_size || 384,
          // Always send strategy / tokenLimit / languages — backend treats
          // empty/0/[] as a valid clear, so we must include them in the
          // payload to let users reset back to defaults.
          strategy: formData.value?.chunkingConfig.strategy ?? '',
          tokenLimit: formData.value?.chunkingConfig.tokenLimit ?? 0,
          languages: formData.value?.chunkingConfig.languages ?? [],
          tableMetadataInstructions: formData.value?.chunkingConfig.tableMetadataInstructions ?? ''
        },
        multimodal: {
          enabled: !!data.vlm_config?.enabled
        },
        questionGeneration: {
          enabled: data.question_generation_config?.enabled,
          questionCount: data.question_generation_config?.question_count || 3,
          customInstructions: data.question_generation_config?.custom_instructions || ''
        }
      }

      await updateKBConfig(kbId, config)
      MessagePlugin.success(t('knowledgeEditor.messages.updateSuccess'))

      // Check if indexing strategy changed and offer rebuild
      if (hasFiles.value && initialIndexingStrategy.value && formData.value) {
        const curr = formData.value.indexingStrategy
        const prev = initialIndexingStrategy.value
        const strategyChanged = (
          curr.vectorEnabled !== prev.vectorEnabled ||
          curr.keywordEnabled !== prev.keywordEnabled
        )
        if (strategyChanged) {
          const dialog = DialogPlugin.confirm({
            header: t('knowledgeEditor.indexing.rebuildConfirmTitle'),
            body: t('knowledgeEditor.indexing.rebuildConfirmBody', { count: '...' }),
            confirmBtn: t('common.confirm'),
            cancelBtn: t('common.cancel'),
            onConfirm: async () => {
              dialog.destroy()
              try {
                const result: any = await rebuildKBIndex(kbId)
                const count = result?.data?.document_count ?? 0
                MessagePlugin.success(t('knowledgeEditor.indexing.rebuildSuccess', { count }))
              } catch (e) {
                console.error('Rebuild index failed:', e)
              }
            },
            onCancel: () => {
              dialog.destroy()
              MessagePlugin.info(t('knowledgeEditor.indexing.rebuildSkip'))
            },
          })
        }
      }

      savedModelTiers.value = JSON.stringify(copyModelTiers(formData.value.modelTiers))
      saving.value = false
      emit('success', kbId)
      handleClose()
    }
  } catch (error: any) {
    console.error('Knowledge base operation failed:', error)
    MessagePlugin.error(error?.message || t('common.operationFailed'))
  } finally {
    saving.value = false
  }
}

// 重置所有状态
const resetState = () => {
  savedModelTiers.value = JSON.stringify(copyModelTiers())
  resolvedModelTiers.value = {}
  savedKbId.value = null
  currentSection.value = 'basic'
  formData.value = null
  hasFiles.value = false
  initialIndexingStrategy.value = null
  saving.value = false
  loading.value = false
  chunkingDirty.value = false
}

// 关闭弹窗
const handleClose = () => {
  if (saving.value) return
  if (formData.value && JSON.stringify(copyModelTiers(formData.value.modelTiers)) !== savedModelTiers.value) {
    const dialog = DialogPlugin.confirm({ header: t('common.discardTitle'), body: t('common.discardHint'), confirmBtn: t('common.discard'), cancelBtn: t('common.cancel'), theme: 'warning', onConfirm: () => { savedModelTiers.value = JSON.stringify(copyModelTiers(formData.value?.modelTiers)); dialog.hide(); handleClose() } })
    return
  }
  emit('update:visible', false)
  setTimeout(() => {
    if (props.visible) return
    resetState()
  }, 300)
}

// 监听弹窗打开/关闭
watch(() => props.visible, async (newVal) => {
  const generation = ++kbEditorLoadGeneration
  if (newVal) {
    // 打开弹窗时，先重置状态
    resetState()
    loading.value = true
    const targetKbId = props.kbId

    // 检查是否有初始 section，如果有则跳转
    if (uiStore.kbEditorInitialSection) {
      currentSection.value = uiStore.kbEditorInitialSection
    }

    // 加载模型列表与空间默认存储引擎（创建 KB 时即使用，不依赖是否打开「存储引擎」Tab）
    await loadAllModels()

    if (generation !== kbEditorLoadGeneration || !props.visible) return

    // 根据模式加载数据
    if (props.mode === 'edit' && targetKbId) {
      await loadKBData(targetKbId, generation)
    } else {
      // 创建模式：初始化空表单，并预填空间默认存储引擎
      formData.value = initFormData(props.initialType || 'document')
      hasFiles.value = false
      applyDefaultModelsIfEmpty()
      loading.value = false
    }
  } else {
    // 关闭弹窗时，延迟重置状态（等待动画结束）
    setTimeout(() => {
      if (props.visible) return
      resetState()
      currentSection.value = 'basic' // 重置为默认 section
    }, 300)
  }
})

// 监听全局设置弹窗关闭后刷新模型列表
watch(
  () => uiStore.showSettingsModal,
  async (visible, previous) => {
    if (!visible && previous && props.visible) {
      await loadAllModels(true)
    }
  }
)

watch(() => resources.allModels, (list) => {
  if (props.visible) {
    allModels.value = list || []
  }
})
</script>

<style scoped lang="less">
// 复用创建知识库的样式
.settings-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  backdrop-filter: blur(4px);
}

.settings-modal {
  position: relative;
  width: 90vw;
  max-width: 1000px;
  height: 85vh;
  max-height: 750px;
  background: var(--td-bg-color-container);
  border-radius: 12px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.12);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.editor-initializing {
  position: absolute;
  inset: 0;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--td-bg-color-container);
}

.close-btn {
  position: absolute;
  top: 20px;
  right: 20px;
  width: 32px;
  height: 32px;
  border: none;
  background: var(--td-bg-color-secondarycontainer);
  border-radius: 6px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--td-text-color-secondary);
  transition: all 0.2s ease;
  z-index: 10;

  &:hover {
    background: var(--td-bg-color-secondarycontainer);
    color: var(--td-text-color-primary);
  }
}

.settings-container {
  display: flex;
  height: 100%;
  width: 100%;
  overflow: hidden;
}

/* 左侧导航 */
.settings-sidebar {
  width: 208px;
  background-color: var(--td-bg-color-settings-modal);
  border-right: 1px solid var(--td-component-stroke);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.sidebar-header {
  padding: 16px 14px 12px;
  border-bottom: 1px solid var(--td-component-stroke);
  flex-shrink: 0;
}

.sidebar-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--td-text-color-primary);
}

.settings-nav {
  flex: 1;
  padding: 8px 8px 12px;
  overflow-y: auto;
  min-height: 0;
}

.nav-group-title {
  padding: 6px 14px 2px;
  color: var(--td-text-color-placeholder);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;

  .settings-nav > &:first-child {
    padding-top: 2px;
  }

  .settings-nav > &:not(:first-child) {
    padding-top: 8px;
  }
}

.nav-item {
  display: flex;
  align-items: center;
  padding: 6px 12px;
  margin-bottom: 2px;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s ease;
  font-size: 14px;
  color: var(--td-text-color-primary);
  user-select: none;

  &:hover {
    background-color: var(--td-bg-color-container-hover);
    color: var(--td-text-color-primary);
  }

  &.active {
    background-color: var(--td-bg-color-secondarycontainer);
    color: var(--td-brand-color);
    font-weight: 500;
  }
}

.nav-icon {
  margin-right: 9px;
  font-size: 16px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: inherit;
}

.nav-label {
  flex: 1;
}

.nav-badge {
  flex-shrink: 0;
  margin-left: 2px;
  padding: 0 6px;
  border-radius: 8px;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);
  font-size: 11px;
  line-height: 16px;
  font-weight: 500;
  text-align: center;
}

.settings-content {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.content-wrapper {
  flex: 1;
  overflow-y: auto;
  padding: 24px 32px;
}

.section {
  margin-bottom: 32px;

  &:last-child {
    margin-bottom: 0;
  }
}

.section-content {
  .section-header {
    margin-bottom: 16px;
  }

  .section-title {
    margin: 0 0 6px 0;
    font-family: var(--app-font-family);
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
  }

  .section-desc {
    margin: 0;
    font-family: var(--app-font-family);
    font-size: 14px;
    color: var(--td-text-color-placeholder);
    line-height: 22px;
  }

  .section-body {
    background: var(--td-bg-color-container);
  }
}

.form-item {
  margin-bottom: 16px;

  &:last-child {
    margin-bottom: 0;
  }
}

.form-label {
  display: block;
  margin-bottom: 8px;
  font-family: var(--app-font-family);
  font-size: 15px;
  font-weight: 500;
  color: var(--td-text-color-primary);

  &.required::after {
    content: '*';
    color: var(--td-error-color);
    margin-left: 4px;
  }
}

.form-tip {
  margin-top: 6px;
  font-size: 12px;
  color: var(--td-text-color-placeholder);
}

.kb-id-field {
  display: flex;
  align-items: center;
  gap: 4px;
  width: 100%;
  max-width: 480px;
  margin-top: 8px;
  padding: 6px 8px 6px 12px;
  background: var(--td-bg-color-secondarycontainer);
  border: 1px solid var(--td-component-stroke);
  border-radius: 6px;

  .kb-id-value {
    flex: 1;
    min-width: 0;
    margin: 0;
    padding: 0;
    background: none;
    border: none;
    font-family: var(--app-font-family-mono);
    font-size: 13px;
    line-height: 1.5;
    color: var(--td-text-color-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .kb-id-copy {
    flex-shrink: 0;
    color: var(--td-text-color-secondary);

    &:hover {
      color: var(--td-brand-color);
    }
  }
}

.granularity-radio-group {
  margin-top: 4px;
}

.granularity-hint {
  margin-top: 8px;
  line-height: 1.6;
  color: var(--td-text-color-secondary);
  white-space: normal;
  word-break: break-word;
}

.indexing-checks {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 12px;
  margin-top: 10px;
}

.indexing-check-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 12px 14px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-container);
  cursor: pointer;
  user-select: none;
  transition: border-color 0.2s ease, background 0.2s ease;

  &:hover {
    border-color: var(--td-brand-color);
  }

  &.is-checked {
    border-color: var(--td-brand-color);
    background: var(--td-brand-color-light);
  }

  &.is-disabled {
    cursor: not-allowed;
    opacity: 0.7;

    &:hover {
      border-color: var(--td-component-stroke);
    }

    &.is-checked:hover {
      border-color: var(--td-brand-color);
    }
  }

  :deep(.t-checkbox__label) {
    font-weight: 500;
    color: var(--td-text-color-primary);
  }
}

.locked-tip {
  color: var(--td-warning-color);
  margin-top: 8px;
}

// 禁用内部 checkbox 自身的点击事件，统一由卡片处理
.indexing-check-box {
  pointer-events: none;
}

.indexing-check-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.indexing-new-badge {
  display: inline-flex;
  align-items: center;
  padding: 0 6px;
  height: 16px;
  border-radius: 3px;
  font-size: 10px;
  font-weight: 600;
  line-height: 1;
  letter-spacing: 0.4px;
  color: var(--td-brand-color);
  background: var(--td-brand-color-light);
}

.indexing-check-desc {
  margin: 0;
  padding-left: 24px;
  font-size: 12px;
  line-height: 18px;
  color: var(--td-text-color-placeholder);
}


.settings-footer {
  padding: 12px 40px;
  border-top: 1px solid var(--td-component-stroke);
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 16px;
  flex-shrink: 0;
}

.settings-footer-note {
  margin: 0;
  margin-right: auto;
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: flex-start;
  gap: 6px;
  font-size: 13px;
  line-height: 20px;
  color: var(--td-text-color-secondary);

  strong {
    margin-right: 4px;
    color: var(--td-text-color-primary);
    font-weight: 500;
  }

  &__icon {
    flex-shrink: 0;
    margin-top: 2px;
    font-size: 14px;
    color: var(--td-success-color);
  }
}

.settings-footer-actions {
  display: flex;
  gap: 12px;
  flex-shrink: 0;
}

// 过渡动画
.modal-enter-active,
.modal-leave-active {
  transition: all 0.3s ease;
}

.modal-enter-from,
.modal-leave-to {
  opacity: 0;

  .settings-modal {
    transform: scale(0.95);
  }
}

// 多模态配置内联样式（与子组件 KBAdvancedSettings 一致）
.kb-multimodal-settings {
  width: 100%;

  .section-header {
    margin-bottom: 20px;

    h2 {
      font-size: 20px;
      font-weight: 600;
      color: var(--td-text-color-primary);
      margin: 0 0 6px 0;
    }

    .section-description {
      font-size: 14px;
      color: var(--td-text-color-secondary);
      margin: 0;
      line-height: 1.5;
    }
  }

  .settings-group {
    display: flex;
    flex-direction: column;
  }

  .setting-row {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    padding: 16px 0;
    border-bottom: 1px solid var(--td-component-stroke);

    &:last-child {
      border-bottom: none;
    }
  }

  .setting-info {
    flex: 1;
    max-width: 65%;
    padding-right: 24px;

    label {
      font-size: 15px;
      font-weight: 500;
      color: var(--td-text-color-primary);
      display: block;
      margin-bottom: 4px;
    }

    .desc {
      font-size: 13px;
      color: var(--td-text-color-secondary);
      margin: 0;
      line-height: 1.5;
    }
  }

  .setting-control {
    flex-shrink: 0;
    min-width: 280px;
    display: flex;
    justify-content: flex-end;
    align-items: center;
  }

  .required {
    color: var(--td-error-color);
    margin-left: 2px;
    font-weight: 500;
  }
}

.mobile-section-select {
  display: none;
}

@media (max-width: 640px) {
  .settings-modal {
    width: calc(100vw - 24px);
    height: calc(100dvh - 24px);
    max-height: none;
  }

  .settings-container {
    flex-direction: column;
  }

  .settings-sidebar {
    width: 100%;
    border-right: 0;
    border-bottom: 1px solid var(--td-component-stroke);
  }

  .sidebar-header {
    padding: 16px 56px 12px 16px;
    border-bottom: 0;
  }

  .close-btn {
    top: 12px;
    right: 12px;
  }

  .settings-nav {
    display: none;
  }

  .mobile-section-select {
    display: block;
    width: calc(100% - 32px);
    margin: 0 16px 12px;
  }

  .content-wrapper {
    padding: 16px;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .settings-footer {
    padding: 12px 16px;
    flex-wrap: wrap;
    gap: 8px;
  }

  .settings-footer-note {
    flex-basis: 100%;
  }

  .settings-footer-actions {
    flex-wrap: wrap;
  }

  .kb-multimodal-settings .setting-row,
  :deep(.setting-row),
  :deep(.kb-parser-settings--embedded .setting-row) {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }

  .kb-multimodal-settings .setting-info,
  :deep(.setting-info),
  :deep(.kb-parser-settings--embedded .setting-info) {
    flex: 1 1 auto;
    width: 100%;
    max-width: 100%;
    padding-right: 0;
  }

  .kb-multimodal-settings .setting-control,
  :deep(.setting-control) {
    min-width: 0;
    width: 100%;
    max-width: 100%;
  }

  :deep(.setting-control .t-select),
  :deep(.setting-control .t-input-number) {
    min-width: 0 !important;
    max-width: 100%;
  }

  :deep(.t-radio-group) {
    max-width: 100%;
    flex-wrap: wrap;
  }
}
</style>
