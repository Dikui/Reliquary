<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="dialogVisible" class="reparse-confirm-overlay">
        <div class="reparse-confirm-modal" role="dialog" :aria-label="dialogTitle">
          <button class="close-btn" type="button" :aria-label="t('general.close')" @click="handleCancel">
            <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path d="M15 5L5 15M5 5L15 15" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
            </svg>
          </button>

          <div class="reparse-confirm-container">
            <aside class="source-panel">
              <div class="sidebar-header">
                <h2 class="sidebar-title">{{ dialogTitle }}</h2>
              </div>
              <div class="source-description">
                <p class="source-title" :title="reparsePreview?.fileName">{{ reparsePreview?.fileName || t('reparseConfirm.reparseSource') }}</p>
                <p class="source-hint">{{ t('reparseConfirm.reparseHint') }}</p>
              </div>
            </aside>

            <aside class="settings-sidebar">
              <div class="settings-sidebar-header">
                <h2 class="settings-sidebar-title">{{ t('reparseConfirm.parseConfig') }}</h2>
              </div>
              <nav class="settings-nav" :aria-label="t('reparseConfirm.configNav')">
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
              <div class="content-wrapper reparse-confirm-content">

                  <div v-show="activeSection === 'chunking'" class="section" data-section="chunking">
                    <LibraryChunkingSettings v-model:config="uiState.chunkingConfig" embedded />
                  </div>

                </div>

              <footer class="modal-footer">
                <t-button theme="default" variant="outline" @click="handleCancel">
                  {{ t('reparseConfirm.cancel') }}
                </t-button>
                <t-button theme="primary" @click="handleConfirm">
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
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import LibraryChunkingSettings from '../settings/LibraryChunkingSettings.vue'
import type { EntryProcessOverrides } from '@/types/entryProcess'
import type { ReparseConfirmSource, ReparseConfirmResult } from '@/stores/reparseConfirm'


type ConfigSectionKey = 'chunking'

interface ChunkingUIConfig {
  chunkSize: number
  chunkOverlap: number
  separators: string[]
  enableParentChild: boolean
  parentChunkSize: number
  childChunkSize: number
  strategy?: string
  tokenLimit?: number
  languages?: string[]
}

interface ReparseUIState {
  chunkingConfig: ChunkingUIConfig
}

const props = defineProps<{
  visible: boolean
  libraryInfo: any
  reparsePreview?: ReparseConfirmSource | null
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
  confirm: [payload: ReparseConfirmResult]
  cancel: []
}>()

const { t } = useI18n()

const activeSection = ref<ConfigSectionKey>('chunking')
const uiState = ref<ReparseUIState>(createDefaultUIState())
const dialogVisible = computed({
  get: () => props.visible,
  set: (value: boolean) => emit('update:visible', value),
})

function truncateNavText(text: string, max = 18): string {
  if (!text) return text
  if (text.length <= max) return text
  return `${text.slice(0, max - 1)}…`
}

const dialogTitle = computed(() => t('reparseConfirm.titleReparse'))
const confirmButtonText = computed(() => t('reparseConfirm.confirmReparse'))
const batchFileExts = computed(() => {
  const ext = (props.reparsePreview?.fileType || '').toLowerCase()
  return ext ? [ext] : []
})

const hasPdf = computed(() => batchFileExts.value.includes('pdf'))

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

  push('chunking', 'file-copy', t('reparseConfirm.chunking'))
  return items
})

function getSectionNavStatus(
  key: ConfigSectionKey,
  issue?: boolean,
): { status: string; statusTone?: 'warning' | 'error' | 'muted' } {
  switch (key) {
    case 'chunking': {
      const chunking = uiState.value.chunkingConfig
      const parts = [t('reparseConfirm.navChunkingSummary', { size: chunking.chunkSize })]
      if (chunking.enableParentChild) {
        parts.push(t('reparseConfirm.summaryParentChildShort'))
      }
      return { status: parts.join(' · ') }
    }
    default:
      return { status: '' }
  }
}

function createDefaultUIState(): ReparseUIState {
  return {
    chunkingConfig: {
      chunkSize: 512,
      chunkOverlap: 80,
      separators: ['\n\n', '\n', '。', '！', '？', ';', '；'],
      enableParentChild: true,
      parentChunkSize: 4096,
      childChunkSize: 384,
      strategy: 'auto',
      tokenLimit: 0,
      languages: [],
    },
  }
}

function initFromLibraryInfo(library: any) {
  if (!library) {
    uiState.value = createDefaultUIState()
    return
  }

  uiState.value = {
    chunkingConfig: {
      chunkSize: library.chunking_config?.chunk_size || 512,
      chunkOverlap: library.chunking_config?.chunk_overlap ?? 80,
      separators: library.chunking_config?.separators || ['\n\n', '\n', '。', '！', '？', ';', '；'],
      enableParentChild: library.chunking_config?.enable_parent_child ?? false,
      parentChunkSize: library.chunking_config?.parent_chunk_size || 4096,
      childChunkSize: library.chunking_config?.child_chunk_size || 384,
      strategy: library.chunking_config?.strategy || 'auto',
      tokenLimit: library.chunking_config?.token_limit || 0,
      languages: library.chunking_config?.languages || [],
    },
  }
}

function buildProcessOverrides(): EntryProcessOverrides {
  const state = uiState.value
  const chunking = state.chunkingConfig

  const overrides: EntryProcessOverrides = {
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
    },
  }

  return overrides
}

function applyOverridesToState(o?: EntryProcessOverrides | null) {
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
  }

}

watch(
  () => props.visible,
  (visible) => {
    if (!visible) return
    initFromLibraryInfo(props.libraryInfo)
    applyOverridesToState(props.reparsePreview?.processOverrides)
    activeSection.value = 'chunking'
  },
)

const handleCancel = () => {
  emit('cancel')
  emit('update:visible', false)
}

const handleConfirm = () => {
  if (!props.reparsePreview) return
  emit('confirm', { processConfig: buildProcessOverrides(), reparse: { ...props.reparsePreview } })
  emit('update:visible', false)
}
</script>

<style lang="less" scoped>
.reparse-confirm-overlay {
  position: fixed;
  inset: 0;
  z-index: 3000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(4px);
}

.reparse-confirm-modal {
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

.reparse-confirm-container {
  display: flex;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.source-panel {
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

 .source-title { margin: 0 0 8px; overflow-wrap: anywhere; color: var(--td-text-color-primary); }
.source-hint { margin: 0; overflow-wrap: anywhere; color: var(--td-text-color-secondary); }
.source-description {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  padding: 12px 16px;
  overflow: hidden;
}

.config-panel {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
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

.reparse-confirm-content {
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

.library-settings-block {
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
  .reparse-confirm-container {
    flex-direction: column;
  }

  .source-panel {
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
