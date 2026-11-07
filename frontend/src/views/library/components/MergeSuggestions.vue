<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, EmptyState, ErrorState, Select, Tag } from '@/components/ui'
import { confirm, toast, type ConfirmHandle } from '@/components/ui/feedback'
import { acceptEntityMergeSuggestion, rejectEntityMergeSuggestion, type EntityMergeSuggestion } from '@/api/entities'
import type { LibraryCatalogItem } from '@/api/library'
import { searchErrorCode } from '@/utils/search'
import MergeCandidate from './MergeCandidate.vue'

const props = defineProps<{ items: EntityMergeSuggestion[]; libraries?: LibraryCatalogItem[]; loading: boolean; error: string }>()
const emit = defineEmits<{ refresh: []; changed: [id: string, status: 'accepted' | 'rejected', keepEntityId?: string]; 'busy-change': [value: boolean] }>()
const { t, n } = useI18n()
const status = ref<'pending' | 'accepted' | 'rejected'>('pending'), index = ref(0), busy = ref(false), asking = ref(false), actionError = ref('')
const filtered = computed(() => props.items.filter(item => item.status === status.value))
const selected = computed(() => filtered.value[index.value])
const keep = ref<'a' | 'b'>('b')
const keepA = computed(() => selected.value?.status === 'accepted' ? selected.value.keep_entity_id === selected.value.new_entity_id : keep.value === 'a')
const targetId = computed(() => keepA.value ? selected.value?.new_entity_id : selected.value?.candidate_entity_id)
const targetName = computed(() => (keepA.value ? selected.value?.new_entity_name : selected.value?.candidate_entity_name) || targetId.value?.slice(0, 8) || '')
const sourceName = computed(() => (keepA.value ? selected.value?.candidate_entity_name : selected.value?.new_entity_name) || (keepA.value ? selected.value?.candidate_entity_id : selected.value?.new_entity_id)?.slice(0, 8) || '')
const statuses = computed(() => [
  { value: 'pending', label: t('mergeSuggestions.status.pending') },
  { value: 'accepted', label: t('mergeSuggestions.status.accepted') },
  { value: 'rejected', label: t('mergeSuggestions.status.rejected') },
])
let dialog: ConfirmHandle | undefined, disposed = false
watch(status, () => { index.value = 0; actionError.value = '' })
watch(() => selected.value?.id, () => { actionError.value = ''; keep.value = 'b' })
watch(() => filtered.value.length, size => { index.value = Math.min(index.value, Math.max(0, size - 1)) })
function move(delta: number) {
  if (busy.value || asking.value || props.loading) return
  index.value = Math.max(0, Math.min(index.value + delta, filtered.value.length - 1))
  actionError.value = ''
}
function keydown(event: KeyboardEvent) {
  if (event.altKey || event.ctrlKey || event.metaKey || event.isComposing || (event.target as HTMLElement)?.closest?.('input, textarea, select, [contenteditable="true"], [role="dialog"]')) return
  if (event.key.toLowerCase() === 'j' || event.key.toLowerCase() === 'k') { event.preventDefault(); move(event.key.toLowerCase() === 'j' ? 1 : -1) }
}
async function decide(item: EntityMergeSuggestion, accept: boolean, keepEntityId?: string) {
  if (busy.value || item.status !== 'pending' || disposed) return
  busy.value = true; emit('busy-change', true); actionError.value = ''
  try {
    const response = await (accept ? acceptEntityMergeSuggestion(item.id, keepEntityId!) : rejectEntityMergeSuggestion(item.id))
    if (!response.success) throw response
    if (disposed) return
    emit('changed', item.id, accept ? 'accepted' : 'rejected', keepEntityId)
    toast.success(accept ? t('mergeSuggestions.feedback.accept') : t('mergeSuggestions.feedback.reject'))
  } catch (cause: any) {
    if (disposed) return
    const code = searchErrorCode(cause)
    actionError.value = t(code === 1003 || cause?.status === 404 ? 'pendingPage.notFound' : code === 1000 || code === 1005 || cause?.status === 409 ? 'pendingPage.changed' : 'pendingPage.saveFailed')
    throw cause
  } finally { busy.value = false; if (!disposed && !asking.value) emit('busy-change', false) }
}
function accept() {
  const item = selected.value
  const keepEntityId = targetId.value
  if (!item || !keepEntityId || busy.value || dialog) return
  asking.value = true; emit('busy-change', true)
  dialog = confirm({ header: t('mergeSuggestions.confirmTitle'), body: t('mergeSuggestions.confirmBody', { source: sourceName.value, target: targetName.value }), confirmBtn: t('pendingPage.mergeInto', { name: targetName.value }), onConfirm: () => decide(item, true, keepEntityId) })
  void dialog.result.finally(() => { dialog = undefined; asking.value = false; if (!disposed) emit('busy-change', false) })
}
async function reject() { if (selected.value) { try { await decide(selected.value, false) } catch { /* The inline state remains available for retry. */ } } }
onBeforeUnmount(() => { disposed = true; dialog?.destroy(); emit('busy-change', false) })
</script>

<template>
  <section class="merge-suggestions" tabindex="0" :aria-label="t('mergeSuggestions.title')" @keydown="keydown">
    <div class="merge-toolbar"><p>{{ t('mergeSuggestions.description') }}</p><Select v-model="status" :disabled="busy || loading || asking" :aria-label="t('mergeSuggestions.statusFilter')"><option v-for="option in statuses" :key="option.value" :value="option.value">{{ option.label }}</option></Select></div>
    <ErrorState v-if="error" :title="error"><Button :disabled="busy" @click="emit('refresh')">{{ t('common.retry') }}</Button></ErrorState>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <EmptyState v-else-if="!selected && !error" :title="status === 'pending' ? t('mergeSuggestions.empty.pending') : status === 'accepted' ? t('mergeSuggestions.empty.accepted') : t('mergeSuggestions.empty.rejected')" />
    <template v-if="selected">
      <div class="merge-queue"><Tag>{{ statuses.find(item => item.value === selected?.status)?.label }}</Tag><span class="mono" role="status">{{ n(index + 1) }} / {{ n(filtered.length) }}</span><Button :disabled="busy || loading || asking || index === 0" :aria-label="t('pendingPage.previous')" @click="move(-1)">K</Button><Button :disabled="busy || loading || asking || index >= filtered.length - 1" :aria-label="t('pendingPage.next')" @click="move(1)">J</Button></div>
      <div class="merge-pair">
        <MergeCandidate :libraries="libraries" :key="selected.new_entity_id" :status="selected.status" :merged-into-name="selected.status === 'accepted' && !keepA ? targetName : undefined" :entity-id="selected.new_entity_id" :name="selected.new_entity_name" :entry-id="selected.entry_id" :label="t('pendingPage.candidateA')" />
        <MergeCandidate :libraries="libraries" :key="selected.candidate_entity_id" :status="selected.status" :merged-into-name="selected.status === 'accepted' && keepA ? targetName : undefined" :entity-id="selected.candidate_entity_id" :name="selected.candidate_entity_name" :entry-id="selected.entry_id" :label="t('pendingPage.candidateB')" />
      </div>
      <div class="merge-reason"><b>{{ t('pendingPage.reason') }}</b><p>{{ selected.reason || t('mergeSuggestions.noReason') }}</p></div>
      <p v-if="actionError" class="merge-action-error" role="alert">{{ actionError }}</p>
      <div v-if="selected.status === 'pending'" class="merge-decide">
        <label class="merge-keep"><span>{{ t('pendingPage.keepEntity') }}</span><Select v-model="keep" :disabled="busy || loading || asking" :aria-label="t('pendingPage.keepEntity')"><option value="a">{{ t('pendingPage.keepCandidate', { candidate: 'A', name: selected.new_entity_name || selected.new_entity_id.slice(0, 8) }) }}</option><option value="b">{{ t('pendingPage.keepCandidate', { candidate: 'B', name: selected.candidate_entity_name || selected.candidate_entity_id.slice(0, 8) }) }}</option></Select></label>
        <Button variant="primary" :loading="busy" :disabled="loading || asking" @click="accept">{{ t('pendingPage.mergeInto', { name: targetName }) }}</Button><Button variant="ghost" :disabled="busy || loading || asking" @click="reject">{{ t('mergeSuggestions.reject') }}</Button><Button v-if="actionError" :disabled="busy" @click="emit('refresh')">{{ t('common.refresh') }}</Button>
      </div>
    </template>
  </section>
</template>

<style scoped>
.merge-suggestions { display: flex; flex-direction: column; gap: var(--space-4); min-width: 0; outline-offset: 4px; }
.merge-toolbar, .merge-queue, .merge-decide { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: center; }
.merge-toolbar { justify-content: space-between; }
.merge-toolbar p, .merge-reason p { margin: 0; color: var(--muted); font-size: var(--type-note-size); line-height: var(--leading); }
.merge-toolbar .rq-select { width: auto; }
.merge-keep { display: flex; align-items: center; gap: var(--space-2); min-width: 0; max-width: 100%; font-size: var(--text-sm); }
.merge-keep .rq-select { width: auto; min-width: 0; max-width: 100%; flex: 1; }
.merge-queue { justify-content: flex-end; }
.merge-queue .rq-tag { margin-right: auto; }
.merge-queue .rq-button { min-height: 32px; }
.merge-queue .mono { font-family: var(--mono); font-size: var(--text-sm); }
.merge-pair { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); border: 2px solid var(--rule); }
.merge-pair > :deep(.merge-candidate + .merge-candidate) { border-left: 2px solid var(--rule); }
.merge-reason { border-top: 2px solid var(--rule); padding: var(--space-3) var(--space-4); display: grid; gap: var(--space-1); font-size: var(--text-sm); overflow-wrap: anywhere; }
.merge-action-error { margin: 0; color: var(--danger); }
.merge-suggestions :deep(.rq-empty) { padding: var(--space-5) 0; }
@media (max-width: 760px) { .merge-pair { grid-template-columns: minmax(0, 1fr); } .merge-pair > :deep(.merge-candidate + .merge-candidate) { border-left: 0; border-top: 2px solid var(--rule); } .merge-keep { width: 100%; } }
</style>
