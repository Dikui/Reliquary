<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="dialogVisible" class="upload-confirm-overlay">
        <div class="upload-confirm-modal" role="dialog" :aria-label="dialogTitle">
          <button class="close-btn" type="button" :aria-label="t('general.close')" @click="handleCancel">
            <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path d="M15 5L5 15M5 5L15 15" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
            </svg>
          </button>

          <div class="upload-confirm-container">
            <aside class="files-panel">
              <div class="sidebar-header">
                <div class="sidebar-header-row">
                  <h2 class="sidebar-title">{{ dialogTitle }}</h2>
                  <div v-if="mode === 'file'" class="sidebar-header-actions">
                    <span class="files-count">{{ batchItemCount }}</span>
                    <KbUploadSourceDropdown
                      :accept-file-types="acceptFileTypes"
                      :supported-file-types="supportedFileTypes"
                      :tooltip="t('uploadConfirm.continueAdd')"
                      placement="bottom-left"
                      @files="appendFiles"
                      @url="appendUrl"
                    />
                  </div>
                </div>

              </div>

              <div class="files-list-wrap">
                <div v-if="mode === 'manual' && manualPreview" class="manual-source-panel">
                  <p class="manual-source-title" :title="manualPreview.title">{{ manualPreview.title }}</p>
                  <p class="manual-source-meta">
                    {{ t('uploadConfirm.manualCharCount', { count: manualCharCount }) }}
                  </p>
                  <div class="intake-source-fields">
                    <label class="intake-source-field"><span>{{ t('knowledgeBase.intakeAuthor') }}</span><t-input v-model="sourceInfo('manual').author" :placeholder="t('knowledgeBase.intakeAuthor')" /></label>
                    <label class="intake-source-field"><span>{{ t('knowledgeBase.intakePublisher') }}</span><t-input v-model="sourceInfo('manual').publisher" :placeholder="t('knowledgeBase.intakePublisher')" /></label>
                    <label class="intake-source-field"><span>{{ t('knowledgeBase.intakeOriginalUrl') }}</span><SafeTextarea v-model="sourceInfo('manual').original_url" :placeholder="t('knowledgeBase.intakeOriginalUrl')" :autosize="{ minRows: 2, maxRows: 4 }" /></label>
                    <label class="intake-source-field"><span>{{ t('knowledgeBase.intakePublishedAt') }}</span><t-input v-model="sourceInfo('manual').published_at" :placeholder="t('knowledgeBase.intakePublishedAt')" /></label>
                  </div>
                </div>
                <div v-else-if="mode === 'reparse' && reparsePreview" class="manual-source-panel">
                  <p class="manual-source-title" :title="reparsePreview.fileName">
                    {{ reparsePreview.fileName || t('uploadConfirm.reparseSource') }}
                  </p>
                  <p class="manual-source-meta">{{ t('uploadConfirm.reparseHint') }}</p>
                </div>
                <ul v-else-if="mode === 'file' && batchItemCount > 0" class="files-list">
                  <li v-for="(url, index) in localUrls" :key="`url-${url}-${index}`" class="file-item">
                    <span class="file-icon-wrap">
                      <t-icon name="link" class="file-icon" />
                    </span>
                    <div class="file-meta">
                      <span class="file-name" :title="url">{{ url }}</span>
                      <span class="file-size">{{ t('uploadConfirm.urlItemLabel') }}</span>
                    </div>
                    <button
                      type="button"
                      class="file-remove"
                      :aria-label="t('common.remove')"
                      @click="removeUrl(index)"
                    >
                      <t-icon name="close" />
                    </button>
                    <div class="intake-source-fields">
                      <label class="intake-source-field"><span>{{ t('knowledgeBase.intakeAuthor') }}</span><t-input v-model="sourceInfo(urlSourceKey(url)).author" :placeholder="t('knowledgeBase.intakeAuthor')" /></label>
                      <label class="intake-source-field"><span>{{ t('knowledgeBase.intakePublisher') }}</span><t-input v-model="sourceInfo(urlSourceKey(url)).publisher" :placeholder="t('knowledgeBase.intakePublisher')" /></label>
                      <label class="intake-source-field"><span>{{ t('knowledgeBase.intakeOriginalUrl') }}</span><SafeTextarea v-model="sourceInfo(urlSourceKey(url)).original_url" :placeholder="t('knowledgeBase.intakeOriginalUrl')" :autosize="{ minRows: 2, maxRows: 4 }" /></label>
                      <label class="intake-source-field"><span>{{ t('knowledgeBase.intakePublishedAt') }}</span><t-input v-model="sourceInfo(urlSourceKey(url)).published_at" :placeholder="t('knowledgeBase.intakePublishedAt')" /></label>
                    </div>
                  </li>
                  <li v-for="(file, index) in localFiles" :key="`${file.name}-${index}`" class="file-item">
                    <span class="file-icon-wrap">
                      <t-icon :name="getFileIcon(file.name)" class="file-icon" />
                    </span>
                    <div class="file-meta">
                      <span class="file-name" :title="fileDisplayTitle(file)">{{ file.name }}</span>
                      <span class="file-size">
                        <template v-if="fileRelativeDir(file)">
                          <span class="file-relative-dir" :title="fileRelativeDir(file)">{{ fileRelativeDir(file) }}</span>
                          <span class="file-meta-sep">·</span>
                        </template>
                        {{ formatFileSize(file.size) }}
                      </span>
                    </div>
                    <button
                      type="button"
                      class="file-remove"
                      :aria-label="t('common.remove')"
                      @click="removeFile(index)"
                    >
                      <t-icon name="close" />
                    </button>
                    <div class="intake-source-fields">
                      <label class="intake-source-field"><span>{{ t('knowledgeBase.intakeAuthor') }}</span><t-input v-model="sourceInfo(fileSourceKey(file)).author" :placeholder="t('knowledgeBase.intakeAuthor')" /></label>
                      <label class="intake-source-field"><span>{{ t('knowledgeBase.intakePublisher') }}</span><t-input v-model="sourceInfo(fileSourceKey(file)).publisher" :placeholder="t('knowledgeBase.intakePublisher')" /></label>
                      <label class="intake-source-field"><span>{{ t('knowledgeBase.intakeOriginalUrl') }}</span><SafeTextarea v-model="sourceInfo(fileSourceKey(file)).original_url" :placeholder="t('knowledgeBase.intakeOriginalUrl')" :autosize="{ minRows: 2, maxRows: 4 }" /></label>
                      <label class="intake-source-field"><span>{{ t('knowledgeBase.intakePublishedAt') }}</span><t-input v-model="sourceInfo(fileSourceKey(file)).published_at" :placeholder="t('knowledgeBase.intakePublishedAt')" /></label>
                    </div>
                  </li>
                </ul>
                <div v-else-if="mode === 'file'" class="files-empty">{{ t('uploadConfirm.noItems') }}</div>
              </div>
            </aside>

            <aside class="settings-sidebar">
              <div class="settings-sidebar-header">
                <h2 class="settings-sidebar-title">{{ t('uploadConfirm.parseConfig') }}</h2>
              </div>
              <nav class="settings-nav" :aria-label="t('uploadConfirm.configNav')">
                <button
                  v-for="item in navItems"
                  :key="item.key"
                  type="button"
                  class="nav-item"
                  :class="{
                    active: activeSection === item.key,
                    'nav-item--issue': item.issue,
                  }"
                  @click="activeSection = item.key"
                >
                  <t-icon :name="item.icon" class="nav-icon" />
                  <span class="nav-label-wrap">
                    <span class="nav-label">{{ item.label }}</span>
                    <span
                      class="nav-status"
                      :class="{
                        'nav-status--warning': item.statusTone === 'warning',
                        'nav-status--error': item.statusTone === 'error',
                        'nav-status--muted': item.statusTone === 'muted',
                      }"
                      :title="item.statusFull"
                    >{{ item.status }}</span>
                  </span>
                  <span v-if="item.issue" class="nav-dot" aria-hidden="true" />
                </button>
              </nav>
            </aside>

            <div class="config-panel">
              <div class="content-wrapper upload-confirm-content">

                  <div v-show="activeSection === 'parser'" class="section" data-section="parser">
                    <KBParserSettings
                      :relevant-extensions="batchFileExts"
                      :parser-engine-rules="uiState.chunkingConfig.parserEngineRules"
                      @update:parser-engine-rules="handleParserEngineRulesUpdate"
                    />
                    <div v-if="hasPdf" class="kb-settings-block">
                      <div class="settings-group">
                        <div class="setting-row">
                          <div class="setting-info">
                            <label>{{ t('uploadConfirm.pdfForceScanned.label') }}</label>
                            <p class="desc">{{ t('uploadConfirm.pdfForceScanned.description') }}</p>
                          </div>
                          <div class="setting-control">
                            <t-switch v-model="uiState.pdfForceScanned" size="medium" />
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>

                  <div v-show="activeSection === 'chunking'" class="section" data-section="chunking">
                    <div class="section-content">
                      <div class="section-header">
                        <h2 class="section-title">{{ t('knowledgeEditor.chunking.title') }}</h2>
                        <p class="section-desc">{{ t('knowledgeEditor.chunking.description') }}</p>
                      </div>
                      <div class="settings-group">
                        <div class="setting-row">
                          <div class="setting-info">
                            <label>{{ t('knowledgeEditor.chunking.strategyLabel') }}</label>
                            <p class="desc">{{ t('knowledgeEditor.chunking.strategyDescription') }}</p>
                          </div>
                          <div class="setting-control">
                            <t-select
                              v-model="uiState.chunkingConfig.strategy"
                              :options="chunkingStrategyOptions"
                              :clearable="false"
                              :style="{ width: '280px' }"
                            />
                          </div>
                        </div>
                        <div class="setting-row">
                          <div class="setting-info">
                            <label>{{ t('knowledgeEditor.chunking.sizeLabel') }}</label>
                            <p class="desc">{{ t('knowledgeEditor.chunking.sizeDescription') }}</p>
                          </div>
                          <div class="setting-control">
                            <t-input-number
                              v-model="uiState.chunkingConfig.chunkSize"
                              :min="100"
                              :max="4000"
                              :step="50"
                              theme="normal"
                              :style="{ width: '200px' }"
                            />
                          </div>
                        </div>
                        <div class="setting-row">
                          <div class="setting-info">
                            <label>{{ t('knowledgeEditor.chunking.overlapLabel') }}</label>
                            <p class="desc">{{ t('knowledgeEditor.chunking.overlapDescription') }}</p>
                          </div>
                          <div class="setting-control">
                            <t-input-number
                              v-model="uiState.chunkingConfig.chunkOverlap"
                              :min="0"
                              :max="500"
                              :step="20"
                              theme="normal"
                              :style="{ width: '200px' }"
                            />
                          </div>
                        </div>
                      </div>

                      <button
                        type="button"
                        class="more-options-toggle"
                        :aria-expanded="chunkingMoreOpen"
                        @click="chunkingMoreOpen = !chunkingMoreOpen"
                      >
                        <t-icon name="chevron-down" :class="{ 'is-open': chunkingMoreOpen }" />
                        <span>{{ t('uploadConfirm.moreOptions') }}</span>
                      </button>

                      <div v-if="chunkingMoreOpen" class="settings-group settings-group--more">
                        <div class="setting-row setting-row--separators">
                          <div class="setting-info">
                            <label>{{ t('knowledgeEditor.chunking.separatorsLabel') }}</label>
                            <p class="desc">{{ t('knowledgeEditor.chunking.separatorsDescription') }}</p>
                          </div>
                          <div class="setting-control">
                            <t-select
                              v-model="uiState.chunkingConfig.separators"
                              :options="separatorOptions"
                              multiple
                              creatable
                              filterable
                              :style="{ width: '280px' }"
                            />
                          </div>
                        </div>
                        <div class="setting-row">
                          <div class="setting-info">
                            <label>{{ t('knowledgeEditor.chunking.tokenLimitLabel') }}</label>
                          </div>
                          <div class="setting-control">
                            <t-input-number
                              v-model="uiState.chunkingConfig.tokenLimit"
                              :min="0"
                              :max="8192"
                              :step="64"
                              theme="normal"
                              :style="{ width: '200px' }"
                            />
                          </div>
                        </div>
                        <div class="setting-row">
                          <div class="setting-info">
                            <label>{{ t('knowledgeEditor.chunking.languagesLabel') }}</label>
                          </div>
                          <div class="setting-control">
                            <t-select
                              v-model="uiState.chunkingConfig.languages"
                              :options="languageOptions"
                              multiple
                              :style="{ width: '280px' }"
                            />
                          </div>
                        </div>
                        <div class="setting-row">
                          <div class="setting-info">
                            <label>{{ t('knowledgeEditor.chunking.parentChildLabel') }}</label>
                            <p class="desc">{{ t('knowledgeEditor.chunking.parentChildDescription') }}</p>
                          </div>
                          <div class="setting-control">
                            <t-switch v-model="uiState.chunkingConfig.enableParentChild" />
                          </div>
                        </div>
                        <div v-if="uiState.chunkingConfig.enableParentChild" class="setting-row">
                          <div class="setting-info">
                            <label>{{ t('knowledgeEditor.chunking.parentChunkSizeLabel') }}</label>
                          </div>
                          <div class="setting-control">
                            <t-input-number v-model="uiState.chunkingConfig.parentChunkSize" :min="512" :max="8192" :step="64" theme="normal" :style="{ width: '200px' }" />
                          </div>
                        </div>
                        <div v-if="uiState.chunkingConfig.enableParentChild" class="setting-row">
                          <div class="setting-info">
                            <label>{{ t('knowledgeEditor.chunking.childChunkSizeLabel') }}</label>
                          </div>
                          <div class="setting-control">
                            <t-input-number v-model="uiState.chunkingConfig.childChunkSize" :min="64" :max="2048" :step="32" theme="normal" :style="{ width: '200px' }" />
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>

                </div>

              <footer class="modal-footer">
                <t-button theme="default" variant="outline" @click="handleCancel">
                  {{ t('uploadConfirm.cancel') }}
                </t-button>
                <t-button theme="primary" :disabled="!canConfirm" @click="handleConfirm">
                  {{ confirmButtonText }}
                </t-button>
              </footer>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import SafeTextarea from '@/components/SafeTextarea.vue'
import { ref, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import KBParserSettings from '../settings/KBParserSettings.vue'
import { useEditorResourcesStore } from '@/stores/editorResources'
import { formatFileSize, getFileIcon } from '@/utils/files'
import { getUploadFileKey } from '../utils/uploadSources'
import KbUploadSourceDropdown from './KbUploadSourceDropdown.vue'
import type { KnowledgeProcessOverrides } from '@/types/knowledgeProcess'
import type {
  IntakeSourceInfo,
  UploadConfirmManualSource,
  UploadConfirmMode,
  UploadConfirmReparseSource,
  UploadConfirmResult,
} from '@/stores/uploadConfirm'


type ConfigSectionKey = 'parser' | 'chunking'

interface ChunkingUIConfig {
  chunkSize: number
  chunkOverlap: number
  separators: string[]
  parserEngineRules?: Array<{
    file_types: string[]
    engine: string
  }>
  enableParentChild: boolean
  parentChunkSize: number
  childChunkSize: number
  strategy?: string
  tokenLimit?: number
  languages?: string[]
  tableMetadataInstructions?: string
}

interface UploadUIState {
  chunkingConfig: ChunkingUIConfig
  pdfForceScanned: boolean
}

const props = withDefaults(defineProps<{
  visible: boolean
  kbInfo: any
  mode?: UploadConfirmMode
  files?: File[]
  urls?: string[]
  manualPreview?: UploadConfirmManualSource | null
  reparsePreview?: UploadConfirmReparseSource | null
  acceptFileTypes?: string
  supportedFileTypes?: string[]
}>(), {
  mode: 'file',
  files: () => [],
  urls: () => [],
  manualPreview: null,
  reparsePreview: null,
  acceptFileTypes: '',
  supportedFileTypes: () => [],
})

const emit = defineEmits<{
  'update:visible': [value: boolean]
  confirm: [payload: UploadConfirmResult]
  cancel: []
}>()

const { t } = useI18n()
const editorResources = useEditorResourcesStore()

const localFiles = ref<File[]>([])
const localUrls = ref<string[]>([])
const sourceInfoByItem = ref<Record<string, IntakeSourceInfo>>({})
const sourceInfo = (key: string) => sourceInfoByItem.value[key] ||= {}
const fileSourceKey = (file: File) => `file:${getUploadFileKey(file)}`
const urlSourceKey = (url: string) => `url:${url}`
const chunkingMoreOpen = ref(false)
const activeSection = ref<ConfigSectionKey>('parser')
const uiState = ref<UploadUIState>(createDefaultUIState())
const dialogVisible = computed({
  get: () => props.visible,
  set: (value: boolean) => emit('update:visible', value),
})

/**
 * Directory a folder-upload file came from, shown under its name so a batch of
 * same-named files (README.md in five folders) stays distinguishable and the
 * resulting structure is visible before confirming.
 */
function fileRelativeDir(file: File): string {
  const relativePath = (file as File & { webkitRelativePath?: string }).webkitRelativePath
  if (!relativePath) return ''
  return relativePath.split('/').filter(Boolean).slice(0, -1).join('/')
}

function fileDisplayTitle(file: File): string {
  const relativePath = (file as File & { webkitRelativePath?: string }).webkitRelativePath
  return relativePath || file.name
}

function truncateNavText(text: string, max = 18): string {
  if (!text) return text
  if (text.length <= max) return text
  return `${text.slice(0, max - 1)}…`
}

function hasParserCustomization(): boolean {
  const rules = uiState.value.chunkingConfig.parserEngineRules
  if (!rules?.length) return false
  return rules.some(rule => rule.engine)
}

function getFileExt(file: File): string {
  const dot = file.name.lastIndexOf('.')
  if (dot < 0) return ''
  return file.name.substring(dot + 1).toLowerCase()
}

function getExtFromUrl(url: string): string {
  try {
    const pathname = new URL(url).pathname
    const dot = pathname.lastIndexOf('.')
    if (dot < 0) return ''
    return pathname.substring(dot + 1).toLowerCase()
  } catch {
    return ''
  }
}

function inferMediaExtsFromMarkdown(content: string): string[] {
  const exts = new Set<string>()
  const patterns = [
    /\.(jpg|jpeg|png|gif|bmp|webp)(\?|#|\)|\s|$)/gi,
    /\.(mp3|wav|m4a|flac|ogg)(\?|#|\)|\s|$)/gi,
  ]
  for (const pattern of patterns) {
    let match: RegExpExecArray | null
    while ((match = pattern.exec(content)) !== null) {
      let ext = match[1].toLowerCase()
      if (ext === 'jpeg') ext = 'jpg'
      exts.add(ext)
    }
  }
  return [...exts]
}

const manualCharCount = computed(() => props.manualPreview?.content?.length ?? 0)
const batchItemCount = computed(() => localFiles.value.length + localUrls.value.length)

const dialogTitle = computed(() => {
  if (props.mode === 'manual') return t('uploadConfirm.titleManual')
  if (props.mode === 'reparse') return t('uploadConfirm.titleReparse')
  return t('uploadConfirm.title')
})

const confirmButtonText = computed(() => {
  if (props.mode === 'manual') return t('uploadConfirm.confirmManual')
  if (props.mode === 'reparse') return t('uploadConfirm.confirmReparse')
  return t('uploadConfirm.confirm')
})

const batchFileExts = computed(() => {
  const set = new Set<string>()
  if (props.mode === 'manual' && props.manualPreview?.content) {
    for (const ext of inferMediaExtsFromMarkdown(props.manualPreview.content)) {
      set.add(ext)
    }
  }
  if (props.mode === 'reparse') {
    const ext = (props.reparsePreview?.fileType || '').toLowerCase()
    if (ext) set.add(ext)
  }
  for (const url of localUrls.value) {
    const ext = getExtFromUrl(url)
    if (ext) set.add(ext)
  }
  for (const file of localFiles.value) {
    const ext = getFileExt(file)
    if (ext) set.add(ext)
  }
  return [...set]
})

const hasPdf = computed(() => batchFileExts.value.includes('pdf'))

const chunkingStrategyOptions = computed(() => [
  { label: t('knowledgeEditor.chunking.strategies.auto.label'), value: 'auto' },
  { label: t('knowledgeEditor.chunking.strategies.heading.label'), value: 'heading' },
  { label: t('knowledgeEditor.chunking.strategies.heuristic.label'), value: 'heuristic' },
  { label: t('knowledgeEditor.chunking.strategies.legacy.label'), value: 'legacy' },
])

const separatorOptions = computed(() => [
  { label: t('knowledgeEditor.chunking.separators.doubleNewline'), value: '\n\n' },
  { label: t('knowledgeEditor.chunking.separators.singleNewline'), value: '\n' },
  { label: t('knowledgeEditor.chunking.separators.periodCn'), value: '。' },
  { label: t('knowledgeEditor.chunking.separators.exclamationCn'), value: '！' },
  { label: t('knowledgeEditor.chunking.separators.questionCn'), value: '？' },
  { label: t('knowledgeEditor.chunking.separators.semicolonCn'), value: '；' },
  { label: t('knowledgeEditor.chunking.separators.semicolonEn'), value: ';' },
  { label: t('knowledgeEditor.chunking.separators.space'), value: ' ' },
])

const languageOptions = computed(() => [
  { label: t('knowledgeEditor.chunking.languageOptions.de'), value: 'de' },
  { label: t('knowledgeEditor.chunking.languageOptions.en'), value: 'en' },
  { label: t('knowledgeEditor.chunking.languageOptions.zh'), value: 'zh' },
])

const navItems = computed(() => {
  const items: Array<{
    key: ConfigSectionKey
    icon: string
    label: string
    status: string
    statusFull: string
    statusTone?: 'warning' | 'error' | 'muted'
    issue?: boolean
  }> = []

  const push = (key: ConfigSectionKey, icon: string, label: string, issue?: boolean) => {
    const statusMeta = getSectionNavStatus(key, issue)
    const full = statusMeta.status
    items.push({
      key,
      icon,
      label,
      status: truncateNavText(full),
      statusFull: full,
      statusTone: statusMeta.statusTone,
      issue,
    })
  }

  push('parser', 'file-search', t('settings.parserEngine'))
  push('chunking', 'file-copy', t('knowledgeEditor.sidebar.chunking'))
  return items
})

function getSectionNavStatus(
  key: ConfigSectionKey,
  issue?: boolean,
): { status: string; statusTone?: 'warning' | 'error' | 'muted' } {
  switch (key) {
    case 'parser':
      if (uiState.value.pdfForceScanned && hasPdf.value) {
        return { status: t('uploadConfirm.summaryParserForceScanned') }
      }
      if (hasParserCustomization()) {
        return { status: t('uploadConfirm.navParserCustomized') }
      }
      return { status: t('uploadConfirm.navParserDefault'), statusTone: 'muted' }
    case 'chunking': {
      const chunking = uiState.value.chunkingConfig
      const parts = [t('uploadConfirm.navChunkingSummary', { size: chunking.chunkSize })]
      if (chunking.enableParentChild) {
        parts.push(t('uploadConfirm.summaryParentChildShort'))
      }
      return { status: parts.join(' · ') }
    }
    default:
      return { status: '' }
  }
}

const canConfirm = computed(() => {
  if (props.mode === 'file' && batchItemCount.value === 0) return false
  if (props.mode === 'manual' && !props.manualPreview?.content?.trim()) return false
  return true
})

function getDefaultSection(): ConfigSectionKey {
  if (props.mode === 'reparse') return 'parser'
  return 'parser'
}

function goToSection(key: ConfigSectionKey) {
  activeSection.value = key
  nextTick(() => {
    document.querySelector(`[data-section="${key}"]`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  })
}

function createDefaultUIState(): UploadUIState {
  return {
    chunkingConfig: {
      chunkSize: 512,
      chunkOverlap: 80,
      separators: ['\n\n', '\n', '。', '！', '？', ';', '；'],
      parserEngineRules: undefined,
      enableParentChild: true,
      parentChunkSize: 4096,
      childChunkSize: 384,
      strategy: 'auto',
      tokenLimit: 0,
      languages: [],
      tableMetadataInstructions: '',
    },
        pdfForceScanned: false,
  }
}

function initFromKbInfo(kb: any) {
  if (!kb) {
    uiState.value = createDefaultUIState()
    return
  }

  uiState.value = {
    chunkingConfig: {
      chunkSize: kb.chunking_config?.chunk_size || 512,
      chunkOverlap: kb.chunking_config?.chunk_overlap ?? 80,
      separators: kb.chunking_config?.separators || ['\n\n', '\n', '。', '！', '？', ';', '；'],
      parserEngineRules: kb.chunking_config?.parser_engine_rules || undefined,
      enableParentChild: kb.chunking_config?.enable_parent_child ?? false,
      parentChunkSize: kb.chunking_config?.parent_chunk_size || 4096,
      childChunkSize: kb.chunking_config?.child_chunk_size || 384,
      strategy: kb.chunking_config?.strategy || 'auto',
      tokenLimit: kb.chunking_config?.token_limit || 0,
      languages: kb.chunking_config?.languages || [],
      tableMetadataInstructions: kb.chunking_config?.table_metadata_instructions || '',
    },
    pdfForceScanned: false,
  }
}

function buildProcessOverrides(): KnowledgeProcessOverrides {
  const state = uiState.value
  const chunking = state.chunkingConfig

  const overrides: KnowledgeProcessOverrides = {
    parser_engine_rules: chunking.parserEngineRules,
    chunking_config: {
      chunk_size: chunking.chunkSize,
      chunk_overlap: chunking.chunkOverlap,
      separators: chunking.separators,
      enable_parent_child: chunking.enableParentChild,
      parent_chunk_size: chunking.parentChunkSize,
      child_chunk_size: chunking.childChunkSize,
      strategy: chunking.strategy,
      token_limit: chunking.tokenLimit,
      languages: chunking.languages,
      table_metadata_instructions: chunking.tableMetadataInstructions,
    },
  }

  if (state.pdfForceScanned) {
    overrides.parser_engine_overrides = {
      pdf_force_scanned: 'true',
    }
  }

  return overrides
}

function applyOverridesToState(o?: KnowledgeProcessOverrides | null) {
  if (!o) return
  const s = uiState.value
  const cc = o.chunking_config
  if (cc) {
    if (cc.chunk_size != null) s.chunkingConfig.chunkSize = cc.chunk_size
    if (cc.chunk_overlap != null) s.chunkingConfig.chunkOverlap = cc.chunk_overlap
    if (cc.separators) s.chunkingConfig.separators = cc.separators
    if (cc.enable_parent_child != null) s.chunkingConfig.enableParentChild = cc.enable_parent_child
    if (cc.parent_chunk_size != null) s.chunkingConfig.parentChunkSize = cc.parent_chunk_size
    if (cc.child_chunk_size != null) s.chunkingConfig.childChunkSize = cc.child_chunk_size
    if (cc.strategy != null) s.chunkingConfig.strategy = cc.strategy
    if (cc.token_limit != null) s.chunkingConfig.tokenLimit = cc.token_limit
    if (cc.languages) s.chunkingConfig.languages = cc.languages
    if (cc.table_metadata_instructions != null) s.chunkingConfig.tableMetadataInstructions = cc.table_metadata_instructions
    if (cc.parser_engine_rules) s.chunkingConfig.parserEngineRules = cc.parser_engine_rules
  }
  if (o.parser_engine_rules) s.chunkingConfig.parserEngineRules = o.parser_engine_rules
  if (o.parser_engine_overrides && o.parser_engine_overrides.pdf_force_scanned === 'true') {
    s.pdfForceScanned = true
  } else {
    s.pdfForceScanned = false
  }
}

watch(
  () => props.visible,
  (visible) => {
    if (!visible) return
    localFiles.value = props.mode === 'file' ? [...(props.files || [])] : []
    localUrls.value = props.mode === 'file' ? [...(props.urls || [])] : []
    sourceInfoByItem.value = {}
    if (props.mode === 'manual' && props.manualPreview?.sourceInfo) {
      sourceInfoByItem.value.manual = { ...props.manualPreview.sourceInfo }
    }
    initFromKbInfo(props.kbInfo)
    if (props.mode === 'reparse') {
      applyOverridesToState(props.reparsePreview?.processOverrides)
    }
    activeSection.value = getDefaultSection()
    chunkingMoreOpen.value = false
  },
)

const appendFiles = (incoming: File[]) => {
  const existingKeys = new Set(localFiles.value.map(getUploadFileKey))
  const toAdd: File[] = []
  let duplicateCount = 0

  for (const file of incoming) {
    const key = getUploadFileKey(file)
    if (existingKeys.has(key)) {
      duplicateCount++
      continue
    }
    existingKeys.add(key)
    toAdd.push(file)
  }

  if (toAdd.length > 0) {
    localFiles.value = [...localFiles.value, ...toAdd]
    MessagePlugin.success(t('uploadConfirm.filesAdded', { count: toAdd.length }))
  } else if (duplicateCount > 0) {
    MessagePlugin.warning(t('uploadConfirm.filesAllDuplicate'))
  }
}

const appendUrl = (url: string) => {
  if (localUrls.value.includes(url)) {
    MessagePlugin.warning(t('uploadConfirm.urlDuplicate'))
    return
  }
  localUrls.value = [...localUrls.value, url]
  MessagePlugin.success(t('uploadConfirm.urlAdded'))
}

const removeUrl = (index: number) => {
  localUrls.value = localUrls.value.filter((_, i) => i !== index)
}

const removeFile = (index: number) => {
  localFiles.value = localFiles.value.filter((_, i) => i !== index)
}

const handleParserEngineRulesUpdate = (rules: Array<{
  file_types: string[]
  engine: string
}>) => {
  uiState.value.chunkingConfig.parserEngineRules = rules
}

const handleCancel = () => {
  emit('cancel')
  emit('update:visible', false)
}

const handleConfirm = () => {
  if (props.mode === 'file' && batchItemCount.value === 0) {
    MessagePlugin.warning(t('uploadConfirm.noItems'))
    return
  }

  const processConfig = buildProcessOverrides()
  if (props.mode === 'manual' && props.manualPreview) {
    emit('confirm', {
      processConfig,
      mode: 'manual',
      manual: { ...props.manualPreview, sourceInfo: sourceInfo('manual') },
    })
  } else if (props.mode === 'reparse' && props.reparsePreview) {
    emit('confirm', { processConfig, mode: 'reparse', reparse: { ...props.reparsePreview } })
  } else {
    emit('confirm', {
      processConfig,
      mode: 'file',
      files: [...localFiles.value],
      urls: [...localUrls.value],
      sourceInfoByItem: sourceInfoByItem.value,
    })
  }
  emit('update:visible', false)
}
</script>

<style lang="less" scoped>
.upload-confirm-overlay {
  position: fixed;
  inset: 0;
  z-index: 3000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(4px);
}

.upload-confirm-modal {
  position: relative;
  display: flex;
  flex-direction: column;
  width: 92vw;
  max-width: 1160px;
  height: 85vh;
  max-height: 750px;
  overflow: hidden;
  border-radius: 12px;
  background: var(--td-bg-color-container);
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.12);
}

.close-btn {
  position: absolute;
  top: 20px;
  right: 20px;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: none;
  border-radius: 6px;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);
  cursor: pointer;

  &:hover {
    color: var(--td-text-color-primary);
  }
}

.upload-confirm-container {
  display: flex;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.files-panel {
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  width: clamp(280px, 28vw, 360px);
  background: var(--td-bg-color-settings-modal, var(--td-bg-color-secondarycontainer));
  border-right: 1px solid var(--td-component-stroke);
}

.sidebar-header,
.settings-sidebar-header {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  box-sizing: border-box;
  min-height: 56px;
  padding: 12px;
  border-bottom: 1px solid var(--td-component-stroke);
}

.sidebar-header {
  flex-direction: column;
  align-items: stretch;
  gap: 10px;
}

.sidebar-header-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  min-width: 0;
}

.sidebar-title {
  margin: 0;
  flex: 1;
  min-width: 0;
  padding-right: 0;
  font-size: 16px;
  font-weight: 600;
  line-height: 1.35;
  color: var(--td-text-color-primary);
}

.sidebar-header-actions {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 6px;
}

.files-count {
  flex-shrink: 0;
  min-width: 20px;
  height: 20px;
  padding: 0 6px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 600;
  line-height: 20px;
  text-align: center;
  background: var(--td-bg-color-component);
  color: var(--td-text-color-secondary);
}

// Destination sits under the title inside the header block.

.files-list-wrap {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  padding: 6px 8px 12px;
  overflow: hidden;
}

.files-list {
  flex: 1;
  margin: 0;
  padding: 0;
  overflow-y: auto;
  list-style: none;
}

.file-item {
  flex-wrap: wrap;
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 2px;
  padding: 6px 6px 6px 8px;
  border-radius: 6px;
  transition: background-color 0.15s ease;

  &:last-child {
    margin-bottom: 0;
  }

  &:hover {
    background: var(--td-bg-color-container-hover);
  }
}

.intake-source-fields {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  box-sizing: border-box;
  width: 100%;
  gap: 6px;
  padding-left: 32px;
}

.intake-source-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  span { white-space: normal; overflow-wrap: anywhere; }
}

.file-icon-wrap {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
}

.file-icon {
  font-size: 16px;
  color: var(--td-text-color-secondary);
}

.file-item:hover .file-icon {
  color: var(--td-brand-color);
}

.file-meta {
  flex: 1;
  min-width: 0;
}

.file-name {
  display: block;
  overflow: hidden;
  font-size: 12px;
  font-weight: 500;
  line-height: 1.35;
  color: var(--td-text-color-primary);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-size {
  display: block;
  margin-top: 1px;
  font-size: 11px;
  line-height: 1.3;
  color: var(--td-text-color-placeholder);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-relative-dir {
  color: var(--td-text-color-secondary);
}

.file-meta-sep {
  margin: 0 4px;
  color: var(--td-text-color-placeholder);
}

.file-remove {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--td-text-color-placeholder);
  cursor: pointer;
  transition: opacity 0.15s ease, color 0.15s ease, background-color 0.15s ease;
  opacity: 0.45;

  .file-item:hover &,
  &:focus-visible {
    opacity: 1;
  }

  &:hover {
    color: var(--td-text-color-primary);
    background: var(--td-bg-color-component);
  }
}

.files-empty {
  flex: 1;
  padding: 16px 8px;
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  text-align: center;
}

.manual-source-panel {
  flex: 1;
  min-height: 0;
  padding: 0;
  overflow-y: auto;
}

.manual-source-title {
  margin: 0;
  padding: 8px 10px;
  border-radius: 6px;
  background: var(--td-bg-color-container-hover);
  font-size: 13px;
  font-weight: 500;
  line-height: 1.4;
  color: var(--td-text-color-primary);
  word-break: break-word;
}

.manual-source-meta {
  margin: 6px 2px 0;
  font-size: 11px;
  color: var(--td-text-color-placeholder);
}

.config-panel {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
  overflow: hidden;
}

.settings-sidebar {
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  width: 216px;
  min-height: 0;
  background-color: var(--td-bg-color-settings-modal, var(--td-bg-color-secondarycontainer));
  border-right: 1px solid var(--td-component-stroke);
}

.settings-sidebar-header {
  width: 100%;
}

.settings-sidebar-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  line-height: 1.35;
  color: var(--td-text-color-primary);
}

.settings-nav {
  flex: 1;
  padding: 10px 6px 12px;
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
  align-items: flex-start;
  width: 100%;
  margin-bottom: 2px;
  padding: 9px 10px;
  border: none;
  border-radius: 6px;
  background: transparent;
  font-size: 14px;
  color: var(--td-text-color-primary);
  text-align: left;
  cursor: pointer;
  transition: all 0.2s ease;
  user-select: none;

  &:hover {
    background-color: var(--td-bg-color-container-hover);
    color: var(--td-text-color-primary);
  }

  &.active {
    background-color: var(--td-bg-color-secondarycontainer);
    color: var(--td-brand-color);
    font-weight: 500;

    .nav-label {
      color: var(--td-brand-color);
    }

    .nav-status {
      color: var(--td-text-color-secondary);
    }
  }

  &--issue .nav-label {
    color: var(--td-error-color);
  }
}

.nav-icon {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  margin-right: 8px;
  margin-top: 2px;
  font-size: 16px;
  color: inherit;
}

.nav-label-wrap {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.nav-label {
  overflow: hidden;
  font-size: 13px;
  font-weight: 500;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.nav-status {
  overflow: hidden;
  font-size: 12px;
  line-height: 1.35;
  color: var(--td-text-color-placeholder);
  text-overflow: ellipsis;
  white-space: nowrap;

  &--muted {
    color: var(--td-text-color-placeholder);
  }

  &--warning {
    color: var(--td-warning-color);
  }

  &--error {
    color: var(--td-error-color);
  }
}

.nav-dot {
  flex-shrink: 0;
  width: 6px;
  height: 6px;
  margin-left: 6px;
  border-radius: 50%;
  background: var(--td-error-color);
}

.content-wrapper {
  flex: 1;
  min-width: 0;
  min-height: 0;
  padding: 22px 32px 28px;
  overflow-y: auto;
  background: var(--td-bg-color-container);
}

.upload-confirm-content {
  .setting-info {
    flex: 0 0 40%;
    max-width: 40%;
  }

  .setting-control {
    flex: 1 1 56%;
    max-width: 56%;
    min-width: 280px;
  }
}

.section-notice {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-bottom: 16px;
  padding: 10px 12px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-secondarycontainer);
  font-size: 13px;
  line-height: 1.5;
  color: var(--td-text-color-secondary);

  .t-icon {
    flex-shrink: 0;
    margin-top: 1px;
    font-size: 16px;
    color: var(--td-brand-color);
  }
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
    margin: 0 0 6px;
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
  }

  .section-desc {
    margin: 0;
    font-size: 14px;
    line-height: 22px;
    color: var(--td-text-color-placeholder);
  }
}

.kb-settings-block {
  width: 100%;

  .section-header {
    margin-bottom: 20px;
  }

  .section-title {
    margin: 0 0 6px;
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
  }

  .section-desc {
    margin: 0;
    font-size: 14px;
    line-height: 1.5;
    color: var(--td-text-color-secondary);
  }
}

.settings-group {
  display: flex;
  flex-direction: column;
  gap: 0;

  &--more {
    margin-top: 4px;
  }
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
  flex: 0 0 40%;
  max-width: 40%;
  padding-right: 24px;

  label {
    display: block;
    margin-bottom: 4px;
    font-size: 15px;
    font-weight: 500;
    color: var(--td-text-color-primary);
  }

  .desc {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--td-text-color-secondary);
  }

  .warn {
    margin: 4px 0 0;
    font-size: 12px;
    line-height: 1.4;
    color: var(--td-warning-color);
  }
}

.setting-control {
  display: flex;
  flex: 0 0 55%;
  max-width: 55%;
  align-items: center;
  justify-content: flex-end;

  &-full {
    flex: none;
    width: 100%;
    max-width: none;
    justify-content: flex-start;
  }

  &-inline {
    gap: 12px;
    justify-content: flex-end;
  }
}

.setting-row-vertical {
  flex-direction: column;
  gap: 12px;

  .setting-info,
  .setting-control {
    flex: none;
    width: 100%;
    max-width: none;
    padding-right: 0;
  }

  .setting-control {
    display: block;
  }
}

.required {
  margin-left: 2px;
  font-weight: 500;
  color: var(--td-error-color);
}

.field-hint {
  margin: 6px 0 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--td-text-color-placeholder);

  &--error {
    color: var(--td-error-color);
  }
}

.more-options-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-top: 4px;
  padding: 6px 0;
  border: none;
  background: transparent;
  color: var(--td-brand-color);
  font-size: 13px;
  cursor: pointer;

  .t-icon {
    transition: transform 0.18s ease;

    &.is-open {
      transform: rotate(180deg);
    }
  }
}

:deep(.t-input-number) {
  width: 100%;
}

.modal-footer {
  display: flex;
  flex-shrink: 0;
  justify-content: flex-end;
  gap: 12px;
  padding: 14px 20px;
  border-top: 1px solid var(--td-component-stroke);
  background: var(--td-bg-color-container);
}

@media (max-width: 800px) {
  .upload-confirm-container {
    flex-direction: column;
  }

  .files-panel {
    width: auto;
    max-height: 140px;
    border-right: none;
    border-bottom: 1px solid var(--td-component-stroke);
  }

  .settings-sidebar {
    width: auto;
    border-right: none;
    border-bottom: 1px solid var(--td-component-stroke);
  }

  .settings-nav {
    display: flex;
    flex-wrap: nowrap;
    gap: 4px;
    flex: none;
    padding: 8px;
    overflow-x: auto;
  }

  .nav-group-title {
    display: none;
  }

  .nav-item {
    flex: 0 0 auto;
    width: auto;
    margin-bottom: 0;
    white-space: nowrap;
  }

  .content-wrapper {
    padding: 16px;
  }

  .setting-info,
  .setting-control {
    flex: 1 1 100%;
    max-width: none;
  }

  .setting-row {
    flex-direction: column;
    gap: 12px;
  }
}

.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.2s ease;
}

.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
</style>

<style lang="less">
// Must sit above the upload modal (z-index 3000). Do not reuse card-more-popup here —
// its global z-index: 99 !important would hide the menu behind the modal overlay.

</style>
