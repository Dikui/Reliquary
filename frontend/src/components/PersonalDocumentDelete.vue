<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { deleteDocument } from '@/api/knowledge-base'
import DocumentDeleteConfirm from '@/components/DocumentDeleteConfirm.vue'

const props = defineProps<{ documentId: string; title: string; disabled?: boolean }>()
const emit = defineEmits<{ deleted: [id: string]; busyChange: [busy: boolean] }>()
const { t } = useI18n()
async function remove() {
  const result = await deleteDocument(props.documentId)
  if (!result.success) throw new Error(result.message || t('personalDocument.deleteFailed'))
}
function deleted(id: string) {
  MessagePlugin.success(t('personalDocument.deleted'))
  emit('deleted', id)
}
</script>

<template>
  <DocumentDeleteConfirm :document-id="documentId" :label="t('personalDocument.delete')"
    :confirmation="t('personalDocument.deleteConfirm', { title })" :failure-message="t('personalDocument.deleteFailed')"
    :disabled="disabled" :delete-action="remove" @deleted="deleted" @busy-change="emit('busyChange', $event)">
    <template v-if="$slots.trigger" #trigger="slotProps"><slot name="trigger" v-bind="slotProps" /></template>
  </DocumentDeleteConfirm>
</template>
