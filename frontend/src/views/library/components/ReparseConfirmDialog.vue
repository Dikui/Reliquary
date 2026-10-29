<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Button, Dialog } from '@/components/ui'
import { useI18n } from 'vue-i18n'
import LibraryChunkingSettings from '../settings/LibraryChunkingSettings.vue'
import type { EntryProcessOverrides } from '@/types/entryProcess'
import type { ReparseConfirmSource, ReparseConfirmResult } from '@/stores/reparseConfirm'



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

const uiState = ref<ReparseUIState>(createDefaultUIState())
const dialogVisible = computed({
  get: () => props.visible,
  set: (value: boolean) => emit('update:visible', value),
})

const dialogTitle = computed(() => t(props.reparsePreview?.sourceKind === 'report' ? 'ikb18.reindex' : 'reparseConfirm.titleReparse'))
const confirmButtonText = computed(() => t('reparseConfirm.confirmReparse'))
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
<template>
  <Dialog :model-value="dialogVisible" :title="dialogTitle" content-class="reparse-dialog" :layer="3000" @update:model-value="!$event && handleCancel()">
    <p class="source-title">{{ reparsePreview?.fileName || t('reparseConfirm.reparseSource') }}</p>
    <p>{{ t(reparsePreview?.sourceKind === 'report' ? 'ikb18.reindexHint' : 'reparseConfirm.reparseHint') }}</p>
    <LibraryChunkingSettings v-if="reparsePreview?.sourceKind !== 'report'" v-model:config="uiState.chunkingConfig" />
    <template #footer>
      <Button @click="handleCancel">{{ t('reparseConfirm.cancel') }}</Button>
      <Button variant="primary" @click="handleConfirm">{{ confirmButtonText }}</Button>
    </template>
  </Dialog>
</template>
<style scoped>
.source-title { font-weight: 700; overflow-wrap: anywhere; }
</style>
<style>
.reparse-dialog { width: min(880px, calc(100vw - 32px)); }
</style>
