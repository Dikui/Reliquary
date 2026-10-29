<template>
  <Dialog v-model="dialogVisible" :title="isEdit ? $t('model.editor.editTitle') : $t('model.editor.addTitle')" :description="getModalDescription()" content-class="model-editor-dialog">

    <!--
      Footer-left slot: connection-test button lives here so it sits next to
      Save/Cancel — primary actions all aligned along the bottom of the
      drawer. Avoids the "test, then scroll back down to save" dance.
      Mirrors the pattern used in ModelSettings' provider drawer.
    -->
    <template #footer><div class="settings-actions"><template v-if="formData.source === 'remote'">
      <Button variant="default" @click="checkRemoteAPI" :loading="checking"
        :disabled="!formData.modelName || !formData.baseUrl">

        {{ checking ? $t('model.editor.testing') : $t('model.editor.testConnection') }}
      </Button>
      <span v-if="remoteChecked" :class="['footer-test-message', remoteAvailable ? 'success' : 'error']"
        :title="remoteMessage">
        {{ remoteMessage }}
      </span>
</template><Button :disabled="saving" @click="handleCancel">{{ $t('common.cancel') }}</Button><Button variant="primary" :loading="saving" @click="handleConfirm">{{ $t('common.save') }}</Button></div></template>

    <form novalidate class="settings-form model-editor-form" @submit.prevent="handleConfirm">

      <section v-if="!isEdit" class="setting-drawer__section">
        <h4 class="setting-drawer__section-title">{{ $t('model.editor.sectionType') }}</h4>
        <div class="model-type-options" role="radiogroup" :aria-label="$t('model.editor.typeLabel')">
          <button
            v-for="opt in modelTypeChoices"
            :key="opt.value"
            type="button"
            class="model-type-option"
            :class="{ 'is-active': activeModelType === opt.value }"
            role="radio"
            :aria-checked="activeModelType === opt.value"
            @click="selectModelType(opt.value)"
          >

            <span class="model-type-option__label">{{ opt.label }}</span>
          </button>
        </div>
      </section>

      <!--
        Section 1 — 模型来源 + 模型名称（来源直接决定下方字段，所以放一节）
      -->
      <section class="setting-drawer__section">
        <h4 class="setting-drawer__section-title">{{ $t('model.editor.sectionSource') }}</h4>

        <div class="form-item">
          <!--
            Section title already says 「模型来源」，所以这里不再重复 label，
            直接把分段控件作为 section 的首个内容呈现，避免「双标题」感。
          -->
          <div class="source-options" role="radiogroup" :aria-label="$t('model.editor.sourceLabel')">
            <button
              type="button"
              class="source-option"
              :class="{ 'is-active': formData.source === 'remote' }"
              role="radio"
              :aria-checked="formData.source === 'remote'"
              @click="handleSourceChange('remote')"
            >

              <span class="source-option__label">{{ $t('model.editor.sourceRemote') }}</span>
            </button>
            <button
              type="button"
              class="source-option"
              :class="{ 'is-active': formData.source === 'local', 'is-disabled': ollamaServiceStatus === false || activeModelType === 'rerank' }"
              :disabled="ollamaServiceStatus === false || activeModelType === 'rerank'"
              role="radio"
              :aria-checked="formData.source === 'local'"
              @click="handleSourceChange('local')"
            >

              <span class="source-option__label">{{ $t('model.editor.sourceLocal') }}</span>
            </button>
            <button v-if="activeModelType === 'chat'" type="button" class="source-option" :class="{ 'is-active': formData.source === 'chatgpt' }" role="radio" :aria-checked="formData.source === 'chatgpt'" @click="handleSourceChange('chatgpt')">
              <span class="source-option__label">{{ $t('chatgpt.source') }}</span>
            </button>
          </div>

          <!-- ReRank模型不支持Ollama的提示信息 -->
          <div v-if="activeModelType === 'rerank'" class="ollama-unavailable-tip rerank-tip">

            <span class="tip-text">{{ $t('model.editor.ollamaNotSupportRerank') }}</span>
          </div>

          <!-- Ollama不可用时的提示信息 -->
          <div v-else-if="shouldShowOllamaUnavailableTip(formData.source, activeModelType, ollamaServiceStatus)"
            class="ollama-unavailable-tip">

            <span class="tip-text">{{ $t('model.editor.ollamaUnavailable') }}</span>
            <Button variant="ghost" @click="goToOllamaSettings" class="tip-link">

              {{ $t('model.editor.goToOllamaSettings') }}
            </Button>
          </div>
        </div>

        <template v-if="formData.source === 'chatgpt'">
          <p class="form-desc">{{ $t('chatgpt.usingPlan') }} · <a href="https://chatgpt.com/settings/usage" target="_blank" rel="noopener noreferrer">{{ $t('chatgpt.manageUsage') }}</a></p>
          <div class="form-item">
            <label for="chatgpt-model-account" class="form-label required">{{ $t('chatgpt.account') }}</label>
            <Select id="chatgpt-model-account" v-model="formData.chatgptAccountID" :loading="loadingChatGPT" :placeholder="$t('chatgpt.selectAccount')" :aria-label="$t('chatgpt.account')" @update:model-value="loadChatGPTModels">
              <option v-for="account in chatgptAccounts" :key="account.id" :value="account.id" :disabled="account.status !== 'connected'" >{{ chatGPTAccountLabel(account) }}</option>
            </Select>
          </div>
          <div class="form-item">
            <label for="chatgpt-model-slug" class="form-label required">{{ $t('model.modelName') }}</label>
            <Select id="chatgpt-model-slug" v-model="formData.modelName" :loading="loadingChatGPT" :placeholder="$t('chatgpt.selectModel')" :aria-label="$t('model.modelName')" @update:model-value="selectChatGPTModel">
              <option v-for="model in chatgptModels" :key="model.slug" :value="model.slug" >{{ model.display_name }}</option>
            </Select>
            <Button variant="ghost" class="refresh-btn" :loading="loadingChatGPT" :disabled="loadingChatGPT || !formData.chatgptAccountID" @click="loadChatGPTModels">

              {{ loadingChatGPT ? $t('common.loading') : $t('chatgpt.refreshModels') }}
            </Button>
          </div>
          <p v-if="chatgptError" class="form-desc form-desc--warn" role="alert">{{ chatgptError }}</p>
          <Button variant="ghost" @click="goToChatGPTSettings">{{ $t('chatgpt.openSettings') }}</Button>
        </template>

        <!-- Ollama 本地模型选择器 -->
        <div v-if="formData.source === 'local'" class="form-item">
          <label class="form-label required" :for="`${formId}-field-1`">{{ $t('model.modelName') }}</label>
          <div class="model-select-row">
            <div class="ollama-model-picker">
              <Input v-model="searchKeyword" :aria-label="$t('model.searchPlaceholder')" :placeholder="$t('model.searchPlaceholder')" :disabled="downloading" @focus="loadOllamaModels" :id="`${formId}-field-1`" />
              <Select v-model="formData.modelName" :aria-label="$t('model.modelName')" :disabled="downloading">
                <option value="">{{ $t('model.searchPlaceholder') }}</option>
                <option v-for="model in filteredOllamaModels" :key="model.name" :value="model.name">{{ model.name }} ({{ formatModelSize(model.size) }})</option>
                <option v-if="showDownloadOption" :value="`__download__${searchKeyword}`">{{ $t('model.editor.downloadLabel', { keyword: searchKeyword }) }}</option>
              </Select>
              <p v-if="downloading" role="status">{{ currentDownloadModel }} {{ downloadProgress.toFixed(1) }}%</p>
              <progress v-if="downloading" :value="downloadProgress" max="100" :aria-label="$t('model.editor.downloadLabel', { keyword: currentDownloadModel })" />
            </div>

            <!-- 刷新按钮 -->
            <Button variant="ghost" :loading="loadingOllamaModels" @click="refreshOllamaModels"
              class="refresh-btn">

              {{ $t('model.editor.refreshList') }}
            </Button>
          </div>
        </div>
      </section>

      <!-- Remote API 配置 -->
      <template v-if="formData.source === 'remote'">
        <section class="setting-drawer__section">
          <h4 class="setting-drawer__section-title">{{ $t('model.editor.sectionProvider') }}</h4>

          <!-- 厂商选择器 -->
          <div class="form-item">
            <label class="form-label" :for="`${formId}-field-2`">{{ $t('model.editor.providerLabel') }}</label>
            <Select v-model="formData.provider" :placeholder="$t('model.editor.providerPlaceholder')"
              @update:model-value="handleProviderChange" :id="`${formId}-field-2`">
              <option v-for="opt in providerOptions" :key="opt.value" :value="opt.value"
               >{{ opt.label }}</option>
            </Select>
          </div>

          <!-- 模型名称 -->
          <div class="form-item">
            <label class="form-label required" :for="`${formId}-field-3`">{{ $t('model.modelName') }}</label>
            <Input v-model="formData.modelName" required maxlength="100" :placeholder="getModelNamePlaceholder()" :id="`${formId}-field-3`" />
          </div>

          <div class="form-item">
            <label class="form-label" :for="`${formId}-field-4`">{{ $t('model.editor.displayNameLabel') }}</label>
            <Input v-model="formData.displayName" :placeholder="$t('model.editor.displayNamePlaceholder')" :id="`${formId}-field-4`" />
            <p class="form-desc">{{ $t('model.editor.displayNameDesc') }}</p>
          </div>

          <div class="form-item">
            <label class="form-label required" :for="`${formId}-field-5`">{{ $t('model.editor.baseUrlLabel') }}</label>
            <Input v-model="formData.baseUrl" required :placeholder="getBaseUrlPlaceholder()" :id="`${formId}-field-5`" />
          </div>

          <div class="form-item">
            <label class="form-label" :for="`${formId}-field-6`">{{ $t('model.editor.apiKeyOptional') }}</label>
            <!--
              Edit mode: credentials live behind the /credentials subresource
              of the model — managed by the shared CredentialResource card,
              which now renders an INPUT-LOOKING row (32px tall, same border
              + radius as t-input) so it sits flush with the Base URL field
              above and the 自定义请求头 controls below — no more
              "card inside a card" feel.
              Create mode: the resource doesn't exist yet, so we render a
              plain password input with a leading lock icon and a trailing
              show/hide eye toggle.
            -->
            <CredentialResource v-if="isEdit && props.modelData?.id" :api="credentialApi" :fields="credentialFields"
              :meta="credentialMeta" />
            <Input v-else v-model="formData.apiKey" :aria-label="$t('model.editor.apiKeyOptional')" :type="showApiKey ? 'text' : 'password'"
              :placeholder="apiKeyPlaceholder"
              class="api-key-input" autocomplete="off" spellcheck="false" :id="`${formId}-field-6`">

            </Input>
            <Button v-if="!isEdit" variant="ghost" :aria-label="$t(showApiKey ? 'common.hide' : 'common.show')" @click="showApiKey = !showApiKey">{{ $t(showApiKey ? 'common.hide' : 'common.show') }}</Button>
          </div>

          <!-- 自定义 HTTP Header（类似 OpenAI Python SDK 的 extra_headers） -->
          <div class="form-item">
            <div class="custom-headers-header">
              <label class="form-label" style="margin-bottom: 0;">{{ $t('model.editor.customHeadersLabel') }}</label>
              <Button variant="ghost" @click="addCustomHeader">

                {{ $t('model.editor.customHeadersAdd') }}
              </Button>
            </div>
            <p class="form-desc custom-headers-desc">{{ $t('model.editor.customHeadersDesc') }}</p>
            <div v-if="formData.customHeaders && formData.customHeaders.length > 0" class="custom-headers-list">
              <div v-for="(item, idx) in formData.customHeaders" :key="idx" class="custom-header-row">
                <Input v-model="item.key" :placeholder="$t('model.editor.customHeadersKeyPlaceholder')"
                  class="custom-header-key" />
                <Input v-model="item.value" :placeholder="$t('model.editor.customHeadersValuePlaceholder')"
                  class="custom-header-value" />
                <Button variant="ghost" class="custom-header-remove"
                  @click="removeCustomHeader(idx)" :aria-label="$t('common.delete')">
                  {{ $t('common.delete') }}
                </Button>
              </div>
            </div>
          </div>

          <!--
            Connection test action moved to the drawer footer (footer-left
            slot above) so primary actions live in one row at the bottom.
          -->
        </section>
      </template>

      <!-- Section 3 — 高级选项（仅在有内容时渲染，避免空 section 出现底部分隔线） -->
      <section v-if="['embedding', 'chat'].includes(activeModelType)" class="setting-drawer__section">
        <h4 class="setting-drawer__section-title">{{ $t('model.editor.sectionAdvanced') }}</h4>

        <!-- Embedding 专用：维度 -->
        <div v-if="activeModelType === 'embedding'" class="form-item">
          <label class="form-label" :for="`${formId}-field-8`">{{ $t('model.editor.dimensionLabel') }}</label>
          <div class="dimension-control">
            <Input :model-value="formData.dimension ?? ''" @update:model-value="formData.dimension = $event === '' ? undefined : Number($event)" type="number" :min="128" :max="4096"
              :placeholder="$t('model.editor.dimensionPlaceholder')"
              :disabled="!formData.supportsDimensionOverride || (formData.source === 'local' && checking)" :id="`${formId}-field-8`" />
            <!-- Ollama 本地模型：自动检测维度按钮 -->
            <Button v-if="formData.source === 'local' && formData.modelName" variant="ghost"
              :loading="checking" @click="checkOllamaDimension" class="dimension-check-btn">

              {{ $t('model.editor.checkDimension') }}
            </Button>
          </div>
          <p v-if="dimensionChecked && dimensionMessage" class="dimension-hint" :class="{ success: dimensionSuccess }">
            {{ dimensionMessage }}
          </p>
        </div>

        <div v-if="activeModelType === 'embedding'" class="form-item">
          <label class="form-label" :for="`${formId}-field-9`">{{ $t('model.editor.dimensionOverrideLabel') }}</label>
          <div class="vision-toggle">
            <Switch v-model="formData.supportsDimensionOverride" :aria-label="$t('model.editor.dimensionOverrideLabel')" :id="`${formId}-field-9`" />
            <span class="form-desc form-desc--inline">{{ $t('model.editor.dimensionOverrideDesc') }}</span>
          </div>
        </div>

        <div v-if="activeModelType === 'embedding'" class="form-item">
          <label class="form-label" :for="`${formId}-field-10`">{{ $t('model.editor.supportsImagesLabel') }}</label>
          <div class="vision-toggle">
            <Switch v-model="formData.supportsImages" :disabled="formData.source === 'local'" :aria-label="$t('model.editor.supportsImagesLabel')" :id="`${formId}-field-10`" />
            <span class="form-desc form-desc--inline">{{ $t(formData.source === 'local' ? 'model.editor.supportsImagesLocalHint' : 'model.editor.supportsImagesDesc') }}</span>
          </div>
        </div>
        <div v-if="activeModelType === 'embedding' && formData.supportsImages && formData.source === 'remote'" class="form-item">
          <label class="form-label" :for="`${formId}-field-11`">{{ $t('model.editor.multimodalFormatLabel') }}</label>
          <Select v-model="formData.multimodalFormat" :aria-label="$t('model.editor.multimodalFormatLabel')" :id="`${formId}-field-11`">
            <option value="jina" >{{ $t('model.editor.multimodalFormatJina') }}</option>
            <option value="dashscope" >{{ $t('model.editor.multimodalFormatDashScope') }}</option>
          </Select>
          <p class="form-desc">{{ $t('model.editor.multimodalFormatHint') }}</p>
        </div>

        <!-- Text and vision model context window. -->
        <div v-if="activeModelType === 'chat'" class="form-item">
          <label class="form-label" :for="`${formId}-field-12`">{{ $t('model.editor.contextWindowLabel') }}</label>
          <Input :model-value="formData.contextWindow ?? ''" @update:model-value="formData.contextWindow = $event === '' ? undefined : Number($event)" type="number" :min="1024" :max="10000000"
            :placeholder="$t('model.editor.contextWindowPlaceholder', { value: DEFAULT_MODEL_CONTEXT_WINDOW })" :id="`${formId}-field-12`" />
          <p class="form-desc">{{ $t('model.editor.contextWindowDesc') }}</p>
        </div>

        <!-- Chat: supports vision toggle -->
        <div v-if="activeModelType === 'chat'" class="form-item">
          <label class="form-label" :for="`${formId}-field-13`">{{ $t('model.editor.supportsVisionLabel') }}</label>
          <div class="vision-toggle">
            <Switch v-model="formData.supportsVision" :aria-label="$t('model.editor.supportsVisionLabel')" :id="`${formId}-field-13`" />
            <span class="form-desc form-desc--inline">{{ $t('model.editor.supportsVisionDesc') }}</span>
          </div>
        </div>

        <!-- Chat + 远程 API：思考模式参数格式 -->
        <div v-if="showThinkingControlField" class="form-item">
          <label class="form-label" :for="`${formId}-field-14`">{{ $t('model.editor.thinkingControlLabel') }}</label>
          <Select
            v-model="formData.thinkingControl"
            :key="`thinking-${formData.id}-${formData.thinkingControl}`"
            @update:model-value="onThinkingControlManualPick"
           :id="`${formId}-field-14`">
            <option
              v-for="opt in thinkingControlOptions"
              :key="opt.value"
              :value="opt.value"
            >{{ opt.label }}</option>
          </Select>
          <p class="form-desc">{{ $t('model.editor.thinkingControlDesc') }}</p>
        </div>

        <!--
          Background concurrency cap for this model. Only chat / embedding
          are gated by the governor (see internal/models/limiter), so we surface
          it just for those three. 0 = fall back to the global default.
        -->
        <div class="form-item">
          <label class="form-label" :for="`${formId}-field-15`">{{ $t('model.editor.maxConcurrencyLabel') }}</label>
          <Input :model-value="formData.maxConcurrency ?? ''" @update:model-value="formData.maxConcurrency = $event === '' ? undefined : Number($event)" type="number" :min="0" :max="4096"
            :placeholder="$t('model.editor.maxConcurrencyPlaceholder')" :id="`${formId}-field-15`" />
          <p class="form-desc">{{ $t('model.editor.maxConcurrencyDesc') }}</p>
        </div>
      </section>

      <section class="pricing-config" :aria-label="$t('ikb18.priceTitle')">
        <h4 class="setting-drawer__section-title">{{ $t('ikb18.priceTitle') }}</h4>
        <p v-if="formData.source === 'chatgpt'" class="form-desc">{{ $t('ikb18.priceNotApplicable') }}</p>
        <template v-else>
          <div class="form-item"><label class="form-label" for="model-price-input">{{ $t(activeModelType === 'chat' ? 'ikb18.priceInput' : 'ikb18.priceTextInput') }}</label><Input id="model-price-input" :model-value="formData.inputPerMillion ?? ''" @update:model-value="formData.inputPerMillion = $event" type="number" :min="0" :aria-label="$t(activeModelType === 'chat' ? 'ikb18.priceInput' : 'ikb18.priceTextInput')" /></div>
          <div v-if="activeModelType !== 'chat'" class="form-item"><label class="form-label" for="model-price-image">{{ $t('ikb18.priceImageInput') }}</label><Input id="model-price-image" :model-value="formData.imageInputPerMillion ?? ''" @update:model-value="formData.imageInputPerMillion = $event" type="number" :min="0" :aria-label="$t('ikb18.priceImageInput')" /></div>
          <div v-else class="form-item"><label class="form-label" for="model-price-output">{{ $t('ikb18.priceOutput') }}</label><Input id="model-price-output" :model-value="formData.outputPerMillion ?? ''" @update:model-value="formData.outputPerMillion = $event" type="number" :min="0" :aria-label="$t('ikb18.priceOutput')" /></div>
          <div class="form-item"><label class="form-label" for="model-price-currency">{{ $t('ikb18.priceCurrency') }}</label><Input id="model-price-currency" v-model="formData.priceCurrency" placeholder="USD" :maxlength="3" :aria-label="$t('ikb18.priceCurrency')" /></div>
          <p class="form-desc">{{ $t('ikb18.priceHint') }}</p>
          <p v-if="activeModelType !== 'chat'" class="form-desc">{{ $t('ikb18.priceModalityHint') }}</p>
        </template>
      </section>

    </form>
  </Dialog>
</template>

<script setup lang="ts">
import { Button, Input, Select, Switch, Dialog, toast, confirm } from '@/components/ui'
import '@/styles/settings-form.css'
import { ref, watch, computed, onUnmounted, nextTick, useId } from 'vue'
import { checkOllamaModels, checkRemoteModel, testEmbeddingModel, checkRerankModel, listOllamaModels, downloadOllamaModel, getDownloadProgress, checkOllamaStatus, listModelProviders, type OllamaModelInfo, type ModelProviderOption } from '@/api/initialization'
import {
  putModelCredentials,
  deleteModelCredentialField,
  type ModelCredentialField,
} from '@/api/model'
import { useI18n } from 'vue-i18n'
import { useUIStore } from '@/stores/ui'
import {
  defaultThinkingControl,
  resolveThinkingControl,
  type ThinkingControlValue,
} from '@/utils/thinkingControl'
import { DEFAULT_MODEL_CONTEXT_WINDOW } from '@/utils/contextWindow'
import CredentialResource, {
  type CredentialFieldDef,
  type CredentialResourceApi,
} from '@/components/credentials/CredentialResource.vue'
import { shouldShowOllamaUnavailableTip } from '@/components/modelEditorSourceState'
import { getChatGPTAccounts, listChatGPTModels, chatGPTAccountLabel, type ChatGPTAccount, type ChatGPTModel } from '@/api/chatgpt'

interface CustomHeaderItem {
  key: string
  value: string
}

interface ModelFormData {
  pricing?: import('@/api/model').ModelPricing | null
  inputPerMillion?: number | string | null
  imageInputPerMillion?: number | string | null
  outputPerMillion?: number | string | null
  priceCurrency?: string
  id: string
  name: string
  source: 'local' | 'remote' | 'chatgpt'
  chatgptAccountID?: string
  provider?: string // Provider identifier: openai, aliyun, zhipu, generic, etc.
  modelName: string
  displayName?: string
  baseUrl?: string
  apiKey?: string
  dimension?: number
  supportsDimensionOverride?: boolean
  supportsImages?: boolean
  multimodalFormat?: 'jina' | 'dashscope' | ''
  interfaceType?: 'ollama' | 'openai'
  isDefault: boolean
  supportsVision?: boolean
  /** 生成 上下文窗口（token）。空/0 表示使用默认 200000。 */
  contextWindow?: number
  /** 后台任务对该模型的并发上限；0/undefined 表示沿用全局默认。仅 chat/embedding 生效。 */
  maxConcurrency?: number
  /** extra_config.thinking_control maps model thinking parameters to API fields. */
  thinkingControl?: string
  // 自定义 HTTP 请求头（类似 OpenAI Python SDK 的 extra_headers）
  customHeaders?: CustomHeaderItem[]
}

type EditorModelType = 'chat' | 'embedding' | 'rerank'

interface Props {
  visible: boolean
  modelType: EditorModelType
  modelData?: ModelFormData | null
}

const { t, te } = useI18n()
const formId = useId()
const uiStore = useUIStore()

const props = withDefaults(defineProps<Props>(), {
  visible: false,
  modelData: null
})

const emit = defineEmits<{
  'update:visible': [value: boolean]
  'confirm': [data: ModelFormData & { modelType?: EditorModelType }]
}>()

const draftModelType = ref<EditorModelType>(props.modelType)

const isEdit = computed(() => !!props.modelData)

const activeModelType = computed(() => (
  isEdit.value ? props.modelType : draftModelType.value
))

const modelTypeChoices = computed(() => ([
  { value: 'chat' as const, label: t('modelSettings.typeShort.chat'), icon: 'chat' },
  { value: 'embedding' as const, label: t('modelSettings.typeShort.embedding'), icon: 'chart-bubble' },
  { value: 'rerank' as const, label: t('modelSettings.typeShort.rerank'), icon: 'filter-sort' },
]))

// API 返回的 Provider 列表
const apiProviderOptions = ref<ModelProviderOption[]>([])
const loadingProviders = ref(false)

// 从 API 获取 Provider 列表
let providerRequestVersion = 0
const loadProviders = async () => {
  const requestVersion = ++providerRequestVersion
  const modelType = activeModelType.value
  loadingProviders.value = true
  apiProviderOptions.value = []
  try {
    const providers = await listModelProviders(modelType)
    if (requestVersion === providerRequestVersion) {
      apiProviderOptions.value = providers
      if (
        formData.value.provider
        && !providers.some(p => p.value === formData.value.provider)
        && providers.some(p => p.value === 'generic')
      ) {
        formData.value.provider = 'generic'
      }
    }
  } catch (error) {
    console.error('Failed to load providers from API', error)
  } finally {
    if (requestVersion === providerRequestVersion) {
      loadingProviders.value = false
    }
  }
}

// 地址和支持类型由后端预设表提供，显示文本使用现有 i18n。
const providerOptions = computed(() => apiProviderOptions.value
  .filter(p => p.modelTypes.includes(activeModelType.value))
  .map(p => ({
    ...p,
    label: te(`model.editor.providers.${p.value}.label`)
      ? t(`model.editor.providers.${p.value}.label`)
      : p.label,
    description: te(`model.editor.providers.${p.value}.description`)
      ? t(`model.editor.providers.${p.value}.description`)
      : p.description,
  }))
)

const dialogVisible = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
})

const showThinkingControlField = computed(() =>
  activeModelType.value === 'chat' && formData.value.source === 'remote',
)

const resolvedThinkingControl = (): ThinkingControlValue =>
  defaultThinkingControl(
    formData.value.provider || '',
    formData.value.modelName || '',
  )

/** 用户是否手动改过思考参数格式（改过则不再自动覆盖，直到换服务商） */
const thinkingControlManual = ref(false)
/** 正在从 modelData 灌入表单，忽略厂商/来源控件的程序化 change 副作用 */
const hydratingForm = ref(false)

const onThinkingControlManualPick = () => {
  thinkingControlManual.value = true
}

const syncThinkingControlToForm = (force = false) => {
  if (!showThinkingControlField.value) return
  if (!force && !isEdit.value && thinkingControlManual.value) return
  formData.value.thinkingControl = resolvedThinkingControl()
}

const applyThinkingControlFromModelData = () => {
  if (!props.modelData || activeModelType.value !== 'chat' || formData.value.source !== 'remote') return
  thinkingControlManual.value = !!props.modelData.thinkingControl
  formData.value.thinkingControl = resolveThinkingControl(
    props.modelData.thinkingControl,
    formData.value.provider || props.modelData.provider || '',
    formData.value.modelName || props.modelData.modelName || '',
  )
}

const thinkingControlOptions = computed(() => {
  const keys = ['none', 'chatTemplateKwargs', 'enableThinking', 'thinkingType'] as const
  const values = ['none', 'chat_template_kwargs', 'enable_thinking', 'thinking_type'] as const
  return keys.map((key, i) => ({
    value: values[i],
    label: t(`model.editor.thinkingControl.${key}.label`),
    hint: t(`model.editor.thinkingControl.${key}.hint`),
  }))
})

// Header icon matches the model kind shown in the list.
const modelTypeIcon = computed(() => {
  const map: Record<string, string> = {
    chat: 'chat',
    embedding: 'chart-bubble',
    rerank: 'filter-sort',
  }
  return map[activeModelType.value] || 'setting'
})

// Credential resource binding for the shared <CredentialResource> component.
const credentialFields = computed<CredentialFieldDef<ModelCredentialField>[]>(() => [
  { key: 'api_key', label: t('model.editor.apiKeyOptional') },
])

const credentialApi = computed<CredentialResourceApi<ModelCredentialField>>(() => {
  const id = props.modelData?.id ?? ''
  return {
    save: async (patch) => {
      const meta = await putModelCredentials(id, patch)
      return meta.fields
    },
    remove: async (field) => {
      await deleteModelCredentialField(id, field)
    },
  }
})

// Initial credential metadata. ModelSettings.convertToLegacyFormat
// preserves `credentials` from the main ListModels response so the card
// renders the correct "Configured" state on dialog open.
const credentialMeta = computed(() => (props.modelData as any)?.credentials ?? {
  api_key: { configured: false },
})

// Placeholder hint for the create-mode API key input. Edit mode replaces
// this input entirely with a <CredentialResource> card.
const apiKeyPlaceholder = computed(() => t('model.editor.apiKeyPlaceholder'))

const saving = ref(false)
// Toggles the create-mode API key input between masked and plain text. Lets
// the user proofread a freshly pasted secret without losing the password
// affordance for everyday use. Reset every time the drawer closes (see
// reset block in the visible watcher) so we never leak the previous value
// across editor sessions.
const showApiKey = ref(false)
const modelChecked = ref(false)
const modelAvailable = ref(false)
const checking = ref(false)
const remoteChecked = ref(false)
const remoteAvailable = ref(false)
const remoteMessage = ref('')
const dimensionChecked = ref(false)
const dimensionSuccess = ref(false)
const dimensionMessage = ref('')

// Ollama 模型状态
const ollamaModelList = ref<OllamaModelInfo[]>([])
const loadingOllamaModels = ref(false)
const searchKeyword = ref('')
const downloading = ref(false)
const downloadProgress = ref(0)
const currentDownloadModel = ref('')
let downloadInterval: any = null

// Ollama 服务状态
const ollamaServiceStatus = ref<boolean | null>(null)
const checkingOllamaStatus = ref(false)

const formData = ref<ModelFormData>({
  id: '',
  name: '',
  source: 'remote',
  provider: 'generic',
  modelName: '',
  displayName: '',
  baseUrl: '',
  apiKey: '',
  dimension: undefined,
  supportsDimensionOverride: false,
  supportsImages: false,
  multimodalFormat: 'jina',
  interfaceType: 'ollama',
  isDefault: false,
  supportsVision: false,
  contextWindow: undefined,
  maxConcurrency: undefined,
  inputPerMillion: '', imageInputPerMillion: '', outputPerMillion: '', priceCurrency: '',
  thinkingControl: defaultThinkingControl('generic', ''),
  customHeaders: [],
})

// 获取弹窗描述文字
const getModalDescription = () => {
  const key = `model.editor.description.${activeModelType.value}` as const
  return t(key) || t('model.editor.description.default')
}

// 获取模型名称占位符
const getModelNamePlaceholder = () => {
  return formData.value.source === 'local'
    ? t('model.editor.modelNamePlaceholder.local')
    : t('model.editor.modelNamePlaceholder.remote')
}

const getBaseUrlPlaceholder = () => {
  return t('model.editor.baseUrlPlaceholder')
}

// 检查Ollama服务状态
const checkOllamaServiceStatus = async () => {
  console.log('开始检查Ollama服务状态...')
  checkingOllamaStatus.value = true
  try {
    const result = await checkOllamaStatus()
    ollamaServiceStatus.value = result.available
    console.log('Ollama服务状态检查完成:', result.available)
  } catch (error) {
    console.error('检查Ollama服务状态失败:', error)
    ollamaServiceStatus.value = false
  } finally {
    checkingOllamaStatus.value = false
  }

  // Ollama 不可用时，新增场景下默认切换到 remote
  if (ollamaServiceStatus.value === false && !isEdit.value && formData.value.source === 'local') {
    await handleSourceChange('remote')
  }
}

// 打开Ollama设置窗口
const goToOllamaSettings = async () => {
  console.log('点击跳转到Ollama设置按钮')
  // 关闭当前弹窗
  emit('update:visible', false)

  // 先关闭设置弹窗（如果已打开）
  if (uiStore.showSettingsModal) {
    uiStore.closeSettings()
    // 等待 DOM 更新
    await nextTick()
  }

  // 打开设置窗口并直接跳转到Ollama设置
  console.log('调用uiStore.openSettings')
  uiStore.openSettings('ollama')
  console.log('uiStore.openSettings调用完成')
}

// 上一次打开时的 modelData id：用来判断切换模型/新增 vs. 同一次新增的连续打开
const lastOpenedModelId = ref<string | null>(null)

const selectModelType = async (type: EditorModelType) => {
  if (isEdit.value || draftModelType.value === type) return
  draftModelType.value = type

  if (type === 'rerank' || (type !== 'chat' && formData.value.source === 'chatgpt')) {
    await handleSourceChange('remote')
  }
  if (type !== 'embedding') {
    formData.value.dimension = undefined
    formData.value.supportsDimensionOverride = false
    formData.value.supportsImages = false
    formData.value.multimodalFormat = 'jina'
    dimensionChecked.value = false
    dimensionSuccess.value = false
    dimensionMessage.value = ''
  }
  if (type !== 'chat') {
    formData.value.supportsVision = false
    thinkingControlManual.value = false
  }
  remoteChecked.value = false
  remoteAvailable.value = false
  remoteMessage.value = ''

  await loadProviders()
  const supported = providerOptions.value.some(p => p.value === formData.value.provider)
  if (!supported) {
    formData.value.provider = 'generic'
    formData.value.baseUrl = ''
  } else {
    await handleProviderChange(formData.value.provider || 'generic')
  }
  if (showThinkingControlField.value && !isEdit.value) {
    thinkingControlManual.value = false
    syncThinkingControlToForm(true)
  }
}

// 监听 visible 变化，初始化表单
const loadedConfiguration = ref('')
const configurationState = () => JSON.stringify({ ...formData.value, apiKey: undefined, credentials: undefined })
watch(() => props.visible, (val) => {
  if (val) {
    // 检查Ollama服务状态
    checkOllamaServiceStatus()

    // 每次打开都清理上一次遗留的校验/检测结果，避免编辑别的模型时
    // 直接显示上一次的“连接成功”
    modelChecked.value = false
    modelAvailable.value = false
    remoteChecked.value = false
    remoteAvailable.value = false
    remoteMessage.value = ''
    dimensionChecked.value = false
    dimensionSuccess.value = false
    dimensionMessage.value = ''

    const currentId = props.modelData?.id ?? null
    draftModelType.value = props.modelType
    // 按当前类型加载，避免打开抽屉时沿用上一次的类型。
    loadProviders()

    hydratingForm.value = true
    try {
      if (props.modelData) {
        // 编辑：始终用最新的 modelData 覆盖。apiKey field is left blank — in
        // edit mode the credential is owned by the <CredentialResource> card,
        // not by this form's apiKey field.
        formData.value = {
          ...props.modelData,
          supportsImages: props.modelData.supportsImages ?? false,
          multimodalFormat: props.modelData.multimodalFormat || 'jina',
          apiKey: '',
          inputPerMillion: props.modelData.pricing?.input_per_million ?? '',
          imageInputPerMillion: props.modelData.pricing?.image_input_per_million ?? '',
          outputPerMillion: props.modelData.pricing?.output_per_million ?? '',
          priceCurrency: props.modelData.pricing?.currency || '',
          customHeaders: Array.isArray(props.modelData.customHeaders)
            ? props.modelData.customHeaders.map(h => ({ key: h.key, value: h.value }))
            : [],
        }
        applyThinkingControlFromModelData()
      } else if (lastOpenedModelId.value !== null || !formData.value.id) {
        // 上次是编辑某个模型，或第一次新增 → 重置成空白
        resetForm()
      }
      // 否则：连续两次"新增"打开（中间是点遮罩/ESC 关闭的）→ 保留上次填写

      if (formData.value.source === 'chatgpt') loadChatGPTAccounts()
      lastOpenedModelId.value = currentId

      // ReRank 模型强制使用 remote 来源（Ollama 不支持 ReRank）
      if (activeModelType.value === 'rerank') {
        formData.value.source = 'remote'
      }

      if (showThinkingControlField.value && !isEdit.value) {
        thinkingControlManual.value = false
        syncThinkingControlToForm(true)
      }
    } finally {
      nextTick(() => {
        loadedConfiguration.value = configurationState()
        hydratingForm.value = false
      })
    }
  }
})

// 重置表单
const resetForm = () => {
  thinkingControlManual.value = false
  formData.value = {
    id: generateId(),
    name: '', // 保留字段但不使用，保存时用 modelName
    source: 'remote',
    provider: 'generic',
    modelName: '',
    displayName: '',
    baseUrl: '',
    apiKey: '',
    dimension: undefined, // 默认不填，让用户手动输入或通过检测按钮获取
    supportsDimensionOverride: false,
    supportsImages: false,
    multimodalFormat: 'jina',
    interfaceType: undefined,
    isDefault: false,
    supportsVision: false,
    contextWindow: undefined,
    maxConcurrency: undefined,
    inputPerMillion: '', imageInputPerMillion: '', outputPerMillion: '', priceCurrency: '',
    thinkingControl: defaultThinkingControl('generic', ''),
    customHeaders: [],
  }
  modelChecked.value = false
  modelAvailable.value = false
  remoteChecked.value = false
  remoteAvailable.value = false
  remoteMessage.value = ''
  dimensionChecked.value = false
  dimensionSuccess.value = false
  dimensionMessage.value = ''
  showApiKey.value = false
}

// 处理厂商选择变化 (自动填充默认 URL)
const handleProviderChange = async (value: string) => {
  const provider = providerOptions.value.find(opt => opt.value === value)
  if (provider && provider.defaultUrls) {
    // 根据当前模型类型获取对应的默认 URL
    const defaultUrl = provider.defaultUrls[activeModelType.value]
    if (defaultUrl) {
      formData.value.baseUrl = defaultUrl
    }
    // 重置校验状态
    remoteChecked.value = false
    remoteAvailable.value = false
    remoteMessage.value = ''
  }
  if (value === 'ollama') {
    await handleSourceChange('local')
    return
  }
  if (hydratingForm.value) return
  if (activeModelType.value !== 'chat' || formData.value.source !== 'remote') return
  if (!isEdit.value) {
    thinkingControlManual.value = false
    syncThinkingControlToForm(true)
    return
  }
  // 编辑时仅用户主动换厂商才跟随默认
  thinkingControlManual.value = false
  syncThinkingControlToForm(true)
}

watch(
  () => [formData.value.source, formData.value.provider, formData.value.modelName] as const,
  ([source, provider, modelName], [prevSource, prevProvider, prevModelName]) => {
    if (hydratingForm.value || isEdit.value) return
    if (activeModelType.value !== 'chat' || source !== 'remote') return
    if (source === prevSource && provider === prevProvider && modelName === prevModelName) return

    const providerChanged = provider !== prevProvider

    if (providerChanged) {
      thinkingControlManual.value = false
      syncThinkingControlToForm(true)
      return
    }
    if (!thinkingControlManual.value) {
      syncThinkingControlToForm(true)
      return
    }
    const prevDefault = defaultThinkingControl(prevProvider || '', prevModelName || '')
    if (formData.value.thinkingControl === prevDefault) {
      syncThinkingControlToForm(true)
    }
  },
)

// 监听来源变化，重置校验状态（已合并到下面的 watch）

// 生成唯一ID
const generateId = () => {
  return `model_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`
}

// 自定义 HTTP Header 编辑
const addCustomHeader = () => {
  if (!Array.isArray(formData.value.customHeaders)) {
    formData.value.customHeaders = []
  }
  formData.value.customHeaders.push({ key: '', value: '' })
}

const removeCustomHeader = (idx: number) => {
  if (!Array.isArray(formData.value.customHeaders)) return
  formData.value.customHeaders.splice(idx, 1)
}

// 过滤后的模型列表
const filteredOllamaModels = computed(() => {
  if (!searchKeyword.value) return ollamaModelList.value
  return ollamaModelList.value.filter(model =>
    model.name.toLowerCase().includes(searchKeyword.value.toLowerCase())
  )
})

// 是否显示"下载模型"选项
const showDownloadOption = computed(() => {
  if (!searchKeyword.value.trim()) return false
  // 检查搜索词是否已存在于模型列表中
  const exists = ollamaModelList.value.some(model =>
    model.name.toLowerCase() === searchKeyword.value.toLowerCase()
  )
  return !exists
})

// 输入事件在本地列表为空时也会触发。

// 加载 Ollama 模型列表
const loadOllamaModels = async () => {
  // 只在选择 local 来源时加载
  if (formData.value.source !== 'local') return

  loadingOllamaModels.value = true
  try {
    const models = await listOllamaModels()
    ollamaModelList.value = models
  } catch (error) {
    console.error(t('model.editor.loadModelListFailed'), error)
    toast.error(t('model.editor.loadModelListFailed'))
  } finally {
    loadingOllamaModels.value = false
  }
}

const handleSourceChange = async (source: 'local' | 'remote' | 'chatgpt') => {
  if (source === 'chatgpt' && activeModelType.value !== 'chat') return
  if (formData.value.source !== source && source === 'chatgpt') {
    formData.value.modelName = ''
    formData.value.displayName = ''
    formData.value.baseUrl = ''
    formData.value.apiKey = ''
    formData.value.customHeaders = []
    formData.value.thinkingControl = undefined
  }
  if (source === 'remote' && formData.value.provider === 'ollama') {
    formData.value.provider = 'generic'
    formData.value.interfaceType = undefined
    formData.value.baseUrl = ''
  }
  formData.value.source = source
  if (source === 'chatgpt') await loadChatGPTAccounts()
  if (source === 'local') {
    formData.value.supportsImages = false
    formData.value.interfaceType = 'ollama'
    await loadOllamaModels()
  }
}

const chatgptAccounts = ref<ChatGPTAccount[]>([])
const chatgptModels = ref<ChatGPTModel[]>([])
const loadingChatGPT = ref(false), chatgptError = ref('')
let chatgptRequestVersion = 0
async function loadChatGPTAccounts() {
  const version = ++chatgptRequestVersion
  loadingChatGPT.value = true; chatgptError.value = ''
  try {
    const result = await getChatGPTAccounts()
    if (version !== chatgptRequestVersion || formData.value.source !== 'chatgpt') return
    chatgptAccounts.value = result.accounts
    if (!formData.value.chatgptAccountID) formData.value.chatgptAccountID = result.accounts.find(account => account.status === 'connected')?.id
    await loadChatGPTModels()
  } catch (err: any) { if (version === chatgptRequestVersion) chatgptError.value = err?.message || t('chatgpt.loadFailed') }
  finally { if (version === chatgptRequestVersion) loadingChatGPT.value = false }
}
async function loadChatGPTModels() {
  const version = ++chatgptRequestVersion, accountID = formData.value.chatgptAccountID
  chatgptModels.value = []; chatgptError.value = ''
  if (!accountID) { loadingChatGPT.value = false; return }
  loadingChatGPT.value = true
  try {
    const models = await listChatGPTModels(accountID)
    if (version !== chatgptRequestVersion || formData.value.source !== 'chatgpt') return
    chatgptModels.value = models
    if (!models.some(model => model.slug === formData.value.modelName)) formData.value.modelName = ''
    if (!models.length) chatgptError.value = t('chatgpt.noModels')
  } catch (err: any) { if (version === chatgptRequestVersion) chatgptError.value = err?.message || t('chatgpt.loadFailed') }
  finally { if (version === chatgptRequestVersion) loadingChatGPT.value = false }
}
function selectChatGPTModel() {
  formData.value.displayName = chatgptModels.value.find(model => model.slug === formData.value.modelName)?.display_name || ''
}
function goToChatGPTSettings() { dialogVisible.value = false; uiStore.openSettings('models') }

// 刷新模型列表
const refreshOllamaModels = async () => {
  ollamaModelList.value = [] // 清空以强制重新加载
  await loadOllamaModels()
  toast.success(t('model.editor.listRefreshed'))
}

// 监听下拉框可见性变化

// 格式化模型大小
const formatModelSize = (bytes: number): string => {
  if (!bytes || bytes === 0) return ''
  const gb = bytes / (1024 * 1024 * 1024)
  return gb >= 1 ? `${gb.toFixed(1)} GB` : `${(bytes / (1024 * 1024)).toFixed(0)} MB`
}

// 检查模型状态（Ollama本地模型）
const checkModelStatus = async () => {
  if (!formData.value.modelName || formData.value.source !== 'local') {
    return
  }

  try {
    // 调用真实 Ollama API 检查模型是否存在
    const result = await checkOllamaModels([formData.value.modelName])
    modelChecked.value = true
    modelAvailable.value = result.models[formData.value.modelName] || false
  } catch (error) {
    console.error('检查模型状态失败:', error)
    modelChecked.value = false
    modelAvailable.value = false
  }
}

// 检查 Ollama 本地 Embedding 模型维度
const checkOllamaDimension = async () => {
  if (!formData.value.modelName || formData.value.source !== 'local' || activeModelType.value !== 'embedding') {
    return
  }

  checking.value = true
  dimensionChecked.value = false
  dimensionMessage.value = ''

  try {
    const result = await testEmbeddingModel({
      source: 'local',
      modelName: formData.value.modelName,
      dimension: formData.value.dimension,
      supportsDimensionOverride: formData.value.supportsDimensionOverride ?? false,
    })

    dimensionChecked.value = true
    dimensionSuccess.value = result.available || false

    if (result.available && result.dimension) {
      formData.value.dimension = result.dimension
      dimensionMessage.value = t('model.editor.dimensionDetected', { value: result.dimension })
      toast.success(dimensionMessage.value)
    } else {
      if (result.message) {
        console.debug('Backend dimension message:', result.message)
      }
      dimensionMessage.value = t('model.editor.dimensionFailed')
      toast.warning(dimensionMessage.value)
    }
  } catch (error: any) {
    console.error('Ollama dimension check failed:', error)
    dimensionChecked.value = true
    dimensionSuccess.value = false
    dimensionMessage.value = t('model.editor.dimensionFailed')
    toast.error(dimensionMessage.value)
  } finally {
    checking.value = false
  }
}

// 检查 Remote API 连接（根据模型类型调用不同的接口）
const checkRemoteAPI = async () => {
  if (!formData.value.modelName || !formData.value.baseUrl) {
    toast.warning(t('model.editor.fillModelAndUrl'))
    return
  }

  checking.value = true
  remoteChecked.value = false
  remoteMessage.value = ''

  try {
    let result: any

    // 把表单里 Key-Value 数组形式的自定义 Header 转成后端期望的 map。
    // 跟 ModelSettings.vue 保存时一致，空行自动丢弃，保证测试连接与真正保存后的
    // 生产调用使用完全相同的 Header 集合。
    const customHeaders: Record<string, string> = {}
    if (Array.isArray(formData.value.customHeaders)) {
      for (const item of formData.value.customHeaders) {
        const key = (item?.key ?? '').trim()
        const value = (item?.value ?? '').trim()
        if (key && value) customHeaders[key] = value
      }
    }
    // 只在非空时带上字段，避免在 URL query / 日志里出现空对象
    const headerPayload = Object.keys(customHeaders).length > 0
      ? { customHeaders }
      : {}

    // 根据模型类型调用不同的校验接口
    // 编辑模式下 apiKey 由 <CredentialResource> 独立管理、不在 formData 里。
    // 把 modelId 透传给后端，让它在 apiKey 为空时自动用存储的解密值兜底，
    // 避免出现"测试连接没带 apiKey 直接失败"的情况。
    const idPayload = isEdit.value && props.modelData?.id
      ? { modelId: props.modelData.id as string }
      : {}

    switch (activeModelType.value) {
      case 'chat':
        // 生成模型（KnowledgeQA）
        result = await checkRemoteModel({
          modelName: formData.value.modelName,
          baseUrl: formData.value.baseUrl || '',
          apiKey: formData.value.apiKey || '',
          provider: formData.value.provider,
          ...idPayload,
          ...headerPayload,
        })
        break

      case 'embedding':
        // Embedding 模型
        result = await testEmbeddingModel({
          source: 'remote',
          modelName: formData.value.modelName,
          baseUrl: formData.value.baseUrl || '',
          apiKey: formData.value.apiKey || '',
          dimension: formData.value.dimension,
          supportsDimensionOverride: formData.value.supportsDimensionOverride ?? false,
          supportsImages: formData.value.supportsImages ?? false,
          multimodalFormat: formData.value.supportsImages ? (formData.value.multimodalFormat || 'jina') : '',
          provider: formData.value.provider,
          ...idPayload,
          ...headerPayload,
        })
        // 如果测试成功且返回了维度，自动填充
        if (result.available && result.dimension) {
          formData.value.dimension = result.dimension
          toast.info(t('model.editor.remoteDimensionDetected', { value: result.dimension }))
        }
        break

      case 'rerank': {
        result = await checkRerankModel({
          modelName: formData.value.modelName,
          baseUrl: formData.value.baseUrl || '',
          apiKey: formData.value.apiKey || '',
          provider: formData.value.provider,
          ...idPayload,
          ...headerPayload,
        })
        break
      }

      default:
        toast.error(t('model.editor.unsupportedModelType'))
        return
    }

    remoteChecked.value = true
    remoteAvailable.value = result.available || false
    // 之前这里把 backend 的错误 message 只丢到 console.debug，用户只能
    // 看到通用的 "连接失败" toast，根本看不出是 401 / 404 / 模型不存在
    // 还是别的什么。改成：成功时用 i18n 通用提示；失败时直接展示后端
    // 给到的具体原因（已经在后端 classifyConnectionError 中包了一层
    // 易读的中文 hint + 原始 SDK 报错），方便排查。
    if (result.available) {
      remoteMessage.value = t('model.editor.connectionSuccess')
      toast.success(remoteMessage.value)
    } else {
      remoteMessage.value = result.message || t('model.editor.connectionFailed')
      console.debug('Backend message:', result.message)
      toast.error(remoteMessage.value)
    }
  } catch (error: any) {
    console.error('Remote API check failed:', error)
    remoteChecked.value = true
    remoteAvailable.value = false
    // 后端 4xx/5xx（如 SSRF 校验失败）会走到这里。axios 拦截器把后端
    // { error: { message: "..." } } 提到了 error.message，里面已经包含
    // 易读 hint + 原因，直接展示出来，比通用 "请检查配置" 有用得多。
    remoteMessage.value = error?.message || t('model.editor.connectionConfigError')
    toast.error(remoteMessage.value)
  } finally {
    checking.value = false
  }
}

// 确认保存
const handleConfirm = async () => {
  if (isEdit.value && loadedConfiguration.value && configurationState() === loadedConfiguration.value) {
    emit('update:visible', false)
    return
  }
  try {
    // 手动校验必填字段
    if (!formData.value.modelName || !formData.value.modelName.trim()) {
      toast.warning(t('model.editor.validation.modelNameRequired'))
      return
    }

    if (formData.value.modelName.trim().length > 100) {
      toast.warning(t('model.editor.validation.modelNameMax'))
      return
    }

    if (formData.value.source === 'remote') {
      if (!formData.value.baseUrl || !formData.value.baseUrl.trim()) {
        toast.warning(t('model.editor.remoteBaseUrlRequired'))
        return
      }

      // 校验 Base URL 格式
      try {
        new URL(formData.value.baseUrl.trim())
      } catch {
        toast.warning(t('model.editor.validation.baseUrlInvalid'))
        return
      }
    }

    if (formData.value.source === 'chatgpt' && (!formData.value.chatgptAccountID || !chatgptModels.value.some(model => model.slug === formData.value.modelName))) {
      toast.warning(t('chatgpt.accountRequired'))
      return
    }
    const parseRate = (value: number | string | null | undefined) => value == null || String(value).trim() === '' ? null : Number(value)
    const inputRate = parseRate(formData.value.inputPerMillion)
    const imageRate = activeModelType.value !== 'chat' ? parseRate(formData.value.imageInputPerMillion) : null
    const outputRate = activeModelType.value === 'chat' ? parseRate(formData.value.outputPerMillion) : null
    const currency = (formData.value.priceCurrency || '').trim().toUpperCase()
    if ([inputRate, imageRate, outputRate].some(value => value !== null && (!Number.isFinite(value) || value < 0 || value > 1e9)) || (currency && !/^[A-Z]{3}$/.test(currency))) {
      toast.warning(t('ikb18.priceInvalid'))
      return
    }
    const pricing = formData.value.source === 'chatgpt' || (inputRate === null && imageRate === null && outputRate === null && !currency) ? null : { input_per_million: inputRate, ...(activeModelType.value !== 'chat' ? { image_input_per_million: imageRate } : {}), output_per_million: outputRate, currency: currency || 'USD' }
    // 执行表单验证

    // Credential removal in edit mode is handled inline by the
    // CredentialResource card (it confirms + DELETEs to /credentials), so
    // the main save flow no longer needs to confirm or handle clear flags.

    saving.value = true

    // 如果是新增且没有 id，生成一个
    if (!formData.value.id) {
      formData.value.id = generateId()
    }

    emit('confirm', {
      ...formData.value,
      pricing,
      ...(isEdit.value ? {} : { modelType: activeModelType.value }),
    })
    dialogVisible.value = false
    // 保存成功后重置草稿，下次打开新增模型时是空白
    resetForm()
    lastOpenedModelId.value = null
    // 移除此处的成功提示，由父组件统一处理
  } catch (error) {
    console.error('表单验证失败:', error)
  } finally {
    saving.value = false
  }
}

// 监听模型选择变化（处理下载逻辑和自动维度检测提示）
watch(() => formData.value.modelName, async (newValue, oldValue) => {
  if (!newValue) return

  // 处理下载逻辑
  if (newValue.startsWith('__download__')) {
    // 提取模型名称
    const modelName = newValue.replace('__download__', '')

    // 重置选择（避免显示 __download__ 前缀）
    formData.value.modelName = ''

    // 开始下载
    await startDownload(modelName)
    return
  }

  // 如果是 embedding 模型且选择的是 Ollama 本地模型，且模型名称发生了实际变化
  if (activeModelType.value === 'embedding' &&
    formData.value.source === 'local' &&
    newValue !== oldValue &&
    oldValue !== '') {
    // 提示用户可以检测维度
    toast.info(t('model.editor.dimensionHint'))
  }
})

// 开始下载模型
const startDownload = async (modelName: string) => {
  downloading.value = true
  downloadProgress.value = 0
  currentDownloadModel.value = modelName

  try {
    // 启动下载
    const result = await downloadOllamaModel(modelName)
    const taskId = result.taskId

    toast.success(t('model.editor.downloadStarted', { name: modelName }))

    // 轮询下载进度
    downloadInterval = setInterval(async () => {
      try {
        const progress = await getDownloadProgress(taskId)
        downloadProgress.value = progress.progress

        if (progress.status === 'completed') {
          // 下载完成
          clearInterval(downloadInterval)
          downloadInterval = null
          downloading.value = false

          toast.success(t('model.editor.downloadCompleted', { name: modelName }))

          // 刷新模型列表
          await loadOllamaModels()

          // 自动选中新下载的模型
          formData.value.modelName = modelName

          // 重置状态
          downloadProgress.value = 0
          currentDownloadModel.value = ''

        } else if (progress.status === 'failed') {
          // 下载失败
          clearInterval(downloadInterval)
          downloadInterval = null
          downloading.value = false
          toast.error(progress.message || t('model.editor.downloadFailed', { name: modelName }))
          downloadProgress.value = 0
          currentDownloadModel.value = ''
        }
      } catch (error) {
        console.error('获取下载进度失败:', error)
      }
    }, 1000) // 每秒查询一次

  } catch (error: any) {
    downloading.value = false
    downloadProgress.value = 0
    currentDownloadModel.value = ''
    console.error('Download start failed:', error)
    toast.error(t('model.editor.downloadStartFailed'))
  }
}

// 组件卸载时清理定时器
onUnmounted(() => {
  chatgptRequestVersion++
  if (downloadInterval) {
    clearInterval(downloadInterval)
  }
})

// 监听来源变化，清理所有状态
watch(() => formData.value.source, () => {
  if (formData.value.source !== 'chatgpt') chatgptRequestVersion++
  // 重置校验状态
  modelChecked.value = false
  modelAvailable.value = false
  remoteChecked.value = false
  remoteAvailable.value = false
  remoteMessage.value = ''
  dimensionChecked.value = false
  dimensionSuccess.value = false
  dimensionMessage.value = ''

  // 清理下载状态
  searchKeyword.value = ''
  if (downloadInterval) {
    clearInterval(downloadInterval)
    downloadInterval = null
  }
  downloading.value = false
  downloadProgress.value = 0
  currentDownloadModel.value = ''

  if (
    !hydratingForm.value
    && !isEdit.value
    && formData.value.source === 'remote'
    && activeModelType.value === 'chat'
  ) {
    thinkingControlManual.value = false
    syncThinkingControlToForm(true)
  }
})

// 监听模型名称变化，清理维度检测状态
watch(() => formData.value.modelName, () => {
  dimensionChecked.value = false
  dimensionSuccess.value = false
  dimensionMessage.value = ''
})

// 取消（点击底部"取消"按钮触发；点遮罩/ESC 不触发，从而保留草稿）
const handleCancel = () => {
  resetForm()
  lastOpenedModelId.value = null
  dialogVisible.value = false
}
</script>

<!-- 非 scoped 样式：t-select popup 渲染到 body 下，scoped 样式无法覆盖 -->

<style scoped>
 .setting-drawer__section, .pricing-config { padding: var(--space-4) 0; border-top: 2px solid var(--rule); }
.form-item { display: grid; grid-template-columns: 180px minmax(0, 1fr); gap: var(--space-2) var(--space-4); margin-bottom: var(--space-4); min-width: 0; }
.form-item > :not(.form-label) { grid-column: 2; }
.form-item > .custom-headers-header { grid-column: 1 / -1; }
.form-label { font-weight: var(--type-label-weight); font-size: var(--type-label-size); }
.form-desc, .dimension-hint { font-size: var(--type-note-size); color: var(--muted); margin: 0; overflow-wrap: anywhere; }
.model-type-options, .source-options, .vision-toggle, .model-select-row, .dimension-control, .custom-headers-header { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: center; }
.model-type-option, .source-option { padding: var(--space-2) var(--space-3); border: 1px solid var(--border); background: var(--bg); color: var(--text); font: inherit; cursor: pointer; }
.model-type-option.is-active, .source-option.is-active { background: var(--text); color: var(--bg); }
.model-type-option:focus-visible, .source-option:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.source-option:disabled { opacity: .5; cursor: not-allowed; }
.custom-header-row { display: flex; gap: var(--space-2); margin-top: var(--space-2); }
.custom-header-row .rq-control { min-width: 0; }
.custom-headers-header { justify-content: space-between; }
.ollama-model-picker { display: grid; gap: var(--space-2); flex: 1; min-width: 0; }
.ollama-model-picker progress { width: 100%; accent-color: var(--text); }
.footer-test-message { overflow-wrap: anywhere; }
.footer-test-message.error, .form-desc--warn, .ollama-unavailable-tip { color: var(--danger); }

@media (max-width: 600px) { .form-item { grid-template-columns: minmax(0, 1fr); } .form-item > :not(.form-label) { grid-column: 1; } }
</style>
