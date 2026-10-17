<script setup lang="ts">
import { computed, onUnmounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { resolveIntakeReview, type IntakeReviewAction, type ReviewEntry } from '@/api/entry-review'
import type { IntakeSourceInfo } from '@/api/library'
import { isValidURL } from '@/utils/security'

const props = defineProps<{ entry: ReviewEntry }>()
const emit = defineEmits<{ changed: [value: ReviewEntry | undefined, removed: boolean]; 'busy-change': [value: boolean] }>()
const { t } = useI18n()
const fieldID = useId()
const busy = ref(false), error = ref(''), sourceDraft = ref<IntakeSourceInfo>({})
let disposed = false
const urlInput = ref<{ focus: () => void }>(), invalidURL = ref(false)
const confirmation = ref<IntakeReviewAction | ''>('')
const missingLink = computed(() => props.entry.source_kind !== 'mine' && !props.entry.metadata?.intake?.source_info?.original_url?.trim())
const confirmationTitle = computed(() => t(`knowledgeReview.confirmations.${confirmation.value}.title`))
const confirmationDescription = computed(() => t(`knowledgeReview.confirmations.${confirmation.value}.description`))
const confirmationLabel = computed(() => t(`knowledgeReview.actions.${confirmation.value}`))
watch(() => props.entry.id, () => { error.value = ''; invalidURL.value = false; confirmation.value = '' })
watch(() => props.entry.metadata?.intake?.source_info, value => { sourceDraft.value = { ...value } }, { immediate: true, deep: true })
watch(busy, value => { if (!disposed) emit('busy-change', value) }, { flush: 'sync' })
onUnmounted(() => { disposed = true; emit('busy-change', false) })
async function submit(action: IntakeReviewAction, confirmed = false) {
  if (disposed || busy.value) return
  error.value = ''
  invalidURL.value = false
  if (action === 'supplement_url') {
    const url = sourceDraft.value.original_url?.trim() || ''
    if (!/^https?:\/\//i.test(url) || !isValidURL(url)) { invalidURL.value = true; error.value = t('knowledgeReview.invalidLink'); urlInput.value?.focus(); return }
    sourceDraft.value.original_url = url
  }
  if (!confirmed && ['admit_unverified', 'reject'].includes(action)) { confirmation.value = action; return }
  const id = props.entry.id
  busy.value = true
  try {
    const response = await resolveIntakeReview(id, { action, ...(['admit_unverified', 'reject'].includes(action) ? { confirmed } : { source_info: { ...sourceDraft.value } }) })
    if (!response.success) throw new Error(response.message || t('knowledgeReview.failed'))
    if (disposed || props.entry.id !== id) return
    confirmation.value = ''
    MessagePlugin.success(t('knowledgeReview.saved'))
    emit('changed', response.data, action === 'reject')
  } catch (cause: any) { if (!disposed && props.entry.id === id) error.value = cause?.message || t('knowledgeReview.failed') }
  finally { busy.value = false }
}
</script>

<template>
  <section class="entry-review-actions" :aria-label="t('knowledgeReview.intake')">
      <p v-if="missingLink">{{ t('knowledgeReview.missingLinkHint') }}</p>
      <div v-if="entry.source_kind !== 'mine'" class="review-source-fields">
        <label v-if="!missingLink"><span>{{ t('library.intakeAuthor') }}</span><t-input v-model="sourceDraft.author" :disabled="busy" :aria-label="t('library.intakeAuthor')" /></label>
        <label v-if="!missingLink"><span>{{ t('library.intakePublisher') }}</span><t-input v-model="sourceDraft.publisher" :disabled="busy" :aria-label="t('library.intakePublisher')" /></label>
        <label :for="`review-original-url-${fieldID}`"><span>{{ t('library.intakeOriginalUrl') }}</span><t-input :id="`review-original-url-${fieldID}`" ref="urlInput" v-model="sourceDraft.original_url" :disabled="busy" type="url" :aria-label="t('library.intakeOriginalUrl')" :aria-invalid="invalidURL" :aria-describedby="invalidURL ? `review-error-${fieldID}` : undefined" /></label>
        <label v-if="!missingLink"><span>{{ t('library.intakePublishedAt') }}</span><t-input v-model="sourceDraft.published_at" :disabled="busy" :aria-label="t('library.intakePublishedAt')" /></label>
      </div>
      <div class="review-buttons">
        <template v-if="missingLink">
          <t-button theme="primary" :loading="busy" :disabled="busy" @click="submit('supplement_url')">{{ t('knowledgeReview.actions.supplement_url') }}</t-button>
          <t-button variant="outline" :disabled="busy" @click="submit('admit_unverified')">{{ t('knowledgeReview.actions.admit_unverified') }}</t-button>
        </template>
        <t-button v-else theme="primary" :loading="busy" :disabled="busy" @click="submit('approve')">{{ t(entry.related_entry_id ? 'knowledgeReview.saveAsNew' : 'articleJudgments.approveReview') }}</t-button>
        <t-button variant="outline" :disabled="busy" @click="submit('retry')">{{ t('library.intakeRetry') }}</t-button>
      </div>
      <t-button theme="danger" variant="text" :disabled="busy" @click="submit('reject')">{{ t('articleJudgments.rejectReview') }}</t-button>
    <p v-if="error" :id="`review-error-${fieldID}`" class="review-error" role="alert">{{ error }}</p>
    <t-dialog :visible="Boolean(confirmation)" :header="confirmation ? confirmationTitle : ''" :confirm-btn="{ content: confirmation ? confirmationLabel : '', theme: confirmation === 'reject' ? 'danger' : 'primary', loading: busy, disabled: busy }" :cancel-btn="{ content: t('common.cancel'), disabled: busy }" :close-btn="!busy" :close-on-overlay-click="false" :close-on-esc-keydown="!busy" :z-index="6500" @close="confirmation = ''" @cancel="confirmation = ''" @confirm="submit(confirmation as IntakeReviewAction, true)">
      <p>{{ entry.title || entry.file_name }}</p>
      <p v-if="confirmation">{{ confirmationDescription }}</p>
      <p v-if="error" class="review-error" role="alert">{{ error }}</p>
    </t-dialog>
  </section>
</template>

<style scoped lang="less">
.entry-review-actions { display: grid; gap: 12px; min-width: 0; color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.7; overflow-wrap: anywhere; }
.entry-review-actions p { margin: 0; }
.review-buttons, .review-type-row { display: flex; flex-wrap: wrap; gap: 8px; align-items: flex-end; }
.review-source-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.review-source-fields label, .review-type-row label { display: grid; gap: 6px; min-width: 0; }
.review-type-row label { flex: 1; min-width: 180px; }
.review-error { color: var(--td-error-color); }
@media (max-width: 560px) { .review-source-fields { grid-template-columns: minmax(0, 1fr); } .review-type-row { align-items: stretch; flex-direction: column; } }
</style>
