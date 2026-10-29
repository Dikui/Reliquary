<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { read, write, remove, type Answer, type EvaluationSet, type Question, type SearchRecord, type EvaluationRun, type Comparison, type VectorSet } from '@/api/insights'
import QuestionEditor from './QuestionEditor.vue'
import EntryPicker from './EntryPicker.vue'
import RunResults from './RunResults.vue'
import LibraryScopeSelect from '@/components/LibraryScopeSelect.vue'
import { Button, Dialog, Tabs, EmptyState, Tag, confirm as confirmAction, toast } from '@/components/ui'
import './diagnostics.css'
import './diagnostics-b.css'
const { t } = useI18n(), route = useRoute()
const sets = ref<EvaluationSet[]>([]), libraries = ref<{ id: string; name: string }[]>([]), models = ref<{ id: string; name: string; type: string }[]>([])
const selected = ref(''), tab = ref('runs'), status = ref('confirmed'), loading = ref(true), busy = ref(false), error = ref('')
const questions = ref<Question[]>([]), records = ref<SearchRecord[]>([]), runs = ref<EvaluationRun[]>([]), vectors = ref<VectorSet[]>([])
const setOffset = ref(0), questionOffset = ref(0), recordOffset = ref(0), runOffset = ref(0)
const setForm = ref<{ id?: string; name: string; description: string }>(), editing = ref<Question | null | undefined>(undefined)
const generating = ref(false), editorBusy = ref(false)
const tabs = computed(() => [{ value: 'runs', label: t('ikb19.runs'), disabled: busy.value }, { value: 'questions', label: t('ikb19.questions'), disabled: busy.value }, { value: 'records', label: t('ikb19.records'), disabled: busy.value }])
const shortId = (id: string) => id.length > 12 ? id.slice(0, 8) : id
const chosenRecord = ref<SearchRecord>(), recordAnswers = ref<Answer[]>([])
const candidateLibraries = ref<string[]>([]), candidateEntries = ref<Answer[]>([]), candidateCount = ref(5), candidateModel = ref('')
const effectiveCandidateLibraries = computed(() => candidateLibraries.value.length ? candidateLibraries.value : libraries.value.map(lib => lib.id))
const filters = ref({ source: '', method: '', library: '', query: '', from: '', to: '', include_evaluation: false })
const k = ref(10), vector = ref('active'), collapse = ref(false), rerank = ref('library')
const detail = ref<EvaluationRun>(), comparison = ref<Comparison>(), before = ref(''), after = ref('')
let version = 0
let initializing = true
const selectedSet = computed(() => sets.value.find(s => s.id === selected.value))
const comparisonRuns = ref<Record<string, EvaluationRun>>({})
const completedRuns = computed(() => Object.values(comparisonRuns.value))
function libraryNames(ids: string[]) { return ids.map(id => libraries.value.find(l => l.id === id)?.name || id).join(', ') }
function vectorName(v: VectorSet) { return `${v.selector === 'active' ? t('ikb19.current') : v.selector.startsWith('candidate:') ? t('ikb19.candidate') : t('ikb19.archive')} · ${v.model_id || t('ikb19.libraryDefault')} ${v.dimension || ''}` }
function runName(r: EvaluationRun) { return `${new Date(r.created_at).toLocaleString()} · K=${r.options.k} · ${r.id.slice(0, 8)}` }
function timestamp(value: string) { return value ? new Date(value).toISOString() : undefined }
async function loadSets() { sets.value = await read<EvaluationSet[]>('evaluation/sets', { limit: 50, offset: setOffset.value }) }
async function loadPane() {
  const ticket = ++version, setId = selected.value, pane = tab.value
  loading.value = true; error.value = ''
  if (pane === 'questions') questions.value = []
  if (pane === 'records') records.value = []
  if (pane === 'runs') runs.value = []
  try {
    if (pane === 'questions') { const rows = setId ? await read<Question[]>(`evaluation/sets/${setId}/questions`, { status: status.value, limit: 50, offset: questionOffset.value }) : []; if (ticket === version) questions.value = rows }
    if (pane === 'records') { const rows = await read<SearchRecord[]>('search/records', { ...filters.value, from: timestamp(filters.value.from), to: timestamp(filters.value.to), limit: 50, offset: recordOffset.value }); if (ticket === version) records.value = rows }
    if (pane === 'runs') {
      const [rows, available] = await Promise.all([read<EvaluationRun[]>('evaluation/runs', { set_id: setId, limit: 50, offset: runOffset.value }), read<VectorSet[]>('evaluation/vector-sets')])
      if (ticket === version) { runs.value = rows; for (const run of rows) { if (run.status === 'completed' && run.set_id === setId) comparisonRuns.value[run.id] = run }; vectors.value = available; if (!available.some(v => v.selector === vector.value)) vector.value = available[0]?.selector || '' }
    }
  } catch (e: any) { if (ticket === version) error.value = e?.message || t('ikb19.loadFailed') }
  finally { if (ticket === version) loading.value = false }
}
async function initialize() {
  initializing = true
  loading.value = true; error.value = ''
  try {
    const [, libs, mods] = await Promise.all([loadSets(), read<{ id: string; name: string }[]>('libraries'), read<{ id: string; name: string; type: string }[]>('models')])
    libraries.value = libs; models.value = mods
    if (!selected.value) selected.value = sets.value[0]?.id || ''
    await nextTick()
    if (typeof route.query.run === 'string') await openRun(route.query.run)
    else await loadPane()
  } catch (e: any) { error.value = e?.message || t('ikb19.loadFailed') }
  finally { loading.value = false; initializing = false }
}
async function action(work: () => Promise<void>, running = false) {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { await work(); return true }
  catch (e: any) { error.value = running && e?.status === 503 ? t('ikb19.graphUnavailable') : e?.message || t('ikb19.actionFailed'); return false }
  finally { busy.value = false }
}
async function saveSet() {
  const draft = setForm.value
  if (!draft?.name.trim()) return
  await action(async () => {
    const saved = await write<EvaluationSet>(`evaluation/sets${draft.id ? `/${draft.id}` : ''}`, { name: draft.name.trim(), description: draft.description }, !!draft.id)
    setForm.value = undefined; setOffset.value = 0; await loadSets(); selected.value = saved.id; toast.success(t('ikb19.saved')); await loadPane()
  })
}
function requestDelete(path: string, label: string, kind: 'set' | 'question') {
  confirmAction({ header: t('ikb19.deleteConfirm'), body: `${label}. ${t(kind === 'set' ? 'ikb19.deleteSetHint' : 'ikb19.deleteQuestionHint')}`, confirmBtn: t('ikb19.confirmDelete'), cancelBtn: t('ikb19.cancel'), theme: 'danger',
    onConfirm: async () => {
      const completed = await action(async () => {
        await remove(path)
        if (kind === 'set') { await loadSets(); selected.value = sets.value[0]?.id || '' }
        await loadPane()
      })
      if (!completed) throw new Error(error.value)
    },
  })
}
function chooseRecord(record: SearchRecord) { chosenRecord.value = record; recordAnswers.value = [] }
function toggleRecordAnswer(id: string) { recordAnswers.value = recordAnswers.value.some(a => a.entry_id === id) ? recordAnswers.value.filter(a => a.entry_id !== id) : [...recordAnswers.value, { entry_id: id, grade: 1 }] }
async function questionFromRecord() {
  if (!chosenRecord.value || !selected.value) return
  await action(async () => { await write(`evaluation/sets/${selected.value}/from-record`, { record_id: chosenRecord.value!.id, answers: recordAnswers.value }); chosenRecord.value = undefined; tab.value = 'questions'; status.value = 'confirmed'; questionOffset.value = 0; toast.success(t('ikb19.recordCreated')); await loadPane() })
}
async function generate() {
  if (!selected.value || !effectiveCandidateLibraries.value.length || !candidateEntries.value.length || candidateEntries.value.length > 50 || !Number.isInteger(candidateCount.value) || candidateCount.value < 1 || candidateCount.value > 20) return
  await action(async () => {
    await write(`evaluation/sets/${selected.value}/candidates`, { libraries: effectiveCandidateLibraries.value, entry_ids: candidateEntries.value.map(a => a.entry_id), count: candidateCount.value, ...(candidateModel.value ? { model_id: candidateModel.value } : {}) })
    generating.value = false; tab.value = 'questions'; status.value = 'pending'; questionOffset.value = 0; toast.success(t('ikb19.generated')); await loadPane()
  })
}
async function confirm(q: Question) { await action(async () => { await write(`evaluation/sets/${selected.value}/questions/${q.id}/confirm`, {}); await loadPane(); toast.success(t('ikb19.confirmedMessage')) }) }
async function startRun() {
  if (!selected.value || !vector.value || !Number.isInteger(k.value) || k.value < 1 || k.value > 100) return
  await action(async () => {
    detail.value = await write<EvaluationRun>(`evaluation/sets/${selected.value}/runs`, { k: k.value, vector_set: vector.value, collapse_similar: collapse.value, ...(rerank.value === 'library' ? {} : { rerank_model_id: rerank.value }) })
    runOffset.value = 0; await loadPane()
  }, true)
}
async function openRun(id: string) {
  await action(async () => {
    detail.value = undefined
    let run: EvaluationRun
    try { run = await read<EvaluationRun>(`evaluation/runs/${id}`) }
    catch (e: any) { if (e?.status === 404) throw new Error(t('ikb19.runNotFound')); throw e }
    if (!sets.value.some(set => set.id === run.set_id)) {
      sets.value.push(await read<EvaluationSet>(`evaluation/sets/${run.set_id}`))
    }
    selected.value = run.set_id; tab.value = 'runs'
    // Let selection watchers clear the previous details before displaying this run.
    await nextTick()
    await loadPane()
    detail.value = run
  })
}
async function compare() {
  const beforeId = before.value, afterId = after.value
  if (!beforeId || !afterId || beforeId === afterId) return
  await action(async () => {
    const result = await read<Comparison>('evaluation/compare', { before: beforeId, after: afterId })
    if (before.value === beforeId && after.value === afterId) comparison.value = result
  })
}
async function page(kind: string, direction: number) {
  if (kind === 'sets') { setOffset.value = Math.max(0, setOffset.value + direction * 50); await action(async () => { await loadSets(); selected.value = sets.value[0]?.id || '' }); return }
  const offset = kind === 'questions' ? questionOffset : kind === 'records' ? recordOffset : runOffset
  offset.value = Math.max(0, offset.value + direction * 50); await loadPane()
}
watch([selected, tab, status], () => { editing.value = undefined; chosenRecord.value = undefined; error.value = ''; questionOffset.value = 0; recordOffset.value = 0; runOffset.value = 0; if (!initializing) void loadPane() })
watch(selected, () => { generating.value = false; setForm.value = undefined; comparisonRuns.value = {}; before.value = ''; after.value = ''; comparison.value = undefined; detail.value = undefined })
watch([before, after], () => { comparison.value = undefined })
onMounted(initialize)
onBeforeUnmount(() => { version++ })
</script>
<template>
  <main class="diagnostics-page analytics-page evaluation-page" :aria-busy="loading || busy">
    <div class="diagnostics-wrap analytics-wrap"><div class="diagnostics-heading"><h1>{{ t('ikb19.evaluationTitle') }}</h1></div><p class="muted">{{ t('ikb19.evaluationHint') }}</p>
    <div v-if="error" role="alert" class="notice error">{{ error }}<p>{{ t('ikb19.reviseRetry') }}</p><Button :disabled="busy" @click="initialize">{{ t('ikb19.refresh') }}</Button></div>
    <div class="toolbar">
      <label class="field">{{ t('ikb19.set') }}<select class="rq-control" :value="selected" :disabled="busy || loading" @change="selected = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.selectSet') }}</option><option v-for="set in sets" :key="set.id" :value="set.id">{{ set.name }}</option></select></label>
      <Button :disabled="busy" @click="error = ''; setForm = { name: '', description: '' }">{{ t('ikb19.newSet') }}</Button>
      <Button v-if="selectedSet" :disabled="busy" @click="error = ''; setForm = { ...selectedSet }">{{ t('ikb19.editSet') }}</Button>
      <Button v-if="selectedSet" :disabled="busy" @click="requestDelete(`evaluation/sets/${selected}`, selectedSet.name, 'set')">{{ t('ikb19.deleteSet') }}</Button>
      <Button v-if="setOffset || sets.length >= 50" :disabled="setOffset === 0 || busy || loading" @click="page('sets', -1)">{{ t('ikb19.previousSets') }}</Button><Button v-if="setOffset || sets.length >= 50" :disabled="sets.length < 50 || busy || loading" @click="page('sets', 1)">{{ t('ikb19.nextSets') }}</Button>
    </div>
    <p v-if="selectedSet?.description" class="muted">{{ selectedSet.description }}</p>
    <Dialog :model-value="!!setForm" :title="setForm?.id ? t('ikb19.editSet') : t('ikb19.newSet')" content-class="analytics-dialog" :closable="!busy" :close-on-overlay-click="!busy" :close-on-esc-keydown="!busy" @update:model-value="!$event && (setForm = undefined)"><form v-if="setForm" class="dialog-form" @submit.prevent="saveSet"><div class="fields"><label class="field">{{ t('ikb19.name') }}<input class="rq-control" required :value="setForm.name" @input="setForm.name = ($event.target as HTMLInputElement).value" /></label><label class="field">{{ t('ikb19.description') }}<textarea class="rq-control" :value="setForm.description" @input="setForm.description = ($event.target as HTMLTextAreaElement).value" /></label></div><p v-if="error" role="alert" class="notice error">{{ error }}</p><div class="toolbar"><Button variant="primary" :disabled="busy || !setForm.name.trim()" type="submit">{{ t('ikb19.save') }}</Button><Button type="button" :disabled="busy" @click="setForm = undefined">{{ t('ikb19.cancel') }}</Button></div></form></Dialog>
    <EmptyState v-if="!loading && !sets.length && !error" :title="t('ikb19.noSets')" />
    <p v-if="loading" role="status">{{ t('ikb19.loading') }}</p>
    <Tabs v-model="tab" :items="tabs" :label="t('ikb19.evaluationTitle')">
      <template #questions>
    <section v-if="selected" class="analytics-stack">
      <div class="toolbar"><label class="field">{{ t('ikb19.questionStatus') }}<select class="rq-control" :value="status" :disabled="busy" @change="status = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.all') }}</option><option value="confirmed">{{ t('ikb19.confirmed') }}</option><option value="pending">{{ t('ikb19.pending') }}</option></select></label><Button :disabled="busy" @click="editing = null">{{ t('ikb19.addQuestion') }}</Button><Button :disabled="busy" @click="error = ''; generating = true">{{ t('ikb19.aiCandidates') }}</Button></div>
      <Dialog :model-value="editing !== undefined" :title="editing ? t('ikb19.editQuestion') : t('ikb19.addQuestion')" content-class="analytics-dialog" :closable="!editorBusy" :close-on-overlay-click="!editorBusy" :close-on-esc-keydown="!editorBusy" @update:model-value="!$event && (editing = undefined)"><QuestionEditor v-if="editing !== undefined" :key="editing?.id || 'new'" :set-id="selected" :question="editing || undefined" :libraries="libraries" @busy="editorBusy = $event" @saved="editing = undefined; toast.success(t('ikb19.saved')); loadPane()" @cancel="editing = undefined" /></Dialog>
      <p v-if="!loading && !questions.length && !error" class="empty">{{ status === 'pending' ? t('ikb19.noCandidates') : t('ikb19.noQuestions') }}</p>
      <div v-if="questions.length" tabindex="0" role="region" :aria-label="t('ikb19.evaluationTitle')" class="table-scroll questions-table"><table class="rq-table"><thead><tr><th>{{ t('ikb19.query') }}</th><th>{{ t('ikb19.method') }}</th><th>{{ t('ikb19.libraries') }}</th><th>{{ t('ikb19.answers') }}</th><th>{{ t('ikb19.actions') }}</th></tr></thead><tbody><tr v-for="q in questions" :key="q.id"><td>{{ q.request.query }}<p class="muted">{{ q.source === 'ai' ? t('ikb19.aiSource') : q.source === 'search_record' ? t('ikb19.records') : t('ikb19.manualSource') }} · {{ t('ikb19.revision') }} {{ q.revision }}</p><p v-if="q.answer_deleted" class="notice">{{ t('ikb19.answerDeleted') }}</p><Tag>{{ q.status === 'pending' ? t('ikb19.pending') : t('ikb19.confirmed') }}</Tag></td><td>{{ q.method === 'graph' ? t('ikb19.graph') : t('ikb19.ordinary') }}</td><td>{{ libraryNames(q.request.libraries) }}</td><td><p v-for="a in q.answers" :key="a.entry_id"><a :title="a.entry_id" :href="`/platform/entries/${encodeURIComponent(a.entry_id)}`">{{ shortId(a.entry_id) }}</a> ({{ a.grade }})</p></td><td><div class="actions"><Button :disabled="busy" @click="editing = q">{{ t('ikb19.edit') }}</Button><Button v-if="q.status === 'pending'" :disabled="busy || q.answer_deleted" @click="confirm(q)">{{ t('ikb19.confirmCandidate') }}</Button><Button :disabled="busy" @click="requestDelete(`evaluation/sets/${selected}/questions/${q.id}`, q.request.query, 'question')">{{ t('ikb19.delete') }}</Button></div></td></tr></tbody></table></div>
      <div v-if="questionOffset || questions.length >= 50" class="toolbar"><Button :disabled="!questionOffset || busy || loading" @click="page('questions', -1)">{{ t('ikb19.previous') }}</Button><Button :disabled="questions.length < 50 || busy || loading" @click="page('questions', 1)">{{ t('ikb19.next') }}</Button></div>
    </section>
      </template><template #records>
    <section class="analytics-stack">
      <div class="fields"><label class="field">{{ t('ikb19.source') }}<select class="rq-control" :value="filters.source" @change="filters.source = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.all') }}</option><option value="web">Web</option><option value="http">HTTP</option><option value="mcp">MCP</option><option value="evaluation">{{ t('ikb19.evaluationTitle') }}</option></select></label><label class="field">{{ t('ikb19.method') }}<select class="rq-control" :value="filters.method" @change="filters.method = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.all') }}</option><option value="ordinary">{{ t('ikb19.ordinary') }}</option><option value="graph">{{ t('ikb19.graph') }}</option></select></label><label class="field">{{ t('ikb19.libraries') }}<select class="rq-control" :value="filters.library" @change="filters.library = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.all') }}</option><option v-for="lib in libraries" :key="lib.id" :value="lib.id">{{ lib.name }}</option></select></label><label class="field">{{ t('ikb19.query') }}<input class="rq-control" :value="filters.query" @input="filters.query = ($event.target as HTMLInputElement).value" /></label><label class="field">{{ t('ikb19.from') }}<input class="rq-control" type="datetime-local" :value="filters.from" @input="filters.from = ($event.target as HTMLInputElement).value" /></label><label class="field">{{ t('ikb19.to') }}<input class="rq-control" type="datetime-local" :value="filters.to" @input="filters.to = ($event.target as HTMLInputElement).value" /></label></div>
      <div class="toolbar"><label><input class="rq-checkbox" type="checkbox" :checked="filters.include_evaluation" @change="filters.include_evaluation = ($event.target as HTMLInputElement).checked" /> {{ t('ikb19.includeEvaluation') }}</label><Button :disabled="busy || loading" @click="recordOffset = 0; chosenRecord = undefined; loadPane()">{{ t('ikb19.apply') }}</Button></div>
      <p v-if="!loading && !records.length && !error" class="empty">{{ t('ikb19.noRecords') }}</p>
      <div v-if="records.length" tabindex="0" role="region" :aria-label="t('ikb19.evaluationTitle')" class="table-scroll records-table"><table class="rq-table"><thead><tr><th>{{ t('ikb19.source') }}</th><th>{{ t('ikb19.method') }}</th><th>{{ t('ikb19.libraries') }}</th><th>{{ t('ikb19.query') }}</th><th>{{ t('ikb19.time') }}</th><th>{{ t('ikb19.actions') }}</th></tr></thead><tbody><tr v-for="r in records" :key="r.id"><td>{{ r.source }}</td><td>{{ r.method === 'graph' ? t('ikb19.graph') : t('ikb19.ordinary') }}</td><td>{{ libraryNames(r.libraries) }}</td><td>{{ r.query }}<p v-if="r.error" class="notice error">{{ r.error }}</p></td><td>{{ new Date(r.created_at).toLocaleString() }}</td><td><Button :disabled="busy || !selected" @click="chooseRecord(r)">{{ t('ikb19.fromRecord') }}</Button></td></tr></tbody></table></div>
      <Dialog :model-value="!!chosenRecord" :title="t('ikb19.fromRecord')" content-class="analytics-dialog" :closable="!busy" :close-on-overlay-click="!busy" :close-on-esc-keydown="!busy" @update:model-value="!$event && (chosenRecord = undefined)"><section v-if="chosenRecord" class="analytics-stack"><h2>{{ t('ikb19.fromRecord') }}: {{ chosenRecord.query }}</h2><p>{{ t('ikb19.recordAnswersHint') }}</p><p v-if="!chosenRecord.results?.length" class="muted">{{ t('ikb19.recordNoResults') }}</p><label v-for="result in chosenRecord.results || []" :key="result.entry_id" class="record-answer"><input class="rq-checkbox" type="checkbox" :checked="recordAnswers.some(a => a.entry_id === result.entry_id)" @change="toggleRecordAnswer(result.entry_id)" /> #{{ result.rank }} <a :title="result.entry_id" :href="`/platform/entries/${encodeURIComponent(result.entry_id)}`">{{ shortId(result.entry_id) }}</a> ({{ result.score }})</label><p v-if="error" role="alert" class="notice error">{{ error }}</p><div class="toolbar"><Button variant="primary" :disabled="busy || !selected" @click="questionFromRecord">{{ t('ikb19.saveQuestion') }}</Button><Button :disabled="busy" @click="chosenRecord = undefined">{{ t('ikb19.cancel') }}</Button></div></section></Dialog>
      <div v-if="recordOffset || records.length >= 50" class="toolbar"><Button :disabled="!recordOffset || busy || loading" @click="page('records', -1)">{{ t('ikb19.previous') }}</Button><Button :disabled="records.length < 50 || busy || loading" @click="page('records', 1)">{{ t('ikb19.next') }}</Button></div>
    </section>
      </template><template #runs>
    <section class="analytics-stack">
      <form v-if="selected" class="panel" @submit.prevent="startRun"><h2>{{ t('ikb19.startRun') }}</h2><div class="fields"><label class="field">{{ t('ikb19.k') }}<input class="rq-control" type="number" min="1" max="100" required :value="k" @input="k = Number(($event.target as HTMLInputElement).value)" /></label><label class="field">{{ t('ikb19.vectorSet') }}<select class="rq-control" :value="vector" @change="vector = ($event.target as HTMLSelectElement).value"><option v-for="v in vectors" :key="v.selector" :value="v.selector">{{ vectorName(v) }}</option></select></label><label class="field">{{ t('ikb19.rerank') }}<select class="rq-control" :value="rerank" @change="rerank = ($event.target as HTMLSelectElement).value"><option value="library">{{ t('ikb19.libraryDefault') }}</option><option value="">{{ t('ikb19.disabled') }}</option><option v-for="m in models.filter(m => m.type === 'Rerank')" :key="m.id" :value="m.id">{{ m.name }}</option></select></label><label><input class="rq-checkbox" type="checkbox" :checked="collapse" @change="collapse = ($event.target as HTMLInputElement).checked" /> {{ t('ikb19.collapse') }}</label></div><p class="muted">{{ t('ikb19.runHint') }}</p><div class="toolbar"><Button type="submit" variant="primary" :disabled="busy || loading || !vectors.length || !Number.isInteger(k) || k < 1 || k > 100">{{ busy ? t('ikb19.running') : t('ikb19.startRun') }}</Button><Button type="button" :disabled="busy || loading" @click="loadPane">{{ t('ikb19.refresh') }}</Button></div></form>
      <p v-if="!loading && !runs.length && !error" class="empty">{{ t('ikb19.noRuns') }}</p>
      <div v-if="runs.length" tabindex="0" role="region" :aria-label="t('ikb19.evaluationTitle')" class="table-scroll runs-table"><table class="rq-table"><thead><tr><th>{{ t('ikb19.set') }}</th><th>{{ t('ikb19.time') }}</th><th>{{ t('ikb19.configuration') }}</th><th>{{ t('ikb19.runStatus') }}</th><th>{{ t('ikb19.actions') }}</th></tr></thead><tbody><tr v-for="r in runs" :key="r.id"><td>{{ r.set_snapshot.name }}</td><td>{{ new Date(r.created_at).toLocaleString() }}</td><td>K={{ r.options.k }} · {{ r.options.vector_set }}</td><td>{{ r.status === 'completed' ? t('ikb19.completed') : r.status === 'failed' ? t('ikb19.failed') : t('ikb19.running') }}<p v-if="r.error" class="notice error">{{ r.error }} {{ t('ikb19.runFailedNext') }}</p></td><td><Button :disabled="busy" @click="openRun(r.id)">{{ t('ikb19.details') }}</Button></td></tr></tbody></table></div>
      <div v-if="runOffset || runs.length >= 50" class="toolbar"><Button :disabled="!runOffset || busy || loading" @click="page('runs', -1)">{{ t('ikb19.previous') }}</Button><Button :disabled="runs.length < 50 || busy || loading" @click="page('runs', 1)">{{ t('ikb19.next') }}</Button></div>
      <div v-if="selected && completedRuns.length >= 2" class="panel compare-form"><h2>{{ t('ikb19.comparison') }}</h2><p class="muted">{{ t('ikb19.compareHint') }}</p><div class="fields"><label class="field">{{ t('ikb19.before') }}<select class="rq-control" :value="before" :disabled="busy" @change="before = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.selectRun') }}</option><option v-for="r in completedRuns" :key="r.id" :value="r.id">{{ runName(r) }}</option></select></label><label class="field">{{ t('ikb19.after') }}<select class="rq-control" :value="after" :disabled="busy" @change="after = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.selectRun') }}</option><option v-for="r in completedRuns" :key="r.id" :value="r.id">{{ runName(r) }}</option></select></label></div><div class="toolbar"><Button :disabled="busy || !before || !after || before === after" @click="compare">{{ t('ikb19.compare') }}</Button></div></div>
      <div v-if="detail" class="toolbar"><Button :disabled="busy" @click="openRun(detail.id)">{{ t('ikb19.refreshDetail') }}</Button></div>
      <RunResults :run="detail" :comparison="comparison" :before-run="comparisonRuns[before]" :after-run="comparisonRuns[after]" />
    </section>
      </template>
    </Tabs>
    <Dialog v-model="generating" :title="t('ikb19.aiCandidates')" content-class="analytics-dialog" :closable="!busy" :close-on-overlay-click="!busy" :close-on-esc-keydown="!busy">
    <form v-if="generating && selected" class="dialog-form" @submit.prevent="generate">
      <p class="muted">{{ t('ikb19.candidateHint') }}</p>
      <div class="candidate-scope"><p>{{ t('ikb19.libraries') }}</p><LibraryScopeSelect :model-value="candidateLibraries" :disabled="busy" @update:model-value="candidateLibraries = $event" @loaded="libraries = $event" /></div>
      <EntryPicker :libraries="effectiveCandidateLibraries" :model-value="candidateEntries" @update:model-value="candidateEntries = $event" />
      <div class="fields"><label class="field">{{ t('ikb19.count') }}<input class="rq-control" type="number" min="1" max="20" required :value="candidateCount" @input="candidateCount = Number(($event.target as HTMLInputElement).value)" /></label><label class="field">{{ t('ikb19.model') }}<select class="rq-control" :value="candidateModel" @change="candidateModel = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.libraryDefault') }}</option><option v-for="m in models.filter(m => m.type === 'KnowledgeQA')" :key="m.id" :value="m.id">{{ m.name }}</option></select></label></div>
      <p v-if="error" role="alert" class="notice error">{{ error }}</p><div class="toolbar"><Button type="submit" variant="primary" :disabled="busy || !effectiveCandidateLibraries.length || !candidateEntries.length || candidateEntries.length > 50 || !Number.isInteger(candidateCount) || candidateCount < 1 || candidateCount > 20">{{ busy ? t('ikb19.generating') : t('ikb19.generate') }}</Button></div>
    </form>
    </Dialog>
    </div>
  </main>
</template>
