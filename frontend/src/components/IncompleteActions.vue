<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { acceptIncomplete, discardIncomplete } from '@/api/ingest'

const props = withDefaults(defineProps<{ articleId: string; title?: string; allowDiscard?: boolean; disabled?: boolean }>(), { title: '', allowDiscard: false, disabled: false })
const emit = defineEmits<{ changed: [removed: boolean]; 'busy-change': [busy: boolean] }>()
const { t } = useI18n()
const busy = ref(false), error = ref(''), confirmation = ref<'accept' | 'discard' | ''>('')
let disposed = false
watch(() => props.articleId, () => { error.value = ''; confirmation.value = '' })
watch(busy, value => { if (!disposed) emit('busy-change', value) }, { flush: 'sync' })
onUnmounted(() => { disposed = true; emit('busy-change', false) })
function request(action: 'accept' | 'discard') {
  if (props.disabled || disposed || busy.value || (action === 'discard' && !props.allowDiscard)) return
  error.value = ''; confirmation.value = action
}
function cancel() { if (!busy.value) confirmation.value = '' }
async function confirm() {
  const action = confirmation.value
  if (!action || props.disabled || disposed || busy.value || (action === 'discard' && !props.allowDiscard)) return
  const id = props.articleId
  busy.value = true; error.value = ''
  try {
    const response = action === 'accept' ? await acceptIncomplete(id) : await discardIncomplete(id)
    if (!response.success) throw new Error(response.message || t('ingestStatus.failed'))
    if (disposed || props.articleId !== id) return
    confirmation.value = ''
    MessagePlugin.success(t(action === 'accept' ? 'ingestStatus.acceptSaved' : 'ingestStatus.discardSaved'))
    emit('changed', action === 'discard')
  } catch (cause: any) { if (!disposed && props.articleId === id) error.value = cause?.message || t('ingestStatus.failed') }
  finally { busy.value = false }
}
</script>

<template>
  <section class="incomplete-actions" :aria-label="t('ingestStatus.acceptIncomplete')">
    <div class="incomplete-buttons">
      <t-button theme="primary" variant="outline" :loading="busy" :disabled="disabled || busy" @click="request('accept')">{{ t('ingestStatus.acceptIncomplete') }}</t-button>
      <t-button v-if="allowDiscard" theme="danger" variant="text" :disabled="disabled || busy" @click="request('discard')">{{ t('ingestStatus.discard') }}</t-button>
    </div>
    <p v-if="error && !confirmation" class="incomplete-error" role="alert">{{ error }}</p>
    <t-dialog :visible="Boolean(confirmation)" :header="t(confirmation === 'discard' ? 'ingestStatus.discardTitle' : 'ingestStatus.acceptTitle')" :confirm-btn="{ content: t(confirmation === 'discard' ? 'ingestStatus.discard' : 'ingestStatus.acceptIncomplete'), theme: confirmation === 'discard' ? 'danger' : 'primary', loading: busy, disabled: disabled || busy }" :cancel-btn="{ content: t('common.cancel'), disabled: busy }" :close-btn="!busy" :close-on-overlay-click="false" :close-on-esc-keydown="!busy" :z-index="6500" @close="cancel" @cancel="cancel" @confirm="confirm">
      <p>{{ title }}</p>
      <p>{{ t(confirmation === 'discard' ? 'ingestStatus.discardDescription' : 'ingestStatus.acceptDescription') }}</p>
      <p v-if="error" class="incomplete-error" role="alert">{{ error }}</p>
    </t-dialog>
  </section>
</template>

<style scoped lang="less">
.incomplete-actions { font-size: 13px; line-height: 1.7; }
.incomplete-buttons { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.incomplete-error { color: var(--td-error-color); }
</style>
