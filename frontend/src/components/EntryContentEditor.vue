<script setup lang="ts">
import { computed, inject, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { routerKey } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Button, Textarea as BodyTextarea, confirm, toast } from '@/components/ui'
import { getEntryEditableBody, updateEntryContent } from '@/api/library'
import { copyToClipboard } from '@/utils/clipboard'

const props = defineProps<{ entryId: string; title: string; indexOnly: boolean }>()
const emit = defineEmits<{ cancel: []; saved: [value: { id: string; content: string; status: string }]; preview: [] }>()
const { t } = useI18n()
const router = inject(routerKey, undefined)
const draft = ref(''), original = ref(''), bodyHash = ref('')
const loading = ref(true), saving = ref(false), error = ref(''), conflict = ref(false)
const mode = ref<'edit' | 'preview'>('edit')
const textarea = ref<InstanceType<typeof BodyTextarea>>()
const dirty = computed(() => draft.value !== original.value)
const controller = new AbortController()
let removeGuard: (() => void) | undefined
let leavePromise: Promise<boolean> | undefined
let leaveDialog: ReturnType<typeof confirm> | undefined

async function load() {
  loading.value = true; error.value = ''
  try {
    const body = await getEntryEditableBody(props.entryId, controller.signal)
    if (controller.signal.aborted) return
    original.value = body.content; draft.value = body.content; bodyHash.value = body.bodyHash
    conflict.value = false; mode.value = 'edit'
    await nextTick(); textarea.value?.focus()
  } catch { if (!controller.signal.aborted) error.value = t('entryEditor.loadFailed') }
  finally { if (!controller.signal.aborted) loading.value = false }
}

function confirmLeave(): Promise<boolean> {
  if (saving.value) return Promise.resolve(false)
  if (!dirty.value) return Promise.resolve(true)
  if (leavePromise) return leavePromise
  leavePromise = new Promise(resolve => {
    const finish = (discard: boolean) => {
      if (discard) draft.value = original.value
      leaveDialog?.destroy(); leaveDialog = undefined; leavePromise = undefined
      resolve(discard)
    }
    leaveDialog = confirm({
      header: t('entryEditor.discardTitle'), body: t('entryEditor.discardHint'),
      confirmBtn: t('entryEditor.discard'), cancelBtn: t('entryEditor.keepEditing'), closeOnOverlayClick: false,
      onConfirm: () => finish(true), onCancel: () => finish(false), onClose: () => finish(false),
    })
  })
  return leavePromise
}
async function cancel() { if (await confirmLeave()) emit('cancel') }
async function reload() { if (await confirmLeave()) await load() }
async function showPreview() {
  mode.value = 'preview'
  await nextTick(); emit('preview')
}
async function save() {
  if (saving.value || loading.value || !bodyHash.value || !dirty.value || conflict.value) return
  if (!draft.value.trim()) { error.value = t('entryEditor.empty'); mode.value = 'edit'; await nextTick(); textarea.value?.focus(); return }
  const content = draft.value
  saving.value = true; error.value = ''
  try {
    const response = await updateEntryContent(props.entryId, { content, base_body_hash: bodyHash.value })
    if (controller.signal.aborted) return
    if (!response.success || !['accepted', 'exists'].includes(response.data?.status)) throw Object.assign(new Error(response.message || t('entryEditor.saveFailed')), { status: 200 })
    original.value = content
    saving.value = false
    toast.success(t(response.data.status === 'exists' ? 'entryEditor.unchanged' : 'entryEditor.savedPending'))
    emit('saved', { id: props.entryId, content, status: response.data.status })
  } catch (cause: any) {
    if (controller.signal.aborted) return
    conflict.value = cause?.error?.details?.reason === 'body_changed'
    error.value = conflict.value ? t('entryEditor.conflict') : cause?.status === 409 ? t('entryEditor.busy')
      : !cause?.status ? t('entryEditor.saveUnconfirmed') : cause?.message || t('entryEditor.saveFailed')
  } finally { saving.value = false }
}
async function copyDraft() {
  const copied = await copyToClipboard(draft.value)
  if (copied) toast.success(t('common.copySuccess'))
  else toast.error(t('common.copyFailed'))
}
function beforeUnload(event: BeforeUnloadEvent) {
  if (dirty.value || saving.value) { event.preventDefault(); event.returnValue = '' }
}
onMounted(() => {
  void load()
  window.addEventListener('beforeunload', beforeUnload)
  removeGuard = router?.beforeEach(async () => {
    const allowed = await confirmLeave()
    if (allowed) emit('cancel')
    return allowed
  })
})
onBeforeUnmount(() => {
  controller.abort(); removeGuard?.(); leaveDialog?.destroy()
  window.removeEventListener('beforeunload', beforeUnload)
})
defineExpose({ confirmLeave })
</script>

<template>
  <section class="entry-editor" :aria-label="t('entryEditor.title')" :aria-busy="loading || saving">
    <div class="entry-editor__heading"><h3>{{ title }}</h3><span role="status">{{ dirty ? t('entryEditor.unsaved') : t('entryEditor.title') }}</span></div>
    <p class="entry-editor__hint">{{ t(indexOnly ? 'entryEditor.indexHint' : 'entryEditor.processHint') }}</p>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <template v-else-if="bodyHash">
      <div class="entry-editor__tabs" role="group" :aria-label="t('entryEditor.view')">
        <Button variant="default" :aria-pressed="mode === 'edit'" @click="mode = 'edit'">{{ t('entryEditor.edit') }}</Button>
        <Button variant="default" :aria-pressed="mode === 'preview'" @click="showPreview">{{ t('entryEditor.preview') }}</Button>
      </div>
      <label v-show="mode === 'edit'" class="entry-editor__input"><span>{{ t('entryEditor.markdown') }}</span>
        <BodyTextarea ref="textarea" v-model="draft" :disabled="saving" :aria-label="t('entryEditor.markdown')" :aria-invalid="Boolean(error)" :aria-describedby="error ? 'entry-editor-error' : undefined" />
      </label>
      <div v-if="mode === 'preview'" class="entry-editor__preview"><slot name="preview" :content="draft" /></div>
    </template>
    <div v-if="error" id="entry-editor-error" class="entry-editor__error" role="alert">
      <p>{{ error }}</p>
      <Button v-if="!bodyHash || conflict" variant="default" :disabled="loading || saving" @click="reload">{{ t('entryEditor.reload') }}</Button>
      <Button v-if="bodyHash" variant="ghost" @click="copyDraft()">{{ t('entryEditor.copyDraft') }}</Button>
    </div>
    <div class="entry-editor__actions">
      <Button variant="default" :disabled="saving" @click="cancel">{{ t('common.cancel') }}</Button>
      <Button variant="primary" :loading="saving" :disabled="loading || !bodyHash || !dirty || conflict" @click="save">{{ t(indexOnly ? 'entryEditor.saveIndex' : 'entryEditor.saveProcess') }}</Button>
    </div>
  </section>
</template>

<style scoped>
.entry-editor { display: flex; flex-direction: column; gap: 16px; color: var(--text); }
.entry-editor__heading, .entry-editor__tabs, .entry-editor__actions { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.entry-editor__heading { justify-content: space-between; }
.entry-editor__heading h3 { margin: 0; overflow-wrap: anywhere; }
.entry-editor__heading span, .entry-editor__hint { color: var(--muted); font-size: 13px; line-height: 1.7; }
.entry-editor__hint { margin: 0; }
.entry-editor__tabs [aria-pressed="true"] { background: var(--text); color: var(--bg); }
.entry-editor__input { display: grid; gap: 8px; }
.entry-editor__input :deep(textarea) { min-height: min(50vh, 480px); resize: none; font-family: var(--mono); }
.entry-editor__preview { min-height: 40vh; overflow-wrap: anywhere; }
.entry-editor__error { color: var(--danger); overflow-wrap: anywhere; }
.entry-editor__actions { position: sticky; bottom: 0; justify-content: flex-end; padding-block: 12px; background: var(--bg); border-top: 1px solid var(--border); z-index: 1; }
@media (max-width: 640px) { .entry-editor__heading { align-items: flex-start; flex-direction: column; } }
</style>
