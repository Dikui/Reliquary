<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Input } from '@/components/ui'
import { confirm, toast, type ConfirmHandle } from '@/components/ui/feedback'
import { resolveIntakeReview, type IntakeReviewAction, type ReviewEntry } from '@/api/entry-review'
import type { IntakeSourceInfo } from '@/api/library'
import { isValidURL } from '@/utils/security'
import { searchErrorCode } from '@/utils/search'

const props = defineProps<{ entry: ReviewEntry; disabled?: boolean }>()
const emit = defineEmits<{ changed: []; 'busy-change': [value: boolean] }>()
const { t } = useI18n(), id = useId()
const source = ref<IntakeSourceInfo>({}), error = ref(''), invalidURL = ref(false), busy = ref(false), asking = ref(false)
const urlInput = ref<InstanceType<typeof Input>>()
const missingLink = computed(() => props.entry.source_kind !== 'mine' && !props.entry.metadata?.intake?.source_info?.original_url?.trim())
const disabled = computed(() => props.disabled || busy.value || asking.value)
let disposed = false, dialog: ConfirmHandle | undefined
watch(() => props.entry, entry => { source.value = { ...entry.metadata?.intake?.source_info }; error.value = ''; invalidURL.value = false }, { immediate: true })
async function submit(action: IntakeReviewAction, confirmed = false) {
  if (disposed || busy.value || (props.disabled && !asking.value)) return
  error.value = ''; invalidURL.value = false
  if (action === 'supplement_url') {
    const url = source.value.original_url?.trim() || ''
    if (!/^https?:\/\//i.test(url) || !isValidURL(url)) { invalidURL.value = true; error.value = t('knowledgeReview.invalidLink'); urlInput.value?.focus(); return }
    source.value.original_url = url
  }
  if (!confirmed && (action === 'admit_unverified' || action === 'reject')) {
    asking.value = true; emit('busy-change', true)
    dialog = confirm({
      header: action === 'reject' ? t('knowledgeReview.confirmations.reject.title') : t('knowledgeReview.confirmations.admit_unverified.title'),
      body: `${props.entry.title || props.entry.file_name || props.entry.id.slice(0, 8)}\n\n${action === 'reject' ? t('knowledgeReview.confirmations.reject.description') : t('knowledgeReview.confirmations.admit_unverified.description')}`,
      confirmBtn: action === 'reject' ? t('pendingPage.reject') : t('knowledgeReview.actions.admit_unverified'),
      theme: action === 'reject' ? 'danger' : 'default',
      onConfirm: () => submit(action, true),
    })
    void dialog.result.finally(() => { asking.value = false; dialog = undefined; if (!disposed) emit('busy-change', false) })
    return
  }
  busy.value = true; emit('busy-change', true)
  try {
    const response = await resolveIntakeReview(props.entry.id, { action, ...(confirmed ? { confirmed: true } : { source_info: { ...source.value } }) })
    if (!response.success) throw response
    if (!disposed) { toast.success(t('knowledgeReview.saved')); emit('changed') }
  } catch (cause: any) {
    if (!disposed) {
      const code = searchErrorCode(cause)
      error.value = t(code === 1003 || cause?.status === 404 ? 'pendingPage.notFound' : code === 1000 || code === 1005 || cause?.status === 409 ? 'pendingPage.changed' : 'knowledgeReview.failed')
    }
    if (confirmed) throw cause
  } finally { busy.value = false; if (!disposed && !asking.value) emit('busy-change', false) }
}
onBeforeUnmount(() => { disposed = true; dialog?.destroy(); emit('busy-change', false) })
</script>

<template>
  <section class="pending-review-actions" :aria-label="t('knowledgeReview.intake')">
    <template v-if="entry.source_kind !== 'mine'">
      <p v-if="missingLink">{{ t('knowledgeReview.missingLinkHint') }}</p>
      <label v-if="missingLink" :for="`${id}-url`">{{ t('library.intakeOriginalUrl') }}<Input :id="`${id}-url`" ref="urlInput" v-model="source.original_url" type="url" :disabled="disabled" :aria-invalid="invalidURL" :aria-describedby="error ? `${id}-error` : undefined" /></label>
      <details class="review-source-fields"><summary>{{ t('pendingPage.editSource') }}</summary><div>
        <label :for="`${id}-author`">{{ t('library.intakeAuthor') }}<Input :id="`${id}-author`" v-model="source.author" :disabled="disabled" /></label>
        <label :for="`${id}-publisher`">{{ t('library.intakePublisher') }}<Input :id="`${id}-publisher`" v-model="source.publisher" :disabled="disabled" /></label>
        <label v-if="!missingLink" :for="`${id}-url`">{{ t('library.intakeOriginalUrl') }}<Input :id="`${id}-url`" ref="urlInput" v-model="source.original_url" type="url" :disabled="disabled" /></label>
        <label :for="`${id}-date`">{{ t('library.intakePublishedAt') }}<Input :id="`${id}-date`" v-model="source.published_at" :disabled="disabled" /></label>
      </div></details>
    </template>
    <div class="review-buttons"><template v-if="missingLink"><Button variant="primary" :disabled="disabled" :loading="busy" @click="submit('supplement_url')">{{ t('knowledgeReview.actions.supplement_url') }}</Button><Button :disabled="disabled" @click="submit('admit_unverified')">{{ t('knowledgeReview.actions.admit_unverified') }}</Button></template><Button v-else variant="primary" :disabled="disabled" :loading="busy" @click="submit('approve')">{{ t(entry.related_entry_id ? 'knowledgeReview.saveAsNew' : 'articleJudgments.approveReview') }}</Button><Button variant="danger" :disabled="disabled" @click="submit('reject')">{{ t('pendingPage.reject') }}</Button></div>
    <p v-if="error" :id="`${id}-error`" class="review-error" role="alert">{{ error }}</p>
  </section>
</template>

<style scoped>
.pending-review-actions { display: grid; gap: var(--space-3); min-width: 0; font-size: var(--text-sm); }
.pending-review-actions p { margin: 0; color: var(--muted); }
.pending-review-actions label { display: grid; gap: var(--space-1); min-width: 0; }
.review-source-fields summary { cursor: pointer; text-decoration: underline; }
.review-source-fields > div { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-3); padding-top: var(--space-3); }
.review-buttons { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.pending-review-actions .review-error { color: var(--danger); }
@media (max-width: 760px) { .review-source-fields > div { grid-template-columns: minmax(0, 1fr); } }
</style>
