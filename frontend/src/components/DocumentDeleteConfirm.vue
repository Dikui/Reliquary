<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  documentId: string
  label: string
  confirmation: string
  failureMessage: string
  disabled?: boolean
  hideTrigger?: boolean
  deleteAction: () => Promise<void>
}>()
const emit = defineEmits<{ deleted: [id: string]; busyChange: [busy: boolean] }>()
const { t } = useI18n()
const visible = ref(false), busy = ref(false), error = ref('')
let generation = 0
watch(busy, value => emit('busyChange', value), { flush: 'sync' })
watch(() => props.documentId, () => { generation++; visible.value = false; busy.value = false; error.value = '' })
onUnmounted(() => { generation++; emit('busyChange', false) })
function open() {
  if (props.disabled || busy.value || !props.documentId) return
  error.value = ''; visible.value = true
}
function close() { if (!busy.value) visible.value = false }
defineExpose({ open })
async function confirm() {
  if (busy.value || props.disabled || !visible.value) return
  const id = props.documentId, current = generation
  busy.value = true; error.value = ''
  try {
    await props.deleteAction()
    if (current !== generation) return
    visible.value = false
    busy.value = false
    emit('deleted', id)
  } catch (cause: any) {
    if (current === generation) error.value = cause?.message || props.failureMessage
  } finally { if (current === generation) busy.value = false }
}
</script>

<template>
  <slot v-if="!hideTrigger" name="trigger" :open="open" :busy="busy">
    <t-button class="personal-document-delete" size="small" variant="text" theme="danger" :disabled="disabled || busy" @click="open">
      <template #icon><t-icon name="delete" size="16px" /></template>
      {{ label }}
    </t-button>
  </slot>
  <t-dialog attach="body" mode="modal" :show-overlay="true" :visible="visible" :header="label" theme="warning"
    :confirm-btn="{ content: t('common.delete'), theme: 'danger', loading: busy, disabled: disabled || busy }"
    :cancel-btn="{ content: t('common.cancel'), disabled: busy }" :close-btn="!busy" :close-on-overlay-click="!busy" :close-on-esc-keydown="!busy"
    @confirm="confirm" @close="close" @cancel="close">
    <p>{{ confirmation }}</p>
    <t-alert v-if="error" theme="error" :message="error" role="alert" />
  </t-dialog>
</template>
