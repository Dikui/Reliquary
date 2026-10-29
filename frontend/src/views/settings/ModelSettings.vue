<template>
  <div class="model-settings settings-form">
    <div class="section-header">
      <div class="section-header__top">
        <div>
          <p class="section-description">{{ $t('modelSettings.description') }}</p>
        </div>
        <Button
          type="button"
          variant="ghost"
          class="model-test-trigger"
          @click="showDebugDrawer = true"
        >

          {{ $t('modelSettings.actions.debugModel') }}
        </Button>
      </div>

      <div class="builtin-models-hint" role="note">
        <p class="builtin-hint-label">{{ $t('modelSettings.builtinModels.title') }}</p>
        <p class="builtin-hint-text">
          {{ $t('modelSettings.builtinModels.descriptionAdmin') }}
        </p>
        <a class="doc-link" href="http://192.168.1.146:3000/dikui/reliquary/src/branch/main/docs/builtin-models.md" target="_blank"
          rel="noopener noreferrer">
          {{ $t('modelSettings.builtinModels.viewGuide') }}

        </a>
      </div>
    </div>

    <ChatGPTSettings :models="allModels" />

    <h3 class="model-list-title">{{ $t('modelSettings.listTitle') }}</h3>
    <nav class="model-type-tabs" :aria-label="$t('model.editor.typeLabel')">
      <Button v-for="type in (['all', 'chat', 'embedding', 'rerank'] as const)" :key="type" :aria-pressed="activeTypeFilter === type" :variant="activeTypeFilter === type ? 'primary' : 'default'" @click="activeTypeFilter = type">{{ type === 'all' ? $t('common.all') : typeLabel(type) }} ({{ type === 'all' ? allLegacyModels.length : countByType(type) }})</Button>
    </nav>
    <p v-if="loading" role="status">{{ $t('common.loading') }}</p>

    <div :aria-busy="loading" class="model-list-loading">
      <div v-if="!loading" class="model-grid">
        <article v-for="model in filteredModels" :key="`${model._modelType}-${model.id}`" class="model-card" :class="[
          `model-card--${model._modelType}`,
          {
            'model-card--builtin': model.isBuiltin,
          },
        ]">
          <div class="model-card__badge" :aria-label="typeLabel(model._modelType)">{{ typeLabel(model._modelType) }}

          </div>
          <div class="model-card__body">
            <div class="model-card__header">
              <h3 class="model-card__title"><button type="button" @click="editModel(model._modelType, model)">{{ modelDisplayName(model) }}</button></h3>
              <Tag v-if="model.isBuiltin">{{ $t('modelSettings.builtinTag') }}</Tag>
              <div class="model-card__actions" @click.stop>
                <Button v-for="action in getModelOptions(model._modelType, model)" :key="String(action.value)" variant="ghost" @click="handleMenuAction({ value: action.value }, model._modelType, model)">{{ action.content }}</Button>
                <Button v-if="canDeleteModel(model)" variant="danger" @click="confirmDeleteModel(model)">{{ $t('common.delete') }}</Button>
              </div>
            </div>
            <p class="model-card__subtitle">
              <span>{{ vendorLabel(model) }}</span>
              <template v-if="model._modelType === 'embedding' && model.dimension">
                <span class="model-card__sep">·</span>
                <span>{{ $t('model.editor.dimensionLabel') }} {{ model.dimension }}</span>
              </template>
              <template v-if="model._modelType === 'chat'">
                <span class="model-card__sep">·</span>
                <span
                  class="model-card__ctx"
                  :class="{ 'model-card__ctx--default': isDefaultContextWindow(model.contextWindow) }"
                  :title="contextWindowTitle(model.contextWindow)"
                >{{ formatContextWindow(model.contextWindow) }}</span>
              </template>
              <template v-if="(model._modelType === 'chat' && model.supportsVision) || (model._modelType === 'embedding' && model.supportsImages)">
                <span class="model-card__sep">·</span>
                <Tag class="model-card__vision" :title="$t(model._modelType === 'embedding' ? 'model.editor.supportsImagesLabel' : 'model.editor.supportsVisionLabel')"
                  :aria-label="$t(model._modelType === 'embedding' ? 'model.editor.supportsImagesLabel' : 'model.editor.supportsVisionLabel')">
                  {{ $t(model._modelType === 'embedding' ? 'model.editor.supportsImagesLabel' : 'model.editor.supportsVisionLabel') }}</Tag>
              </template>
            </p>
            <a v-if="model.source === 'chatgpt'" class="model-card__usage" href="https://chatgpt.com/settings/usage" target="_blank" rel="noopener noreferrer" @click.stop @keydown.enter.stop>{{ $t('chatgpt.manageUsage') }}</a>
          </div>
        </article>
        <button
          type="button"
          class="model-card model-card--add"
          @click="openAddDialog"
        >
          <span class="model-card--add__icon" aria-hidden="true">
            ＋
          </span>
          <span class="model-card--add__label">{{ $t('modelSettings.actions.addModel') }}</span>
        </button>
      </div>
    </div>

    <Dialog v-model="showUsageDialog" :title="$t('modelSettings.usage.title')">
      <div v-if="usageConflict" class="model-usage-dialog">
        <p class="model-usage-dialog__description">
          {{ $t('modelSettings.usage.description', { name: usageConflictModelName }) }}
        </p>

        <div class="model-usage-dialog__content">
          <section v-if="usageConflict.libraries.length" class="model-usage-group">
            <h3>
              {{ $t('modelSettings.usage.librarySettings') }}
            </h3>
            <ul>
              <li v-for="resource in usageConflict.libraries" :key="resource.id">
                <div class="model-usage-resource">
                  <strong>{{ resource.name || resource.id }}</strong>
                  <div class="model-usage-bindings">
                    <Tag
                      v-for="(binding, index) in resource.bindings"
                      :key="`${binding}-${index}`"
                      variant="plain"
                    >
                      {{ $t(modelUsageBindingI18nKey(binding)) }}
                    </Tag>
                  </div>
                </div>
                <Button
                  variant="ghost"
                  @click="openUsageResource('library', resource.id, resource.bindings)"
                >
                  {{ $t('modelSettings.usage.openConfiguration') }}
                </Button>
              </li>
            </ul>
            <p
              v-if="modelUsageListTruncated(usageConflict.libraries, usageConflict.library_total)"
              class="model-usage-truncated"
            >
              {{ $t('modelSettings.usage.truncated', {
                shown: usageConflict.libraries.length,
                total: modelUsageResourceCount(usageConflict.libraries, usageConflict.library_total)
              }) }}
            </p>
          </section>

        </div>

        <div class="model-usage-dialog__actions">
          <Button @click="showUsageDialog = false">{{ $t('common.close') }}</Button>
        </div>
      </div>
    </Dialog>

    <!-- 模型编辑器抽屉 -->
    <ModelEditorDialog v-model:visible="showDialog" :model-type="currentModelType" :model-data="editingModel"
      @confirm="handleModelSave" />
    <ModelDebugDrawer v-model:visible="showDebugDrawer" :models="allModels" />

  </div>
</template>

<script setup lang="ts">
import { Button, Tag, Dialog, toast, useConfirm } from '@/components/ui'
import '@/styles/settings-form.css'
import { ref, computed, nextTick, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import ModelEditorDialog from '@/components/ModelEditorDialog.vue'
import ModelDebugDrawer from '@/components/ModelDebugDrawer.vue'
import ChatGPTSettings from './ChatGPTSettings.vue'
import {
  listModels,
  createModel,
  updateModel as updateModelAPI,
  deleteModel as deleteModelAPI,
  ModelInUseError,
  modelUsageBindingI18nKey,
  modelUsageLibrarySection,
  modelUsageListTruncated,
  modelUsageResourceCount,
  modelUsageResourceRoute,
  type ModelConfig,
  type ModelUsageDetails,
  type ModelUsageResourceKind,
} from '@/api/model'
import { useUIStore } from '@/stores/ui'
import { useResourcesStore } from '@/stores/resources'
import {
  formatContextWindow,
  isDefaultContextWindow,
  effectiveContextWindow,
} from '@/utils/contextWindow'

const confirm = useConfirm()
const { t, te } = useI18n()
const uiStore = useUIStore()
const resources = useResourcesStore()
const router = useRouter()
type ModelType = 'chat' | 'embedding' | 'rerank'
type FilterType = 'all' | ModelType

function confirmDeleteModel(model: any) {
  const dialog = confirm({ header: t('common.delete'), body: t('modelSettings.confirmDelete', { name: modelDisplayName(model) }), confirmBtn: t('common.delete'), theme: 'danger', onConfirm: async () => { await deleteModel(model._modelType, model.id); dialog.destroy() } })
}
const showDialog = ref(false)
const showDebugDrawer = ref(false)
const showUsageDialog = ref(false)
const usageConflict = ref<ModelUsageDetails | null>(null)
const usageConflictModelName = ref('')
const currentModelType = ref<ModelType>('chat')
const editingModel = ref<any>(null)
const loading = ref(true)
const props = defineProps<{ initialSection?: string }>()
const activeTypeFilter = ref<FilterType>('all')

const MODEL_TAB_TYPES: FilterType[] = ['chat', 'embedding', 'rerank']
const LIBRARY_EDITOR_HOST_ROUTES = new Set([
  'home',
  'libraryList',
  'libraryDetail',
])
watch(
  () => props.initialSection || uiStore.settingsInitialSubSection,
  (sub) => {
    if (sub && MODEL_TAB_TYPES.includes(sub as FilterType)) {
      activeTypeFilter.value = sub as FilterType
    }
  },
  { immediate: true },
)

// 模型列表数据
const allModels = ref<ModelConfig[]>([])

// 后端 type → 前端分组 type 的映射
const backendTypeToModelType: Record<string, ModelType> = {
  KnowledgeQA: 'chat',
  Embedding: 'embedding',
  Rerank: 'rerank',
}

// 将后端模型格式转换为旧的前端格式（附带 _modelType 便于渲染）
// apiKey is always blank here: the server's main GET response does not
// include it (see internal/handler/dto/model.go — ModelParametersDTO omits
// secret fields). Credential read/write happens inside the editor dialog
// via the dedicated /credentials subresource.
function convertToLegacyFormat(model: ModelConfig) {
  return {
    id: model.id!,
    name: model.name,
    displayName: model.display_name || model.parameters.extra_config?.chatgpt_display_name || '',
    chatgptAccountID: model.parameters.extra_config?.chatgpt_account_id || '',
    source: model.source,
    modelName: model.name,
    baseUrl: model.parameters.base_url || '',
    apiKey: '',
    provider: model.parameters.provider || '',
    dimension: model.parameters.embedding_parameters?.dimension,
    supportsDimensionOverride: model.parameters.embedding_parameters?.supports_dimension_override || false,
    supportsImages: model.parameters.embedding_parameters?.supports_images || false,
    multimodalFormat: model.parameters.embedding_parameters?.multimodal_format || 'jina',
    isBuiltin: model.is_builtin || false,
    supportsVision: model.parameters.supports_vision || false,
    contextWindow: model.parameters.context_window || undefined,
    maxConcurrency: model.parameters.max_concurrency,
    pricing: model.parameters.pricing ?? null,
    customHeaders: model.parameters.custom_headers
      ? Object.entries(model.parameters.custom_headers).map(([key, value]) => ({ key, value: String(value) }))
      : [],
    // 原始存库值，编辑弹窗内再 resolve（避免打开时被推断值覆盖）
    thinkingControl: model.parameters.extra_config?.thinking_control,
    _modelType: backendTypeToModelType[model.type] || 'chat' as ModelType,
    // Preserve the credential metadata map so the editor dialog can render
    // the "Configured" state without an extra round-trip.
    credentials: model.credentials,
  }
}

// 平铺 + 过滤
const allLegacyModels = computed(() => allModels.value.map(convertToLegacyFormat))
const filteredModels = computed(() => {
  if (activeTypeFilter.value === 'all') return allLegacyModels.value
  return allLegacyModels.value.filter(m => m._modelType === activeTypeFilter.value)
})

const countByType = (type: ModelType) => allLegacyModels.value.filter(m => m._modelType === type).length

const typeLabel = (type: ModelType) => {
  const map: Record<ModelType, string> = {
    chat: t('modelSettings.typeShort.chat'),
    embedding: t('modelSettings.typeShort.embedding'),
    rerank: t('modelSettings.typeShort.rerank'),
  }
  return map[type]
}

const sourceLabel = (type: ModelType) => {
  return t('modelSettings.source.remote')
}

// Maps a backend `provider` id (e.g. "openai", "aliyun")
// to its localized short label. Reuses the same i18n keys the editor's
// provider dropdown uses, so the model card and the editor stay in sync
// when a provider is renamed. Falls back to '' when the backend didn't
// store a provider — caller falls back to sourceLabel().
const providerLabel = (model: any): string => {
  const id = model.provider
  if (!id) return ''
  const key = `model.editor.providers.${id}.label`
  // Unknown preset ids use the same generic display name as the editor.
  return te(key) ? t(key) : t('model.editor.providers.generic.label')
}

// What the vendor chip on a card shows. Keeps the chip text uniformly
// short so cards line up:
//   local  → "Ollama"
//   remote → provider's localized short name (e.g. "OpenAI",
//            "阿里云百炼"). For the catch-all "generic" provider
//            we render a single short word ("自定义" / "Custom") — the
//            editor dropdown's longer "自定义 (OpenAI兼容接口)" label
//            blows out the card chip row, and the "OpenAI 兼容" framing
//            isn't meaningful to most end users (they didn't pick "I
//            want OpenAI compatibility", they just pasted a base URL).
const vendorLabel = (model: any): string => {
  if (model.source === 'local') return 'Ollama'
  if (model.source === 'chatgpt') return t('chatgpt.usingPlan')
  if (model.provider === 'generic') {
    return t('modelSettings.source.custom')
  }
  return providerLabel(model) || sourceLabel(model._modelType)
}

const modelDisplayName = (model: any) => {
  const displayName = typeof model.displayName === 'string' ? model.displayName.trim() : ''
  return displayName || model.name
}

const contextWindowTitle = (tokens?: number) => {
  if (isDefaultContextWindow(tokens)) {
    return t('model.editor.contextWindowDefaultHint', { value: formatContextWindow(tokens) })
  }
  return t('model.editor.contextWindowTokens', { count: effectiveContextWindow(tokens) })
}

const emptyHint = computed(() => {
  if (activeTypeFilter.value === 'all') return t('modelSettings.chat.empty')
  const map: Record<ModelType, string> = {
    chat: t('modelSettings.chat.empty'),
    embedding: t('modelSettings.embedding.empty'),
    rerank: t('modelSettings.rerank.empty'),
  }
  return map[activeTypeFilter.value as ModelType]
})

// 加载模型列表
const loadModels = async () => {
  loading.value = true
  try {
    const models = await listModels()
    allModels.value = models
    // 加载后立即更新空间级模型缓存，让库编辑器和模型选择器读取最新配置。
    resources.replaceModels(models)
  } catch (error: any) {
    console.error('加载模型列表失败:', error)
    toast.error(error.message)
  } finally {
    loading.value = false
  }
}

// 打开添加对话框；类型在抽屉内选择，此处仅按当前 Tab 预填默认值
const openAddDialog = () => {
  currentModelType.value = activeTypeFilter.value === 'all' ? 'chat' : activeTypeFilter.value
  editingModel.value = null
  showDialog.value = true
}

// Built-in lifecycle remains deployment-managed (YAML / SQL). The UI only
const canDeleteModel = (model: any) =>
  !model.isBuiltin

// 编辑模型
const editModel = (type: ModelType, model: any) => {
  currentModelType.value = type
  editingModel.value = { ...model }
  showDialog.value = true
}

// 保存模型
const handleModelSave = async (modelData: any) => {
  const saveType: ModelType = modelData.modelType ?? currentModelType.value
  currentModelType.value = saveType

  try {
    if (!modelData.modelName || !modelData.modelName.trim()) {
      toast.warning(t('modelSettings.toasts.nameRequired'))
      return
    }

    if (modelData.modelName.trim().length > 100) {
      toast.warning(t('modelSettings.toasts.nameTooLong'))
      return
    }

    if (modelData.displayName && modelData.displayName.trim().length > 100) {
      toast.warning(t('modelSettings.toasts.displayNameTooLong'))
      return
    }

    if (modelData.source === 'remote') {
      if (!modelData.baseUrl || !modelData.baseUrl.trim()) {
        toast.warning(t('modelSettings.toasts.baseUrlRequired'))
        return
      }

      try {
        new URL(modelData.baseUrl.trim())
      } catch {
        toast.warning(t('modelSettings.toasts.baseUrlInvalid'))
        return
      }
    }

    if (saveType === 'embedding') {
      if (!modelData.dimension || modelData.dimension < 128 || modelData.dimension > 4096) {
        toast.warning(t('modelSettings.toasts.dimensionInvalid'))
        return
      }
    }

    const customHeadersMap: Record<string, string> = {}
    if (Array.isArray(modelData.customHeaders)) {
      for (const item of modelData.customHeaders) {
        const key = (item?.key ?? '').trim()
        const value = (item?.value ?? '').trim()
        if (key && value) {
          customHeadersMap[key] = value
        }
      }
    }

    // api_key flows in only on initial create (modelData.apiKey is wiped on
    // every edit-mode open). Edits to existing models commit credentials via
    // the /credentials subresource (handled inside ModelEditorDialog).
    const trimmedApiKey = (modelData.apiKey ?? '').trim()
    const apiKeyFields: { api_key?: string } =
      !editingModel.value && trimmedApiKey ? { api_key: trimmedApiKey } : {}
    const extraConfig: Record<string, string> = {}
    if (
      saveType === 'chat'
      && modelData.source === 'remote'
      && modelData.thinkingControl
    ) {
      extraConfig.thinking_control = modelData.thinkingControl
    }
    if (modelData.source === 'chatgpt') {
      if (saveType !== 'chat' || !modelData.chatgptAccountID) {
        toast.warning(t('chatgpt.accountRequired'))
        return
      }
      extraConfig.chatgpt_account_id = modelData.chatgptAccountID
      extraConfig.chatgpt_display_name = modelData.displayName || modelData.modelName
    }
    const extraConfigFields = Object.keys(extraConfig).length > 0
      ? { extra_config: extraConfig }
      : {}

    const apiModelData: ModelConfig = {
      name: modelData.modelName.trim(),
      display_name: modelData.displayName?.trim() || '',
      type: getModelType(saveType),
      source: modelData.source,
      description: '',
      parameters: {
        ...(Object.hasOwn(modelData, 'pricing') ? { pricing: modelData.pricing } : {}),
        ...(modelData.source === 'chatgpt' ? {} : {
          base_url: modelData.baseUrl?.trim() || '',
          ...apiKeyFields,
          provider: modelData.provider || '',
        }),
        ...extraConfigFields,
        ...(modelData.source !== 'chatgpt' && Object.keys(customHeadersMap).length > 0 ? { custom_headers: customHeadersMap } : {}),
        ...(saveType === 'embedding' && modelData.dimension ? {
          embedding_parameters: {
            dimension: modelData.dimension,
            truncate_prompt_tokens: 0,
            supports_dimension_override: modelData.supportsDimensionOverride ?? false,
            supports_images: modelData.source === 'remote' && !!modelData.supportsImages,
            multimodal_format: modelData.source === 'remote' && modelData.supportsImages ? (modelData.multimodalFormat || 'jina') : '',
          }
        } : {}),
        ...(saveType === 'chat' ? {
          supports_vision: modelData.supportsVision ?? false
        } : {}),
        ...((saveType === 'chat')
          && Number(modelData.contextWindow) >= 1024
          ? { context_window: Math.round(Number(modelData.contextWindow)) }
          : {}),
        // 后台并发上限：仅 chat/embedding 受治理，>0 才写入（0/空沿用全局默认）。
        ...(['chat', 'embedding'].includes(saveType)
          && Number(modelData.maxConcurrency) > 0
          ? { max_concurrency: Number(modelData.maxConcurrency) }
          : {})
      }
    }

    if (editingModel.value && editingModel.value.id) {
      await updateModelAPI(editingModel.value.id, apiModelData)
      toast.success(t('modelSettings.toasts.updated'))
    } else {
      await createModel(apiModelData)
      toast.success(t('modelSettings.toasts.added'))
    }

    showDialog.value = false
    await loadModels()
  } catch (error: any) {
    console.error('保存模型失败:', error)
    toast.error(error.message || t('modelSettings.toasts.saveFailed'))
  }
}

// 删除模型
const deleteModel = async (_type: ModelType, modelId: string) => {
  const model = allModels.value.find(m => m.id === modelId)
  if (model?.is_builtin) {
    toast.warning(t('modelSettings.toasts.builtinCannotDelete'))
    return
  }

  try {
    await deleteModelAPI(modelId)
    toast.success(t('modelSettings.toasts.deleted'))
    await loadModels()
  } catch (error: any) {
    if (error instanceof ModelInUseError) {
      usageConflict.value = error.details
      usageConflictModelName.value = model?.display_name || model?.name || modelId
      showUsageDialog.value = true
      return
    }
    console.error('删除模型失败:', error)
    toast.error(error.message || t('modelSettings.toasts.deleteFailed'))
  }
}

const openUsageResource = async (
  kind: ModelUsageResourceKind,
  id: string,
  bindings: readonly string[] = [],
) => {
  showUsageDialog.value = false
  const librarySection = modelUsageLibrarySection(bindings)
  uiStore.openSettings('entry', librarySection)
}

// 获取模型操作菜单选项
const getModelOptions = (type: ModelType, model: any) => {
  const options: any[] = []

  if (model.isBuiltin) {
    options.push({
        content: t('common.edit'),
        value: `edit-${type}-${model.id}`
    })
    return options
  }

  options.push({
    content: t('common.edit'),
    value: `edit-${type}-${model.id}`
  })

  options.push({
    content: t('common.copy'),
    value: `copy-${type}-${model.id}`
  })

  return options
}

// 处理菜单操作
const handleMenuAction = (data: { value: string }, type: ModelType, model: any) => {
  const value = data.value

  if (value.indexOf('edit-') === 0) {
    editModel(type, model)
  } else if (value.indexOf('copy-') === 0) {
    copyModel(type, model.id)
  }
}

// 生成不重复的复制名称
const generateCopyName = (originalName: string): string => {
  const suffix = t('modelSettings.copySuffix')
  const existingNames = new Set(allModels.value.map(m => m.name))
  let candidate = `${originalName}${suffix}`
  let counter = 2
  while (existingNames.has(candidate)) {
    candidate = `${originalName}${suffix} ${counter}`
    counter += 1
  }
  return candidate
}

// 复制模型
const copyModel = async (_type: ModelType, modelId: string) => {
  const source = allModels.value.find(m => m.id === modelId)
  if (!source) {
    return
  }
  if (source.is_builtin) {
    toast.warning(t('modelSettings.toasts.builtinCannotCopy'))
    return
  }

  try {
    const newModel: ModelConfig = {
      name: generateCopyName(source.name),
      display_name: source.display_name || '',
      type: source.type,
      source: source.source,
      description: source.description || '',
      parameters: JSON.parse(JSON.stringify(source.parameters || {}))
    }

    await createModel(newModel)
    toast.success(t('modelSettings.toasts.copied'))
    await loadModels()
  } catch (error: any) {
    console.error('复制模型失败:', error)
    toast.error(error.message || t('modelSettings.toasts.copyFailed'))
  }
}

// 获取后端模型类型
function getModelType(type: ModelType): 'KnowledgeQA' | 'Embedding' | 'Rerank' {
  const typeMap = {
    chat: 'KnowledgeQA' as const,
    embedding: 'Embedding' as const,
    rerank: 'Rerank' as const,
  }
  return typeMap[type]
}

onMounted(() => {
  loadModels()
})
</script>

<style scoped>
 .section-header__top, .model-card__header, .model-card__actions, .model-type-tabs { display: flex; gap: var(--space-2); flex-wrap: wrap; align-items: center; justify-content: space-between; }
.builtin-models-hint { padding: var(--space-3) 0; border-top: 2px solid var(--rule); color: var(--muted); font-size: var(--type-note-size); }
.builtin-hint-label { color: var(--text); font-weight: 800; }
.model-type-tabs { justify-content: flex-start; margin: var(--space-4) 0; }
.model-list-title { margin: var(--space-6) 0 0; padding-top: var(--space-4); border-top: 2px solid var(--rule); }
.model-grid { display: grid; }
.model-card { display: flex; gap: var(--space-3); min-width: 0; padding: var(--space-4) 0; border: 0; border-top: 2px solid var(--rule); background: var(--bg); color: var(--text); text-align: left; }
.model-card:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.model-card__badge { min-width: 64px; font-size: var(--text-xs); color: var(--muted); }
.model-card__body { flex: 1; min-width: 0; }
.model-card__title { overflow-wrap: anywhere; margin: 0; font-size: var(--text-md); font-weight: 600; }
.model-card__title button { padding: 0; border: 0; background: transparent; color: inherit; font: inherit; font-weight: inherit; text-align: left; text-decoration: underline; cursor: pointer; }
.model-card__title button:hover { color: var(--accent); }
.model-card__title button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.model-card__subtitle { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); font-size: var(--type-note-size); color: var(--muted); }
.model-card--add { cursor: pointer; font: inherit; }
.model-usage-group ul { padding: 0; list-style: none; }
.model-usage-group li, .model-usage-bindings { display: flex; gap: var(--space-2); flex-wrap: wrap; justify-content: space-between; margin-bottom: var(--space-3); }
</style>
