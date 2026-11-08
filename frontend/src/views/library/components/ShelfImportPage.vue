<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter, onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { Button, Checkbox, EmptyState, Field, Input, Select } from '@/components/ui'
import { confirm, toast } from '@/components/ui/feedback'
import { formatDateTime } from '@/utils/dateTime'
import type { LibraryCatalogItem } from '@/api/library'
import { listEntities, type Entity } from '@/api/entities'
import { confirmShelfChangeset, type ShelfReviewChangeset } from '@/api/shelf-review'
import { IMPORT_KINDS, listShelfImportPlans, listShelfImportBatches, uploadShelfImport, saveShelfImportPlan, suggestShelfImportPlan, type ShelfImportPlan, type ShelfImportPlanInput } from '@/api/shelf-import'
import ShelfImportPlanEditor from './ShelfImportPlanEditor.vue'

const props = defineProps<{ library: LibraryCatalogItem }>()
const { t, locale } = useI18n(), route = useRoute(), router = useRouter()
const plans = ref<ShelfImportPlan[]>([]), batches = ref<ShelfReviewChangeset[]>([]), lines = ref<Entity[]>([])
const selected = ref(''), file = ref<File>(), fileDate = ref(''), picker = ref<HTMLInputElement>()
const ready = ref(false), editorKey = ref(0), submitted = ref(false), formElement = ref<HTMLFormElement>()
const loading = ref(false), busy = ref(false), dragging = ref(false), error = ref(''), fileError = ref(''), formError = ref('')
const draft = ref<ShelfImportPlanInput>(), baseline = ref(''), suggestion = ref<ShelfReviewChangeset>(), reviewed = ref(false), editorValid = ref(true)
const page = ref(Math.max(1, Number.parseInt(String(route.query.import_page || '1'), 10) || 1)), total = ref(0)
let generation = 0
const dirty = computed(() => !!draft.value && JSON.stringify(draft.value) !== baseline.value)
const blocked = computed(() => loading.value || busy.value)
const savedPlan = computed(() => plans.value.find(value => value.key === selected.value))
const canImport = computed(() => !!file.value && !!savedPlan.value && !dirty.value && !suggestion.value && !blocked.value && ready.value)
const suggestionIssues = computed(() => suggestion.value?.items.flatMap(item => item.conflicts.map(conflict => [conflict.message, typeof conflict.details === 'string' ? conflict.details : Array.isArray(conflict.details) ? conflict.details.filter(value => typeof value === 'string').join(' · ') : ''].filter(Boolean).join(' · '))) || [])
function failure(cause: unknown, fallback: string) {
  const value = cause as { message?: string; status?: number; error?: { details?: ShelfReviewChangeset } }
  const messages = value?.error?.details?.items?.flatMap(item => item.conflicts.map(conflict => conflict.message).filter(Boolean)) || []
  return [fallback, value?.message, ...messages, ...(value?.status === 409 ? [t('shelfImport.staleHint')] : [])].filter(Boolean).join(' · ')
}
async function allowLeave() {
  if (busy.value) return false
  if (!dirty.value) return true
  return await confirm({ header: t('shelfImport.unsaved'), body: t('shelfImport.unsavedHint'), confirmBtn: t('shelfImport.leave'), cancelBtn: t('common.cancel') }).result
}
function chooseFile(files: File[]) {
  if (blocked.value) return
  fileError.value = ''
  if (files.length !== 1 || !files[0] || !/\.xlsx$/i.test(files[0].name) || files[0].size === 0 || files[0].size > 32 * 1024 * 1024) {
    fileError.value = t('shelfImport.fileError'); return
  }
  file.value = files[0]; fileDate.value = ''; error.value = ''
}
function choose(event: Event) { const input = event.target as HTMLInputElement; if (input.files?.length) chooseFile(Array.from(input.files)); input.value = '' }
function drop(event: DragEvent) { dragging.value = false; chooseFile(Array.from(event.dataTransfer?.files || [])) }
function copyPlan(value: ShelfImportPlanInput): ShelfImportPlanInput {
  const copy: ShelfImportPlanInput = JSON.parse(JSON.stringify(value))
  copy.sheets.forEach(sheet => { sheet.columns ||= [] })
  return copy
}
async function open(plan?: ShelfImportPlan) {
  if (blocked.value || !await allowLeave()) return
  suggestion.value = undefined; reviewed.value = false; formError.value = ''; error.value = ''; editorValid.value = true
  draft.value = plan ? copyPlan({ ...plan.definition, key: plan.key, name: plan.name, file_kind: plan.file_kind, revision: plan.revision }) : { key: '', name: '', file_kind: 'product_map', revision: 0, sheets: [] }
  editorKey.value++; submitted.value = false
  if (plan) selected.value = plan.key
  baseline.value = JSON.stringify(draft.value)
}
async function close() { if (await allowLeave()) { draft.value = undefined; suggestion.value = undefined; formError.value = ''; error.value = '' } }
function changeKind() { editorKey.value++; if (draft.value) { draft.value.sheets = []; draft.value.deadlines = {}; draft.value.status_values = {}; draft.value.fill_meanings = {} } }
function validate() {
  const value = draft.value
  if (!value || !/^[a-z][a-z0-9_]{0,63}$/.test(value.key) || !value.name.trim() || !value.sheets.length || !editorValid.value) return t('shelfImport.planError')
  if (value.sheets.some(sheet => !sheet.worksheet || !Number.isInteger(sheet.header_rows) || sheet.header_rows < 0 || sheet.header_rows > 100 || (!sheet.ignore && (!sheet.columns.length || sheet.columns.some(column => !/^[A-Z]{1,3}$/.test(column.column) || !column.field))))) return t('shelfImport.planError')
  if (suggestion.value && !reviewed.value) return t('shelfImport.reviewRequired')
  return ''
}
async function save() {
  if (blocked.value || !draft.value) return
  formError.value = validate()
  submitted.value = true
  if (formError.value) {
    await nextTick(); (formElement.value?.querySelector('[aria-invalid=true]') as HTMLElement | null)?.focus(); return
  }
  busy.value = true; error.value = ''
  try {
    const response = suggestion.value
      ? await confirmShelfChangeset(props.library.id, suggestion.value.id, { revision: suggestion.value.revision, decisions: [{ item_id: suggestion.value.items[0]!.id, action: 'replace', replacement: { ...suggestion.value.items[0]!.operation, import_plan: draft.value, issues: [] } }] })
      : await saveShelfImportPlan(props.library.id, draft.value)
    if (!response.success || response.data.status !== 'confirmed') throw new Error(response.message || t('shelfImport.saveFailed'))
    const key = draft.value.key
    baseline.value = JSON.stringify(draft.value); suggestion.value = undefined; draft.value = undefined; selected.value = key
    toast.success(t('shelfImport.saved')); await load()
  } catch (cause) { error.value = failure(cause, t('shelfImport.saveFailed')) }
  finally { busy.value = false }
}
async function suggest() {
  if (blocked.value || !file.value || !draft.value || draft.value.revision !== 0) return
  if (!/^[a-z][a-z0-9_]{0,63}$/.test(draft.value.key)) { formError.value = t('shelfImport.keyHint'); return }
  if (dirty.value && draft.value.sheets.length && !await allowLeave()) return
  busy.value = true; error.value = ''; formError.value = ''
  try {
    const response = await suggestShelfImportPlan(props.library.id, file.value, draft.value.key, draft.value.file_kind)
    if (!response.success) throw new Error(response.message)
    const item = response.data.items[0]
    if (response.data.items.length !== 1 || item?.operation.type !== 'save_import_plan' || !item.operation.import_plan) throw new Error(t('shelfImport.suggestFailed'))
    suggestion.value = response.data; draft.value = copyPlan(item.operation.import_plan as ShelfImportPlanInput); reviewed.value = false
    // A model proposal is unsaved work until the reviewer explicitly saves it.
    baseline.value = ''; editorValid.value = true; editorKey.value++; submitted.value = false
  } catch (cause) { error.value = failure(cause, t('shelfImport.suggestFailed')) }
  finally { busy.value = false }
}
async function review(id: string) { await router.replace({ query: { ...route.query, tab: 'review', changeset: id } }) }
async function upload() {
  if (!canImport.value || !file.value) return
  if (fileDate.value && (!/^\d{4}-\d{2}-\d{2}$/.test(fileDate.value) || Number.isNaN(Date.parse(`${fileDate.value}T00:00:00Z`)) || new Date(`${fileDate.value}T00:00:00Z`).toISOString().slice(0, 10) !== fileDate.value)) { fileError.value = t('shelfImport.dateError'); return }
  busy.value = true; error.value = ''; fileError.value = ''
  try {
    const response = await uploadShelfImport(props.library.id, file.value, selected.value, fileDate.value ? `${fileDate.value}T00:00:00Z` : '')
    if (!response.success) throw new Error(response.message)
    draft.value = undefined; suggestion.value = undefined
    // Release the navigation guard only after the mutation has completed.
    busy.value = false; await review(response.data.id)
  } catch (cause) { error.value = failure(cause, t('shelfImport.uploadFailed')) }
  finally { busy.value = false }
}
async function loadLines() {
  const result: Entity[] = []
  for (let offset = 0; ; offset += 100) {
    const response = await listEntities({ library_id: props.library.id, kind: 'product_line', limit: 100, offset })
    if (!response.success) throw new Error(t('shelfImport.loadFailed'))
    result.push(...response.data)
    if (response.data.length < 100) return result
  }
}
async function load() {
  const current = ++generation; loading.value = true; ready.value = false; error.value = ''
  try {
    const [planResult, batchResult, lineResult] = await Promise.all([listShelfImportPlans(props.library.id), listShelfImportBatches(props.library.id, page.value), loadLines()])
    if (!planResult.success || !batchResult.success) throw new Error(t('shelfImport.loadFailed'))
    if (current !== generation) return
    ready.value = true; plans.value = planResult.data; batches.value = batchResult.data.items; total.value = batchResult.data.total; lines.value = lineResult
    if (page.value > 1 && !batches.value.length) { await changePage(Math.max(1, Math.ceil(total.value / 20))); return }
    if (selected.value && !plans.value.some(plan => plan.key === selected.value)) selected.value = ''
  } catch (cause) { if (current === generation) error.value = failure(cause, t('shelfImport.loadFailed')) }
  finally { if (current === generation) loading.value = false }
}
async function changePage(next: number) { page.value = next; await router.replace({ query: { ...route.query, import_page: String(next) } }); await load() }
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value || busy.value) { event.preventDefault(); event.returnValue = '' } }
watch(() => props.library.id, () => { generation++; plans.value = []; batches.value = []; lines.value = []; draft.value = undefined; file.value = undefined; selected.value = ''; void load() }, { immediate: true })
onBeforeRouteLeave(allowLeave)
onBeforeRouteUpdate((to, from) => to.query.libraries !== from.query.libraries || to.query.tab !== from.query.tab ? allowLeave() : true)
if (typeof window !== 'undefined') window.addEventListener('beforeunload', beforeUnload)
onBeforeUnmount(() => { generation++; if (typeof window !== 'undefined') window.removeEventListener('beforeunload', beforeUnload) })
</script>
<template>
  <section class="shelf-import">
    <header class="import-row spread"><h1>{{ t('shelfImport.title') }}</h1><Button :disabled="blocked || dirty" @click="load">{{ t('common.refresh') }}</Button></header>
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <div class="import-drop" :class="{ dragging }" @dragover.prevent="dragging = true" @dragleave.prevent="dragging = false" @drop.prevent="drop">
      <strong>{{ t('shelfImport.dropHint') }}</strong>
      <input ref="picker" class="file-picker" type="file" accept=".xlsx" :aria-label="t('shelfImport.chooseFile')" :disabled="blocked" @change="choose">
      <Button :disabled="blocked" @click="picker?.click()">{{ t('shelfImport.chooseFile') }}</Button>
      <p class="import-hint">{{ t('shelfImport.fileHint') }}</p>
      <div v-if="file" class="import-row" role="status"><strong>{{ file.name }}</strong><Button :disabled="blocked" @click="file = undefined; fileDate = ''">{{ t('shelfImport.removeFile') }}</Button></div>
    </div>
    <p v-if="fileError" role="alert">{{ fileError }}</p>
    <div v-if="file" class="import-row upload-fields">
      <Field :label="t('shelfImport.plan')"><Select v-model="selected" :aria-label="t('shelfImport.plan')" :disabled="blocked || !!draft"><option value="">{{ t('shelfImport.choosePlan') }}</option><option v-for="plan in plans" :key="plan.key" :value="plan.key">{{ plan.name }} · v{{ plan.revision }}</option></Select></Field>
      <Field :label="t('shelfImport.fileDate')" :hint="t('shelfImport.dateHint')"><Input v-model="fileDate" type="date" :aria-label="t('shelfImport.fileDate')" :disabled="blocked" /></Field>
      <Button :disabled="!canImport" :loading="busy && !draft" @click="upload">{{ t('shelfImport.tryImport') }}</Button>
    </div>
    <p v-if="error" role="alert">{{ error }}</p>
    <Button v-if="error && !draft" :disabled="blocked" @click="load">{{ t('common.retry') }}</Button>
    <form v-if="draft" ref="formElement" novalidate class="import-section" @submit.prevent="save">
      <div class="import-row spread"><h2>{{ t('shelfImport.plan') }}<span v-if="draft.revision"> · v{{ draft.revision }}</span></h2><Button :disabled="blocked" @click="close">{{ t('shelfImport.back') }}</Button></div>
      <fieldset :disabled="blocked" class="plan-identity import-row">
        <Field :label="t('shelfImport.planName')" :error="submitted && !draft.name.trim() ? t('shelfImport.planError') : undefined"><Input v-model="draft.name" :aria-label="t('shelfImport.planName')" /></Field>
        <Field :label="t('shelfImport.planKey')" :hint="t('shelfImport.keyHint')" :error="submitted && !/^[a-z][a-z0-9_]{0,63}$/.test(draft.key) ? t('shelfImport.keyHint') : undefined"><Input v-model="draft.key" :aria-label="t('shelfImport.planKey')" :disabled="draft.revision > 0 || !!suggestion" /></Field>
        <Field :label="t('shelfImport.fileKind')"><Select v-model="draft.file_kind" :aria-label="t('shelfImport.fileKind')" :disabled="draft.revision > 0 || !!suggestion" @update:model-value="changeKind"><option v-for="kind in IMPORT_KINDS" :key="kind" :value="kind">{{ t(`shelfImport.kinds.${kind}`) }}</option></Select></Field>
      </fieldset>
      <div v-if="draft.revision === 0 && !suggestion" class="import-row"><Button :disabled="blocked || !file" :loading="busy" @click="suggest">{{ t('shelfImport.suggest') }}</Button><p class="import-hint">{{ t('shelfImport.suggestHint') }}</p></div>
      <div v-if="suggestion" class="suggestion-note"><strong>{{ t('shelfImport.suggestionPending') }}</strong><p v-for="(issue, index) in suggestionIssues" :key="index">{{ issue }}</p><label class="import-check"><Checkbox v-model="reviewed" :disabled="blocked" />{{ t('shelfImport.reviewed') }}</label></div>
      <ShelfImportPlanEditor :key="editorKey" v-model="draft" :library="library" :lines="lines" :disabled="blocked" :submitted="submitted" @validity="editorValid = $event" />
      <p v-if="formError" id="import-plan-error" role="alert">{{ formError }}</p>
      <footer class="import-row"><Button type="submit" variant="primary" :disabled="blocked || !editorValid || (!!suggestion && !reviewed)" :loading="busy">{{ t('shelfImport.save') }}</Button><Button :disabled="!canImport" @click="upload">{{ t('shelfImport.tryImport') }}</Button><span v-if="dirty" class="import-hint">{{ t('shelfImport.saveBeforeImport') }}</span></footer>
    </form>
    <template v-else>
      <section class="import-section"><div class="import-row spread"><h2>{{ t('shelfImport.plans') }}</h2><Button :disabled="blocked" @click="open()">{{ t('shelfImport.newPlan') }}</Button></div>
        <EmptyState v-if="!loading && !error && !plans.length" :title="t('shelfImport.noPlans')" :description="t('shelfImport.noPlansHint')" />
        <div v-else class="import-table"><table><thead><tr><th>{{ t('shelfImport.fileKind') }}</th><th>{{ t('shelfImport.lastFile') }}</th><th>{{ t('shelfImport.state') }}</th><th>{{ t('shelfImport.actions') }}</th></tr></thead><tbody><tr v-for="plan in plans" :key="plan.key"><td>{{ t(`shelfImport.kinds.${plan.file_kind}`) }}</td><td>{{ plan.last_file_name || t('shelfImport.noFileYet') }}<small class="plan-caption">{{ plan.name }}</small></td><td>{{ t('shelfImport.confirmedPlan', { revision: plan.revision }) }}</td><td><Button variant="ghost" :disabled="blocked" @click="open(plan)">{{ t('shelfImport.view') }}</Button></td></tr></tbody></table></div>
      </section>
      <section class="import-section"><h2>{{ t('shelfImport.batches') }}</h2><EmptyState v-if="!loading && !error && !batches.length" :title="t('shelfImport.noBatches')" />
        <div v-else class="import-table"><table><thead><tr><th>{{ t('shelfImport.batch') }}</th><th>{{ t('shelfImport.file') }}</th><th>{{ t('shelfImport.time') }}</th><th>{{ t('shelfImport.effective') }}</th><th>{{ t('shelfImport.state') }}</th></tr></thead><tbody><tr v-for="batch in batches" :key="batch.id"><td class="mono" :title="batch.id">{{ batch.id.slice(0, 8) }}</td><td>{{ batch.source.file_name }}</td><td>{{ formatDateTime(batch.created_at, locale, { dateStyle: 'medium', timeStyle: 'short' }) }}</td><td>{{ formatDateTime(batch.source.file_date, locale, { dateStyle: 'medium', timeStyle: 'short', timeZone: 'UTC' }) }}</td><td><Button variant="ghost" :disabled="blocked" @click="review(batch.id)">{{ batch.status === 'confirmed' ? t('shelfImport.confirmed') : t('shelfImport.pending', { count: batch.items.filter(item => !item.decision).length }) }}</Button></td></tr></tbody></table></div>
        <nav v-if="total > 20" class="import-row" :aria-label="t('shelfImport.pages')"><Button :disabled="blocked || page === 1" @click="changePage(page - 1)">{{ t('shelfImport.previous') }}</Button><span>{{ page }} / {{ Math.ceil(total / 20) }}</span><Button :disabled="blocked || page * 20 >= total" @click="changePage(page + 1)">{{ t('shelfImport.next') }}</Button></nav>
      </section>
    </template>
  </section>
</template>
<style scoped>
.shelf-import { display: grid; align-content: start; gap: var(--space-5); min-width: 0; padding: var(--space-5) var(--space-6) var(--space-7); }
h1, h2, p { margin: 0; } h1 { font-size: var(--text-3xl); font-weight: 800; letter-spacing: -0.035em; } h2 { font-size: var(--text-xl); }
.import-row { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); }.spread { justify-content: space-between; }
.import-drop { display: flex; flex-direction: column; align-items: center; gap: var(--space-2); border: 2px dashed var(--rule); padding: var(--space-6); text-align: center; }.import-drop.dragging { background: var(--surface-soft); }
.file-picker { display: none; }.import-hint { color: var(--muted); font-size: var(--text-sm); }
.upload-fields, .plan-identity { align-items: end; }.upload-fields > :deep(.rq-field), .plan-identity > :deep(.rq-field) { flex: 1 1 220px; }
.plan-identity { border: 0; margin: 0; padding: 0; min-width: 0; }.import-section { display: grid; gap: var(--space-3); min-width: 0; }
.import-table { overflow: auto; border-top: 2px solid var(--rule); }table { border-collapse: collapse; width: 100%; min-width: 700px; }th, td { text-align: left; padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--border); }th { font-size: var(--text-xs); }td { overflow-wrap: anywhere; }tbody tr:hover { background: var(--surface-soft); }
.plan-caption { display: block; color: var(--muted); font-size: var(--text-xs); margin-top: var(--space-1); }.mono { font-family: var(--mono); }.suggestion-note { background: var(--surface-soft); border-left: 3px solid var(--rule); display: grid; gap: var(--space-2); padding: var(--space-4); overflow-wrap: anywhere; }.import-check { display: flex; align-items: center; gap: var(--space-2); }
footer { padding-block: var(--space-3); border-top: 2px solid var(--rule); }
</style>
