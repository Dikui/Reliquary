<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { reviewLibrary, type LibraryCatalogItem, type LibraryReviewScope } from '@/api/library'
import { useUIStore } from '@/stores/ui'

const props = defineProps<{ library?: LibraryCatalogItem; scope: LibraryReviewScope; disabled?: boolean }>()
const emit = defineEmits<{ submitted: [] }>()
const { t } = useI18n()
const ui = useUIStore()
const target = ref<LibraryCatalogItem>()
const visible = ref(false), busy = ref(false), error = ref('')
const result = ref<{ task_id: string; total: number }>()
const label = computed(() => t(props.scope === 'all' ? 'knowledgeReview.reviewAll' : 'knowledgeReview.reviewPending'))
const unavailable = computed(() => props.disabled || !props.library || props.library.template === 'generated')

function open() {
  if (unavailable.value || busy.value) return
  target.value = props.library
  result.value = undefined
  error.value = ''
  visible.value = true
}
function close() { if (!busy.value) visible.value = false }
async function confirm() {
  if (!visible.value || busy.value || !target.value) return
  if (result.value) { close(); return }
  busy.value = true
  error.value = ''
  try {
    const response = await reviewLibrary(target.value.id, props.scope)
    if (!response.success) throw new Error(response.message || t('knowledgeReview.failed'))
    result.value = response.data
    emit('submitted')
  } catch (cause: any) {
    error.value = cause?.message || t('knowledgeReview.failed')
  } finally { busy.value = false }
}
function showQueue() { close(); ui.openSettings('runtime-queues') }
</script>

<template>
  <span v-if="library?.template !== 'generated'" class="library-review-action">
    <t-tooltip :content="library ? (library.display_name || library.name) : t('library.rebuildLibrarySelectOne')">
      <span><t-button variant="text" size="small" :disabled="unavailable || busy" @click="open">{{ label }}</t-button></span>
    </t-tooltip>
    <t-dialog :visible="visible" :header="label" :confirm-btn="{ content: result ? t('common.close') : label, loading: busy, disabled: busy }"
      :cancel-btn="result ? null : { content: t('common.cancel'), disabled: busy }" :close-btn="!busy"
      :close-on-overlay-click="false" :close-on-esc-keydown="!busy" @confirm="confirm" @cancel="close" @close="close">
      <template v-if="result">
        <p role="status">{{ t('knowledgeReview.reviewSubmitted', { count: result.total }) }}</p>
        <t-button variant="text" @click="showQueue">{{ t('settings.taskQueue') }}</t-button>
      </template>
      <template v-else>
        <p>{{ t(scope === 'all' ? 'knowledgeReview.reviewAllConfirm' : 'knowledgeReview.reviewPendingConfirm', { name: target?.display_name || target?.name }) }}</p>
        <p>{{ t('knowledgeReview.reviewCost') }}</p>
      </template>
      <t-alert v-if="error" theme="error" :message="error" role="alert" />
    </t-dialog>
  </span>
</template>

<style scoped>
.library-review-action { display: inline-flex; flex-shrink: 0; }
</style>
