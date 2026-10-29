<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui'
import type { EntryBatchAction } from '@/api/entry-batches'
defineProps<{ count: number }>()
const emit = defineEmits<{ cancel: []; manage: []; action: [action: EntryBatchAction] }>()
const { t } = useI18n()
</script>
<template>
  <div class="entries-batch" role="toolbar" :aria-label="t('library.selectedCount', { count })">
    <strong>{{ t('library.selectedCount', { count }) }}</strong>
    <Button @click="emit('action', 'reprocess')">{{ t('library.reprocess') }}</Button>
    <Button @click="emit('action', 'content_type')">{{ t('entryBatch.actions.content_type') }}</Button>
    <Button @click="emit('manage')">{{ t('entryBatch.title') }}</Button>
    <Button variant="danger" @click="emit('action', 'delete')">{{ t('entryBatch.actions.delete') }}</Button>
    <Button @click="emit('cancel')">{{ t('library.clearSelection') }}</Button>
  </div>
</template>
<style scoped>
.entries-batch { position: sticky; bottom: 0; display: flex; flex-wrap: wrap; align-items: center; gap: 12px; padding: 12px 24px; background: var(--text); color: var(--bg); }
.entries-batch :deep(.rq-button) { background: transparent; color: var(--bg); border-color: var(--bg); }
.entries-batch :deep(.rq-button--danger) { color: var(--accent); border-color: var(--accent); }
</style>
