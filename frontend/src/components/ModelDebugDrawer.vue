<template>
  <Drawer v-model="drawerVisible" :title="$t('modelSettings.debug.title')" :description="$t('modelSettings.debug.description')" content-class="model-debug-drawer">

    <div class="model-debug settings-form">
      <section class="setting-drawer__section">
        <h4 class="setting-drawer__section-title">{{ $t('modelSettings.debug.groupModel') }}</h4>
        <div v-if="availableModelTypes.length > 1" class="form-item">
          <div class="model-type-options" role="radiogroup" :aria-label="$t('modelSettings.debug.modelType')">
            <button
              v-for="option in availableModelTypes"
              :key="option.value"
              type="button"
              class="model-type-option"
              :class="{ 'is-active': selectedModelType === option.value }"
              role="radio"
              :aria-checked="selectedModelType === option.value"
              @click="selectModelType(option.value)"
            >

              <span class="model-type-option__label">{{ option.label }}</span>
            </button>
          </div>
        </div>
        <div class="form-item">
          <label class="form-label" :for="`${formId}-field-1`">{{ $t('modelSettings.debug.model') }}</label>
          <Select
            v-model="selectedModelId"
            :placeholder="$t('modelSettings.debug.modelPlaceholder')"
            :disabled="filteredModels.length === 0"
            @update:model-value="resetResult"
           :id="`${formId}-field-1`">
            <option
              v-for="model in filteredModels"
              :key="model.id"
              :value="model.id!"
            >{{ modelLabel(model) }}</option>
          </Select>
          <p v-if="filteredModels.length === 0" class="form-desc">
            {{ $t('modelSettings.debug.noModelsForType') }}
          </p>
        </div>
      </section>

      <template v-if="selectedModel">
        <section class="setting-drawer__section">
          <h4 class="setting-drawer__section-title">{{ $t('modelSettings.debug.groupInput') }}</h4>
          <div class="form-item">
            <label class="form-label" :for="`${formId}-field-2`">{{ inputLabel }}</label>
            <Textarea
              v-model="input"
              :placeholder="inputPlaceholder"
              rows="5"
            :id="`${formId}-field-2`" />
          </div>

          <div v-if="selectedModel.type === 'Rerank'" class="form-item">
            <label class="form-label" :for="`${formId}-field-3`">{{ $t('modelSettings.debug.documents') }}</label>
            <Textarea
              v-model="documentsText"
              :placeholder="$t('modelSettings.debug.documentsPlaceholder')"
              rows="5"
            :id="`${formId}-field-3`" />
            <p class="form-desc">{{ $t('modelSettings.debug.documentsHint') }}</p>
          </div>

          <div v-if="needsFile" class="form-item">
            <label class="form-label" :for="`${formId}-field-4`">{{ fileLabel }}</label>
            <div class="file-picker">
              <input
                ref="fileInputRef"
                class="file-picker__input"
                type="file"
                accept="image/*"
                @change="onNativeFileChange"
              >
              <Button variant="default" @click="fileInputRef?.click()">

                {{ $t('modelSettings.debug.chooseFile') }}
              </Button>
            </div>
            <p v-if="file" class="form-desc">{{ file.name }} · {{ formatBytes(file.size) }}</p>
          </div>
        </section>

        <section v-if="isChat" class="setting-drawer__section">
          <h4 class="setting-drawer__section-title">{{ $t('modelSettings.debug.parameters') }}</h4>
          <div class="parameter-grid">
            <div class="form-item">
              <label class="form-label">Temperature</label>
              <Input type="number" :model-value="temperature" @update:model-value="temperature = Number($event)" :min="0" :max="2" :step="0.1" :id="`${formId}-field-4`" />
            </div>
            <div class="form-item">
              <label class="form-label" :for="`${formId}-field-5`">Top P</label>
              <Input type="number" :model-value="topP" @update:model-value="topP = Number($event)" :min="0.01" :max="1" :step="0.1" :id="`${formId}-field-5`" />
            </div>
            <div class="form-item">
              <label class="form-label" :for="`${formId}-field-6`">Max Tokens</label>
              <Input type="number" :model-value="maxTokens" @update:model-value="maxTokens = Number($event)" :min="1" :max="8192" :step="128" :id="`${formId}-field-6`" />
            </div>
          </div>
          <div class="form-item">
            <label class="form-label" :for="`${formId}-field-7`">{{ $t('modelSettings.debug.systemPrompt') }}</label>
            <Textarea
              v-model="systemPrompt"
              :placeholder="$t('modelSettings.debug.systemPromptPlaceholder')"
              rows="3"
            :id="`${formId}-field-7`" />
          </div>
          <div v-if="supportsThinking" class="form-item">
            <label class="form-label" :for="`${formId}-field-8`">{{ $t('modelSettings.debug.thinking') }}</label>
            <div class="switch-field">
              <Switch v-model="thinking" :id="`${formId}-field-8`" />
              <span class="form-desc form-desc--inline">{{ $t('modelSettings.debug.thinkingDesc') }}</span>
            </div>
          </div>
        </section>

        <section v-if="result || history.length > 0" class="setting-drawer__section">
          <h4 class="setting-drawer__section-title">{{ $t('modelSettings.debug.groupResult') }}</h4>

          <div v-if="history.length > 1" class="form-item">
            <label class="form-label">{{ $t('modelSettings.debug.history') }}</label>
            <div class="history-list">
              <button
                v-for="run in history"
                :key="run.id"
                type="button"
                class="history-item"
                :class="{ 'history-item--active': result === run.result }"
                @click="result = run.result"
              >
                <span class="history-item__label">{{ run.label }}</span>
                <span class="history-item__meta">{{ run.result.elapsed_ms }} ms</span>
              </button>
            </div>
          </div>

          <div v-if="result" class="debug-result">
            <div class="result-banner" :class="result.ok ? 'result-banner--ok' : 'result-banner--error'">

              <div class="result-banner__text">
                <strong>{{ result.ok ? $t('modelSettings.debug.success') : $t('modelSettings.debug.failed') }}</strong>
                <span>{{ result.elapsed_ms }} ms</span>
              </div>
            </div>

            <div v-if="resultMetrics.length > 0" class="metric-list">
              <span v-for="metric in resultMetrics" :key="metric.key" class="metric-chip">
                {{ metric.label }}: {{ metric.value }}
              </span>
            </div>

            <p v-if="result.error" class="result-error">{{ result.error }}</p>

            <div class="result-tabs"><Button v-for="tab in (['response', 'request'] as const)" :key="tab" :aria-pressed="resultTab === tab" :variant="resultTab === tab ? 'primary' : 'default'" @click="resultTab = tab">{{ $t(tab === 'response' ? 'modelSettings.debug.rawResponse' : 'modelSettings.debug.requestPreview') }}</Button></div>
            <pre class="json-output">{{ formattedResult }}</pre>
          </div>
        </section>
      </template>
    </div>
  <template #footer><div class="settings-actions"><Button v-if="result" @click="copyResult">{{ $t('modelSettings.debug.copyResult') }}</Button><Button @click="drawerVisible = false">{{ $t('common.close') }}</Button><Button variant="primary" :loading="running" :disabled="!canRun" @click="runDebug">{{ $t('modelSettings.debug.run') }}</Button></div></template></Drawer>
</template>

<script setup lang="ts">
import { Button, Input, Select, Switch, Textarea, Drawer, toast } from '@/components/ui'
import '@/styles/settings-form.css'
import { computed, ref, watch, onBeforeUnmount, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { copyToClipboard } from '@/utils/clipboard'
import { debugModel, type ModelConfig, type ModelDebugResult } from '@/api/model'
import { fileSizeVerification, MAX_FILE_SIZE_MB } from '@/utils'
import { modelSupportsThinking } from '@/utils/thinkingControl'
import {
  formatContextWindow,
  modelHasContextWindow,
} from '@/utils/contextWindow'

const props = defineProps<{
  visible: boolean
  models: ModelConfig[]
}>()

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
}>()

const { t, te } = useI18n()
const formId = useId()
const drawerVisible = computed({
  get: () => props.visible,
  set: value => emit('update:visible', value),
})

type DebugModelType = ModelConfig['type']

const selectedModelType = ref<DebugModelType>('KnowledgeQA')
const selectedModelId = ref('')
const input = ref('')
const documentsText = ref('')
const file = ref<File | null>(null)
const fileInputRef = ref<HTMLInputElement | null>(null)
const thinking = ref(false)
const temperature = ref(0.7)
const topP = ref(1)
const maxTokens = ref(1024)
const systemPrompt = ref('')
const running = ref(false)
const result = ref<ModelDebugResult | null>(null)
const resultTab = ref<'response' | 'request'>('response')
const history = ref<Array<{
  id: number
  label: string
  result: ModelDebugResult
}>>([])
let runSequence = 0

const selectedModel = computed(() => props.models.find(model => model.id === selectedModelId.value))
const filteredModels = computed(() => props.models.filter(model => model.type === selectedModelType.value))
const isChat = computed(() => selectedModel.value?.type === 'KnowledgeQA')
const supportsThinking = computed(() => selectedModel.value ? modelSupportsThinking(selectedModel.value) : false)
const needsFile = computed(() => selectedModel.value?.type === 'KnowledgeQA' && selectedModel.value?.parameters.supports_vision === true)
const documents = computed(() => documentsText.value.split('\n').map(item => item.trim()).filter(Boolean))
const canRun = computed(() => {
  if (!selectedModel.value) return false
  if (selectedModel.value.type === 'Rerank') return !!input.value.trim() && documents.value.length > 0
  return !!input.value.trim()
})

const allModelTypeOptions = computed(() => {
  const keys: Record<DebugModelType, { short: string; icon: string }> = {
    KnowledgeQA: { short: 'chat', icon: 'chat' },
    Embedding: { short: 'embedding', icon: 'chart-bubble' },
    Rerank: { short: 'rerank', icon: 'filter-sort' },
  }
  return (Object.keys(keys) as DebugModelType[]).map(value => ({
    value,
    label: t(`modelSettings.typeShort.${keys[value].short}`),
    icon: keys[value].icon,
  }))
})

const modelCount = (type: DebugModelType) => props.models.filter(model => model.type === type).length

const availableModelTypes = computed(() =>
  allModelTypeOptions.value.filter(option => modelCount(option.value) > 0),
)

const modelLabel = (model: ModelConfig) => model.display_name?.trim() || model.name

const vendorLabel = (model: ModelConfig) => {
  const provider = model.parameters.provider || ''
  if (model.source === 'local') return 'Ollama'
  if (provider === 'generic') return t('modelSettings.source.custom')
  const key = `model.editor.providers.${provider}.label`
  return te(key) ? t(key) : provider || model.source
}

const inputLabel = computed(() => {
  if (selectedModel.value?.type === 'Embedding') return t('modelSettings.debug.embeddingInput')
  if (selectedModel.value?.type === 'Rerank') return t('modelSettings.debug.query')
  return t('modelSettings.debug.query')
})

const inputPlaceholder = computed(() => {
  if (selectedModel.value?.type === 'Embedding') return t('modelSettings.debug.embeddingPlaceholder')
  return t('modelSettings.debug.queryPlaceholder')
})

const fileLabel = computed(() => t('modelSettings.debug.imageFile'))

const formattedResult = computed(() => {
  if (!result.value) return ''
  const value = resultTab.value === 'response'
    ? result.value.raw_response
    : result.value.request
  return JSON.stringify(value, null, 2)
})

const OBSERVATION_LABELS: Record<string, string> = {
  dimension: 'modelSettings.debug.metrics.dimension',
  result_count: 'modelSettings.debug.metrics.resultCount',
  answer_characters: 'modelSettings.debug.metrics.answerChars',
  reasoning_characters: 'modelSettings.debug.metrics.reasoningChars',
  reasoning_returned: 'modelSettings.debug.metrics.reasoningReturned',
}

const resultMetrics = computed(() => {
  if (!result.value?.observations) return []
  const obs = result.value.observations
  const keys = Object.keys(OBSERVATION_LABELS).filter(key => obs[key] !== undefined && obs[key] !== null)
  return keys.map(key => ({
    key,
    label: t(OBSERVATION_LABELS[key]),
    value: formatMetricValue(key, obs[key]),
  }))
})

const formatMetricValue = (key: string, value: unknown) => {
  if (typeof value === 'boolean') {
    return value ? t('common.yes') : t('common.no')
  }
  return String(value)
}

const ensureDefaultSelection = () => {
  const types = availableModelTypes.value
  if (types.length === 0) {
    selectedModelId.value = ''
    return
  }
  if (!types.some(option => option.value === selectedModelType.value)) {
    selectedModelType.value = types[0].value
  }
  const models = filteredModels.value
  if (!models.some(model => model.id === selectedModelId.value)) {
    selectedModelId.value = models[0]?.id || ''
  }
}

watch(() => props.visible, visible => {
  if (visible) ensureDefaultSelection()
})

watch(availableModelTypes, () => {
  if (props.visible) ensureDefaultSelection()
})

watch(() => selectedModel.value?.id, () => {
  if (!supportsThinking.value) thinking.value = false
})

watch(() => selectedModel.value?.type, () => {
  file.value = null
  result.value = null
  history.value = []
  resultTab.value = 'response'
})

const resetResult = () => {
  result.value = null
  history.value = []
  resultTab.value = 'response'
}

const selectModelType = (type: DebugModelType) => {
  if (selectedModelType.value === type) return
  selectedModelType.value = type
  selectedModelId.value = filteredModels.value[0]?.id || ''
  input.value = ''
  documentsText.value = ''
  file.value = null
  resetResult()
}

const onNativeFileChange = (event: Event) => {
  const target = event.target as HTMLInputElement
  const selectedFile = target.files?.[0] || null
  if (selectedFile && fileSizeVerification(selectedFile, true)) {
    toast.error(t('error.fileSizeExceeded', { size: MAX_FILE_SIZE_MB }))
    target.value = ''
    file.value = null
    resetResult()
    return
  }
  file.value = selectedFile
  resetResult()
}

const formatBytes = (bytes: number) => {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

const historyLabel = (thinkingValue: boolean) => {
  if (supportsThinking.value) {
    return thinkingValue ? t('modelSettings.debug.thinkOn') : t('modelSettings.debug.thinkOff')
  }
  return t('modelSettings.debug.runLabel', { n: runSequence })
}

const runDebug = async () => {
  if (!selectedModel.value?.id || !canRun.value || running.value) return
  running.value = true
  try {
    const thinkingValue = supportsThinking.value ? thinking.value : false
    const nextResult = await debugModel(selectedModel.value.id, {
      input: input.value.trim(),
      documents: documents.value,
      file: file.value,
      options: isChat.value ? {
        system_prompt: systemPrompt.value.trim() || undefined,
        temperature: temperature.value,
        top_p: topP.value,
        max_tokens: maxTokens.value,
        thinking: thinkingValue,
      } : {},
    })
    result.value = nextResult
    history.value.unshift({
      id: ++runSequence,
      label: historyLabel(thinkingValue),
      result: nextResult,
    })
    history.value = history.value.slice(0, 6)
    resultTab.value = 'response'
  } catch (error: any) {
    toast.error(error?.message || t('modelSettings.debug.requestFailed'))
  } finally {
    running.value = false
  }
}

const copyResult = async () => {
  if (!result.value) return
  if (await copyToClipboard(JSON.stringify(result.value, null, 2))) toast.success(t('common.copied'))
  else toast.error(t('common.copyFailed'))
}

onBeforeUnmount(() => {
  if (document.activeElement instanceof HTMLElement) {
    document.activeElement.blur()
  }
})
</script>

<style scoped>
 .setting-drawer__section { padding: var(--space-4) 0; border-top: 2px solid var(--rule); }
.form-item { display: grid; gap: var(--space-2); margin-bottom: var(--space-4); }
.form-label { font-size: var(--type-label-size); font-weight: var(--type-label-weight); }
.form-desc { color: var(--muted); font-size: var(--type-note-size); }
.model-type-options, .switch-field, .result-tabs, .metric-list { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.model-type-option, .history-item { padding: var(--space-2); color: var(--text); background: var(--bg); border: 1px solid var(--border); font: inherit; cursor: pointer; }
.model-type-option.is-active, .history-item--active { background: var(--text); color: var(--bg); }
.model-type-option:focus-visible, .history-item:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.parameter-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: var(--space-3); }
.json-output { white-space: pre-wrap; overflow-wrap: anywhere; font-family: var(--mono); background: var(--surface-soft); padding: var(--space-3); }
.result-banner--error, .result-error { color: var(--danger); }
.file-picker__input { max-width: 100%; }

</style>
