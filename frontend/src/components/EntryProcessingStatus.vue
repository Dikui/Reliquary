<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { reparseEntry } from '@/api/library'
import { entryProcessingState, type EntryProcessingInput } from '@/utils/entryProcessingStatus'

const props = defineProps<{ entry: EntryProcessingInput; compact?: boolean; disabled?: boolean }>()
const emit = defineEmits<{ updated: [value: Record<string, unknown> & { id: string }] }>()
const { t } = useI18n()
const state = computed(() => entryProcessingState(props.entry))
const retrying = ref(false), error = ref('')
let generation = 0
watch(() => props.entry.id, () => { generation++; retrying.value = false; error.value = '' })
onBeforeUnmount(() => { generation++ })

async function retry() {
  if (!props.entry.id || !state.value.retryable || retrying.value || props.disabled) return
  const id = props.entry.id, request = ++generation
  retrying.value = true; error.value = ''
  try {
    const response: any = await reparseEntry(id)
    if (request !== generation || props.entry.id !== id) return
    if (!response?.success || !response.data) throw Object.assign(new Error(response?.message || t('entryStatus.retryFailed')), { status: 200 })
    emit('updated', { ...response.data, id })
    MessagePlugin.success(t('entryStatus.retryQueued'))
  } catch (cause: any) {
    if (request === generation && props.entry.id === id) {
      error.value = !cause?.status ? t('entryStatus.retryUncertain') : cause?.message || t('entryStatus.retryFailed')
    }
  } finally { if (request === generation) retrying.value = false }
}
</script>

<template>
  <div v-if="entry.id" class="entry-processing-status" :class="{ compact }" :aria-label="t('entryStatus.title')" :aria-busy="retrying">
    <div class="entry-processing-status__labels">
      <t-tag v-if="state.stored" size="small" theme="success" variant="light-outline">{{ t('entryStatus.saved') }}</t-tag>
      <t-tag size="small" :theme="state.theme" variant="light-outline">{{ t(state.label) }}</t-tag>
      <t-button v-if="state.retryable" size="small" variant="text" :loading="retrying" :disabled="retrying || disabled" @click.stop="retry">
        {{ t('entryStatus.retry') }}
      </t-button>
    </div>
    <p v-if="!compact && state.failed" class="entry-processing-status__hint">{{ t('entryStatus.savedHint') }}</p>
    <p v-if="!compact && state.failed && entry.error_message" class="entry-processing-status__error">{{ entry.error_message }}</p>
    <p v-if="error" class="entry-processing-status__error" role="alert">{{ error }}</p>
  </div>
</template>

<style scoped>
.entry-processing-status { display: grid; gap: 6px; min-width: 0; }
.entry-processing-status__labels { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; }
.entry-processing-status__labels :deep(.t-tag) { height: auto; white-space: normal; }
.entry-processing-status__hint, .entry-processing-status__error { margin: 0; font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.entry-processing-status__hint { color: var(--td-text-color-secondary); }
.entry-processing-status__error { color: var(--td-error-color); }
.compact { font-size: 12px; }
</style>
