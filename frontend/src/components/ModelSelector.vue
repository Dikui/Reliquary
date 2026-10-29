<template>
  <div class="model-selector settings-form">
    <Combobox :clearable="clearable" :model-value="selectedModelId" :options="options" :aria-label="ariaLabel || placeholderText" :placeholder="placeholderText" :disabled="disabled || loading" :aria-invalid="status === 'error' || undefined" @update:model-value="handleModelChange" />
    <p v-if="selectedModel && showContextWindow" class="model-context" :title="contextWindowTitle(selectedModel.parameters?.context_window)">{{ formatContextWindow(selectedModel.parameters?.context_window) }}</p>
    <p v-if="selectedModel?.source === 'chatgpt'" class="model-plan-usage">{{ $t('chatgpt.usingPlan') }} · <a href="https://chatgpt.com/settings/usage" target="_blank" rel="noopener noreferrer">{{ $t('chatgpt.manageUsage') }}</a></p>
  </div>
</template>

<script setup lang="ts">
import { Combobox, toast } from '@/components/ui'
import '@/styles/settings-form.css'
import { ref, computed, watch, onMounted } from 'vue'
import { type ModelConfig } from '@/api/model'
import { useResourcesStore } from '@/stores/resources'
import { useI18n } from 'vue-i18n'
import { filterModelsByType } from './modelSelectorFilter'
import {
  formatContextWindow,
  isDefaultContextWindow,
  effectiveContextWindow,
  modelHasContextWindow,
} from '@/utils/contextWindow'

interface Props {
  ariaLabel?: string
  modelType: 'KnowledgeQA' | 'Embedding' | 'Rerank'
  selectedModelId?: string
  disabled?: boolean
  placeholder?: string
  status?: 'default' | 'success' | 'warning' | 'error'
  clearable?: boolean
  // 可选：外部传入的所有模型列表，如果提供则不调用API
  allModels?: ModelConfig[]
}

const props = withDefaults(defineProps<Props>(), {
  disabled: false,
  placeholder: '',
  status: 'default',
  clearable: false,
})

const emit = defineEmits<{
  'update:selectedModelId': [value: string]
  'add-model': []
}>()

const models = ref<ModelConfig[]>([])
const loading = ref(false)
const { t } = useI18n()
const resources = useResourcesStore()

const placeholderText = computed(() => {
  return props.placeholder || t('model.selectModelPlaceholder')
})

const modelDisplayName = (model: ModelConfig) => {
  const displayName = model.display_name?.trim() || model.parameters.extra_config?.chatgpt_display_name?.trim()
  return displayName || model.name
}

const showContextWindow = computed(() => modelHasContextWindow(props.modelType))

const contextWindowTitle = (tokens?: number) => {
  if (isDefaultContextWindow(tokens)) {
    return t('model.editor.contextWindowDefaultHint', { value: formatContextWindow(tokens) })
  }
  return t('model.editor.contextWindowTokens', { count: effectiveContextWindow(tokens) })
}

// 外部传入 allModels 时跟着 prop 走；否则用空间级缓存，设置页改完窗口立刻能看见。
watch(
  () => [props.allModels, props.modelType, resources.allModels] as const,
  ([newModels]) => {
    const source = Array.isArray(newModels) ? newModels : resources.allModels
    models.value = filterModelsByType(source, props.modelType)
  },
  { immediate: true },
)

const options = computed(() => [
  ...models.value.map(model => ({ value: model.id!, label: [modelDisplayName(model), model.display_name ? model.name : '', model.is_builtin ? t('model.builtinTag') : '', model.is_default ? t('model.defaultTag') : '', model.source === 'chatgpt' ? t('chatgpt.usingPlan') : '', showContextWindow.value ? formatContextWindow(model.parameters?.context_window) : ''].filter(Boolean).join(' · ') })),
  ...(!props.disabled ? [{ value: '__add_model__', label: t('model.addModelInSettings') }] : []),
])
const selectedModel = computed(() => {
  if (!props.selectedModelId) return null
  return models.value.find(m => m.id === props.selectedModelId)
})

// 加载模型列表（仅在未提供 allModels 时调用）
const loadModels = async () => {
  if (props.allModels) {
    return
  }

  loading.value = true
  try {
    await resources.ensureModels()
    models.value = filterModelsByType(resources.allModels, props.modelType)
  } catch (error) {
    console.error(t('model.loadFailed'), error)
    toast.error(t('model.loadFailed'))
    models.value = []
  } finally {
    loading.value = false
  }
}

// 处理模型选择变化
const handleModelChange = (value?: string) => {
  // 如果选择的是添加模型选项，触发添加事件而不更新选中值
  if (value === '__add_model__') {
    emit('add-model')
    return
  }
  emit('update:selectedModelId', value || '')
}

// 暴露刷新方法给父组件
defineExpose({
  refresh: loadModels
})

onMounted(() => {
  // 只有在没有提供 allModels 时才加载
  if (!props.allModels) {
    loadModels()
  }
})
</script>

<style scoped>
 .model-selector { width: 100%; min-width: 0; }
.model-context, .model-plan-usage { color: var(--muted); font-size: var(--text-sm); overflow-wrap: anywhere; }

</style>
