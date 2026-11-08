<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter, onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { Button, EmptyState, Select } from '@/components/ui'
import { confirm, toast } from '@/components/ui/feedback'
import { formatDateTime } from '@/utils/dateTime'
import { SHELF_REVIEW_GROUPS, shelfReviewGroup, shelfReviewReady, shelfReviewSubject, shelfOperationForEdit, shelfNameConfirmations } from '@/utils/shelfReview'
import { getEntity } from '@/api/entities'
import ShelfReviewValue from './ShelfReviewValue.vue'
import type { LibraryCatalogItem } from '@/api/library'
import { listShelfChangesets, getShelfChangeset, saveShelfReview, confirmShelfChangeset, type ShelfReviewChangeset, type ShelfReviewDecision, type ShelfReviewItem, type ShelfReviewOperation } from '@/api/shelf-review'

const props = defineProps<{ library: LibraryCatalogItem }>()
const { t, locale } = useI18n(), route = useRoute(), router = useRouter()
const sets = ref<ShelfReviewChangeset[]>([]), selected = ref<ShelfReviewChangeset>(), decisions = ref<Record<string, ShelfReviewDecision>>({})
const loading = ref(false), detailLoading = ref(false), busy = ref(false), error = ref(''), notice = ref('')
const page = ref(1), total = ref(0), baseline = ref('')
const edits = ref<Record<string, ShelfReviewOperation>>({})
const targets = ref<Record<string, string>>({})
let generation = 0, detailGeneration = 0
const blocked = computed(() => busy.value || loading.value || detailLoading.value || selected.value?.status !== 'pending')
const dirty = computed(() => JSON.stringify(decisions.value) !== baseline.value || Object.keys(edits.value).length > 0)
const groups = computed(() => SHELF_REVIEW_GROUPS.map(type => ({ type, items: selected.value?.items.filter(item => shelfReviewGroup(item) === type) || [] })))
const required = computed(() => selected.value?.items.filter(item => item.conflicts.some(conflict => conflict.type === 'required_missing') && !shelfReviewReady(item, decisions.value[item.id])).length || 0)
const canConfirm = computed(() => !!selected.value && selected.value.status === 'pending' && !Object.keys(edits.value).length && selected.value.items.every(item => shelfReviewReady(item, decisions.value[item.id])))
function adopt(set: ShelfReviewChangeset) {
  edits.value = {}; targets.value = {}
  selected.value = set; decisions.value = Object.fromEntries(set.items.filter(item => item.decision).map(item => [item.id, item.decision!])); baseline.value = JSON.stringify(decisions.value)
  sets.value = sets.value.map(item => item.id === set.id ? set : item)
}
function edit(item: ShelfReviewItem) { if (blocked.value) return; edits.value[item.id] = decisions.value[item.id]?.replacement || shelfOperationForEdit(item); notice.value = '' }
function applyEdit(item: ShelfReviewItem) { if (blocked.value) return; decisions.value[item.id] = { item_id: item.id, action: 'replace', replacement: edits.value[item.id] }; delete edits.value[item.id] }
function candidates(item: ShelfReviewItem) { return item.conflicts.flatMap(conflict => conflict.type === 'suspected_same' ? conflict.candidates || [] : []) }
async function sameEntity(item: ShelfReviewItem) {
  const id = targets.value[item.id] || (candidates(item).length === 1 ? candidates(item)[0]!.other_entity_id : '')
  if (!id || blocked.value) return
  busy.value = true; error.value = ''
  try {
    const response = await getEntity(id)
    if (!response.success || !response.data.entity.revision) throw new Error(t('shelfReview.loadFailed'))
    decisions.value[item.id] = { item_id: item.id, action: 'same_entity', target_entity_id: id, target_revision: response.data.entity.revision }; delete edits.value[item.id]
  } catch (cause) { error.value = failure(cause, t('shelfReview.loadFailed')) }
  finally { busy.value = false }
}
function distinctEntity(item: ShelfReviewItem) { const operation = shelfOperationForEdit(item); delete operation.candidate_entity_id; decisions.value[item.id] = { item_id: item.id, action: 'replace', replacement: operation }; delete edits.value[item.id] }
function confirmNames(item: ShelfReviewItem) { decisions.value[item.id] = { item_id: item.id, action: 'apply', name_confirmations: shelfNameConfirmations(item) }; delete edits.value[item.id] }
function title(set: ShelfReviewChangeset) { return `${set.source.type === 'agent' ? `agent · ${set.source.agent_name}` : t(`shelfReview.sources.${set.source.type}`)} · ${set.source.import_batch_id || set.id}` }
async function allowLeave() {
  if (busy.value) return false
  if (!dirty.value) return true
  return await confirm({ header: t('shelfReview.unsaved'), body: t('shelfReview.unsavedHint'), confirmBtn: t('shelfReview.leave'), cancelBtn: t('common.cancel') }).result
}
async function choose(id: string) { if (id !== selected.value?.id && await allowLeave()) await select(id) }
async function changePage(next: number) {
  if (busy.value || loading.value || !await allowLeave()) return
  const query = { ...route.query }; query.review_page = String(next); delete query.changeset
  await router.replace({ query }); page.value = next; await load()
}
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value || busy.value) { event.preventDefault(); event.returnValue = '' } }
function acceptNew() { if (blocked.value) return; for (const item of groups.value.find(group => group.type === 'new')?.items || []) decide(item, 'apply') }
function actions(item: ShelfReviewItem): ShelfReviewDecision['action'][] {
  const type = shelfReviewGroup(item)
  if (type === 'disappeared') return ['retire', 'keep', 'defer']
  if (type === 'uncertain' && selected.value?.source.type === 'agent') return ['return_to_submitter', 'discard', 'defer']
  return [...(['new', 'modified'].includes(type) || item.operation.type === 'import_notice' ? ['apply' as const] : []), 'discard', 'defer']
}
async function writeConfirmation() {
  if (!canConfirm.value || blocked.value || !selected.value) return
  const accepted = await confirm({ header: t('shelfReview.confirm'), body: t('shelfReview.confirmHint', { count: selected.value.items.length }), confirmBtn: t('shelfReview.confirm'), cancelBtn: t('common.cancel') }).result
  if (!accepted || !selected.value || busy.value) return
  busy.value = true; error.value = ''
  try {
    const response = await confirmShelfChangeset(props.library.id, selected.value.id, { revision: selected.value.revision, decisions: Object.values(decisions.value) })
    if (!response.success) throw new Error(response.message)
    baseline.value = JSON.stringify(decisions.value); selected.value = undefined
    toast.success(t('shelfReview.confirmed'))
    const query = { ...route.query }; delete query.changeset
    if (response.data.deferred_changeset_id) query.changeset = response.data.deferred_changeset_id
    await router.replace({ query }); await load()
  } catch (cause) { error.value = failure(cause, t('shelfReview.confirmFailed')) }
  finally { busy.value = false }
}
function failure(cause: unknown, fallback: string) {
  const value = (cause && typeof cause === 'object' ? cause : {}) as { message?: string; status?: number; error?: { details?: ShelfReviewChangeset } }
  const conflicts = value.error?.details?.items?.flatMap(item => item.conflicts.map(conflict => conflict.message).filter(Boolean)) || []
  return [fallback, value.message, ...conflicts, ...(value.status === 409 ? [t('shelfReview.reloadHint')] : [])].filter(Boolean).join(' · ')
}
const processed = computed(() => selected.value?.items.filter(item => decisions.value[item.id]).length || 0)
async function select(id: string) {
  const current = ++detailGeneration; detailLoading.value = true; selected.value = undefined; error.value = ''; notice.value = ''
  try {
    const response = await getShelfChangeset(props.library.id, id)
    if (!response.success) throw new Error(response.message)
    if (current !== detailGeneration) return
    adopt(response.data)
    void router.replace({ query: { ...route.query, changeset: id } })
  } catch { if (current === detailGeneration) error.value = t('shelfReview.loadFailed') }
  finally { if (current === detailGeneration) detailLoading.value = false }
}
async function load() {
  const current = ++generation; loading.value = true; error.value = ''
  try {
    const response = await listShelfChangesets(props.library.id, page.value)
    if (!response.success) throw new Error(response.message)
    if (current !== generation) return
    sets.value = response.data.items; total.value = response.data.total
    if (!sets.value.length && page.value > 1) { page.value = Math.max(1, Math.ceil(total.value / 20)); await load(); return }
    const id = typeof route.query.changeset === 'string' ? route.query.changeset : sets.value[0]?.id
    if (id) await select(id)
  } catch { if (current === generation) error.value = t('shelfReview.loadFailed') }
  finally { if (current === generation) loading.value = false }
}
function decide(item: ShelfReviewItem, action: ShelfReviewDecision['action']) { if (blocked.value) return; decisions.value[item.id] = { item_id: item.id, action }; delete edits.value[item.id]; notice.value = '' }
async function save() {
  if (blocked.value || !selected.value) return
  for (const item of selected.value.items) if (edits.value[item.id]) applyEdit(item)
  busy.value = true; error.value = ''; notice.value = ''
  try {
    const response = await saveShelfReview(props.library.id, selected.value.id, { revision: selected.value.revision, decisions: Object.values(decisions.value) })
    if (!response.success) throw new Error(response.message)
    adopt(response.data); notice.value = t('shelfReview.saved')
  } catch (cause) { error.value = failure(cause, t('shelfReview.saveFailed')) }
  finally { busy.value = false }
}
watch(() => props.library.id, () => { generation++; detailGeneration++; selected.value = undefined; sets.value = []; decisions.value = {}; baseline.value = '{}'; page.value = Math.max(1, Number.parseInt(String(route.query.review_page || '1'), 10) || 1); void load() }, { immediate: true })
onBeforeRouteLeave(allowLeave)
onBeforeRouteUpdate((to, from) => to.query.libraries !== from.query.libraries || to.query.tab !== from.query.tab || (to.query.changeset && to.query.changeset !== from.query.changeset && to.query.changeset !== selected.value?.id) ? allowLeave() : true)
watch(() => route.query.changeset, id => { if (typeof id === 'string' && id !== selected.value?.id && !loading.value && !detailLoading.value) void select(id) })
if (typeof window !== 'undefined') window.addEventListener('beforeunload', beforeUnload)
onBeforeUnmount(() => { generation++; detailGeneration++; if (typeof window !== 'undefined') window.removeEventListener('beforeunload', beforeUnload) })
</script>
<template>
  <section class="shelf-review">
    <header class="review-heading"><h1>{{ t('shelfReview.title') }}</h1><Button :disabled="busy || loading || detailLoading" @click="async () => { if (await allowLeave()) await load() }">{{ t('common.refresh') }}</Button></header>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <Button v-if="error" :disabled="busy || loading || detailLoading" @click="async () => { if (await allowLeave()) await load() }">{{ t('shelfReview.reload') }}</Button>
    <EmptyState v-if="!loading && !error && !sets.length" :title="t('shelfReview.empty')" :description="t('shelfReview.emptyHint')" />
    <div v-if="sets.length || selected" class="review-split">
      <aside>
        <ul class="review-sets" :aria-label="t('shelfReview.title')"><li v-for="set in sets" :key="set.id"><button type="button" :aria-current="selected?.id === set.id ? 'true' : undefined" :disabled="busy || loading || detailLoading" @click="choose(set.id)"><strong>{{ title(set) }}</strong><span>{{ set.source.file_name }} {{ set.source.file_date ? formatDateTime(set.source.file_date.slice(0, 10), locale) : '' }}</span><span>{{ formatDateTime(set.created_at, locale, { dateStyle: 'short', timeStyle: 'short' }) }}</span><span>{{ t('shelfReview.processed', { count: set.id === selected?.id ? processed : set.items.filter(item => item.decision).length, total: set.items.length }) }}</span></button></li></ul>
        <nav v-if="total > 20" class="review-pages" :aria-label="t('shelfReview.pages')"><Button :disabled="busy || loading || page <= 1" @click="changePage(page - 1)">{{ t('follows.previousPage') }}</Button><span>{{ page }} / {{ Math.ceil(total / 20) }}</span><Button :disabled="busy || loading || page * 20 >= total" @click="changePage(page + 1)">{{ t('follows.nextPage') }}</Button></nav>
      </aside>
      <div v-if="detailLoading" role="status">{{ t('common.loading') }}</div>
      <div v-else-if="selected" class="review-detail">
        <h2>{{ title(selected) }}</h2>
        <div class="review-counts"><span v-for="group in groups" :key="group.type">{{ t(`shelfReview.groups.${group.type}`) }} {{ group.items.length }}</span></div>
        <details v-for="group in groups" :key="group.type" class="review-group" :open="group.items.length > 0">
          <summary><h3>{{ t(`shelfReview.groups.${group.type}`) }}</h3><span>{{ t('shelfReview.groupCount', { count: group.items.length, remaining: group.items.filter(item => !decisions[item.id]).length }) }}</span></summary>
          <Button v-if="group.type === 'new' && group.items.length" :disabled="blocked" class="review-bulk" @click="acceptNew">{{ t('shelfReview.acceptGroup') }}</Button>
          <article v-for="item in group.items" :key="item.id" :aria-label="shelfReviewSubject(item).name">
            <div class="review-what">
              <div class="review-object"><span v-if="shelfReviewSubject(item).kind" class="review-kind">{{ library.entity_schema?.kinds?.find(kind => kind.key === shelfReviewSubject(item).kind)?.name || shelfReviewSubject(item).kind }}</span><strong>{{ shelfReviewSubject(item).name || t('shelfReview.unnamed') }}</strong></div>
              <p v-for="(conflict, index) in item.conflicts" :key="index">{{ conflict.message }}</p>
              <p v-if="group.type === 'disappeared'">{{ t('shelfReview.disappearedHint') }}</p>
              <p v-for="candidate in shelfNameConfirmations(item)" :key="candidate.other_entity_id">{{ candidate.name }} · {{ library.entity_schema?.kinds?.find(kind => kind.key === candidate.other_kind)?.name || candidate.other_kind }} · {{ candidate.other_entity_id }}</p>
              <details class="review-diff"><summary>{{ t('shelfReview.compare') }}</summary><div><strong>{{ t('shelfReview.before') }}</strong><ShelfReviewValue :value="item.before" :library="library" readonly /></div><div><strong>{{ t('shelfReview.proposed') }}</strong><ShelfReviewValue :value="decisions[item.id]?.replacement || item.after || item.operation" :library="library" readonly /></div><div v-for="(conflict, index) in item.conflicts.filter(conflict => conflict.manual_before)" :key="index"><strong>{{ t('shelfReview.manual') }}</strong><ShelfReviewValue :value="conflict.manual_before" :library="library" readonly /></div><div v-if="item.operation.import_location?.values"><strong>{{ t('shelfReview.original') }}</strong><ShelfReviewValue :value="item.operation.import_location.values" :library="library" readonly /></div></details>
              <small v-if="item.operation.import_location">{{ selected.source.file_name }} › {{ item.operation.import_location.worksheet }} › {{ item.operation.import_location.cells?.join('、') }}</small>
              <p v-if="decisions[item.id]?.action === 'return_to_submitter'" class="review-returned">{{ t('shelfReview.returnedHint') }}</p>
            </div>
            <div class="review-actions" role="group" :aria-label="t('shelfReview.handle', { name: shelfReviewSubject(item).name })">
              <template v-if="group.type === 'suspected_same'"><Select v-if="candidates(item).length > 1" :model-value="targets[item.id] || ''" :aria-label="t('shelfReview.candidate')" :disabled="blocked" @update:model-value="targets[item.id] = $event"><option value="">{{ t('shelfReview.chooseCandidate') }}</option><option v-for="candidate in candidates(item)" :key="candidate.other_entity_id" :value="candidate.other_entity_id">{{ candidate.name }} · {{ candidate.other_kind }} · {{ candidate.other_entity_id }}</option></Select><Button :disabled="blocked || (!targets[item.id] && candidates(item).length !== 1)" :aria-pressed="decisions[item.id]?.action === 'same_entity'" @click="sameEntity(item)">{{ t('shelfReview.actions.same_entity') }}</Button><Button :disabled="blocked" @click="distinctEntity(item)">{{ t('shelfReview.distinct') }}</Button></template>
              <Button v-if="group.type === 'cross_kind_same_name'" :disabled="blocked || !shelfNameConfirmations(item).length" :aria-pressed="!!decisions[item.id]?.name_confirmations?.length" @click="confirmNames(item)">{{ t('shelfReview.confirmNames') }}</Button>
              <Button v-for="action in actions(item)" :key="action" :disabled="blocked" :aria-pressed="decisions[item.id]?.action === action" @click="decide(item, action)">{{ item.operation.type === 'import_notice' && action === 'apply' ? t('shelfReview.acknowledge') : t(`shelfReview.actions.${action}`) }}</Button><Button v-if="item.operation.type !== 'import_notice'" :disabled="blocked" :aria-pressed="!!edits[item.id] || decisions[item.id]?.action === 'replace'" @click="edit(item)">{{ t('shelfReview.actions.replace') }}</Button>
            </div>
            <form v-if="edits[item.id]" class="review-edit" novalidate @submit.prevent="applyEdit(item)"><fieldset :disabled="blocked"><legend>{{ t('shelfReview.actions.replace') }}</legend><ShelfReviewValue :value="edits[item.id]" :library="library" @change="edits[item.id] = $event as ShelfReviewOperation" /><div class="review-footer-actions"><Button type="submit">{{ t('shelfReview.applyEdit') }}</Button><Button @click="delete edits[item.id]">{{ t('common.cancel') }}</Button></div></fieldset></form>
          </article>
        </details>
        <footer><div><p v-if="required" class="review-blocked">{{ t('shelfReview.required', { count: required }) }}</p><span>{{ t('shelfReview.processed', { count: processed, total: selected.items.length }) }}<template v-if="dirty"> · {{ t('shelfReview.unsaved') }}</template></span><p v-if="!canConfirm && !required" class="review-hint">{{ t('shelfReview.resolveHint') }}</p><p v-if="notice" role="status">{{ notice }}</p></div><div class="review-footer-actions"><Button :disabled="blocked" :loading="busy" @click="save">{{ t('shelfReview.save') }}</Button><Button variant="primary" :disabled="blocked || !canConfirm" @click="writeConfirmation">{{ t('shelfReview.confirm') }}</Button></div></footer>
      </div>
    </div>
  </section>
</template>
<style scoped>
.shelf-review { padding:var(--space-5) var(--space-6) var(--space-7); display:flex; flex-direction:column; gap:var(--space-4); min-width:0; }
.review-heading { display:flex; align-items:end; justify-content:space-between; gap:var(--space-3); }
.review-heading h1 { margin:0; font:800 var(--type-page-size)/1 var(--font); letter-spacing:-0.04em; }
.review-split { display:grid; grid-template-columns:304px minmax(0,1fr); gap:var(--space-5); align-items:start; }
.review-sets { list-style:none; margin:0; padding:0; border-top:2px solid var(--rule); }
.review-sets button { width:100%; display:grid; gap:var(--space-1); padding:var(--space-3); border:0; border-bottom:1px solid var(--border); background:transparent; color:var(--text); text-align:left; cursor:pointer; overflow-wrap:anywhere; }
.review-sets button:hover { background:var(--surface-soft); }
.review-sets button[aria-current=true] { background:var(--select); color:var(--on-select); }
.review-sets span { font-size:var(--text-xs); }
.review-detail { min-width:0; display:grid; gap:var(--space-4); }
.review-detail h2 { margin:0; font-size:var(--text-xl); overflow-wrap:anywhere; }
.review-counts { display:flex; flex-wrap:wrap; gap:var(--space-1); }
.review-counts span { padding:2px var(--space-2); background:var(--surface-soft); font-size:var(--text-xs); font-weight:700; }
.review-group { border-top:2px solid var(--rule); }
.review-group > summary { display:flex; align-items:baseline; gap:var(--space-3); padding:var(--space-3) 0; cursor:pointer; list-style:none; }
.review-group > summary::-webkit-details-marker { display:none; }
.review-group > summary::before { content:'+'; font-weight:800; width:16px; }
.review-group[open] > summary::before { content:'−'; }
.review-group h3 { margin:0; font-size:var(--text-lg); }
.review-group summary span { font-size:var(--text-sm); }
.review-bulk { margin:0 0 var(--space-3) 28px; }
article { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:var(--space-2) var(--space-4); border-bottom:1px solid var(--border); padding:var(--space-3) 0 var(--space-3) 28px; }
article:last-child { border:0; }
.review-what { display:grid; gap:var(--space-1); min-width:0; overflow-wrap:anywhere; }
.review-what p { margin:0; }
.review-object { display:flex; flex-wrap:wrap; align-items:center; gap:var(--space-2); }
.review-object strong { font:700 var(--text-sm)/var(--leading) var(--mono); }
.review-kind { border:1px solid var(--text); padding:0 4px; font-size:var(--text-xs); font-weight:700; }
.review-what small, .review-hint { color:var(--muted); font-size:var(--text-xs); }
.review-actions { display:flex; flex-wrap:wrap; align-self:start; justify-content:end; max-width:360px; }
.review-actions :deep(button) { min-height:28px; padding:2px var(--space-2); font-size:var(--text-xs); margin:0 -1px -1px 0; }
.review-actions :deep(button[aria-pressed=true]) { background:var(--text); color:var(--bg); }
footer { position:sticky; bottom:0; z-index:1; background:var(--bg); display:flex; flex-wrap:wrap; justify-content:space-between; gap:var(--space-3); border-top:2px solid var(--rule); padding:var(--space-3) 0; }
footer p { margin:0 0 var(--space-1); }
.review-footer-actions, .review-pages { display:flex; align-items:center; gap:var(--space-2); }
.review-pages { flex-wrap:wrap; padding-top:var(--space-3); }
[role=alert], .review-blocked { color:var(--danger); }
.review-blocked, .review-returned { font-weight:700; }
.review-edit { grid-column:1 / -1; }
.review-edit > fieldset { margin:0; padding:var(--space-3); border:1px solid var(--border); display:grid; gap:var(--space-3); min-width:0; }
.review-diff { font-size:var(--text-xs); }
.review-diff > summary { cursor:pointer; }
.review-diff > div { margin-top:var(--space-2); padding:var(--space-2); background:var(--surface-soft); }
</style>
