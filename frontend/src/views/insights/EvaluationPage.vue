<script setup lang="ts">
import { ref, computed, watch, onMounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { read, write, remove, type Answer, type EvaluationSet, type Question, type SearchRecord, type EvaluationRun, type Comparison, type VectorSet } from '@/api/insights'
import QuestionEditor from './QuestionEditor.vue'
import EntryPicker from './EntryPicker.vue'
import RunResults from './RunResults.vue'
import LibraryScopeSelect from '@/components/LibraryScopeSelect.vue'
import './insights.css'
const { t } = useI18n(), route = useRoute()
const sets = ref<EvaluationSet[]>([]), libraries = ref<{ id: string; name: string }[]>([]), models = ref<{ id: string; name: string; type: string }[]>([])
const selected = ref(''), tab = ref('questions'), status = ref('confirmed'), loading = ref(true), busy = ref(false), error = ref(''), message = ref('')
const questions = ref<Question[]>([]), records = ref<SearchRecord[]>([]), runs = ref<EvaluationRun[]>([]), vectors = ref<VectorSet[]>([])
const setOffset = ref(0), questionOffset = ref(0), recordOffset = ref(0), runOffset = ref(0)
const setForm = ref<{ id?: string; name: string; description: string }>(), editing = ref<Question | null | undefined>(undefined)
const deletion = ref<{ path: string; label: string; kind: 'set' | 'question' }>()
const chosenRecord = ref<SearchRecord>(), recordAnswers = ref<Answer[]>([])
const candidateLibraries = ref<string[]>([]), candidateEntries = ref<Answer[]>([]), candidateCount = ref(5), candidateModel = ref('')
const effectiveCandidateLibraries = computed(() => candidateLibraries.value.length ? candidateLibraries.value : libraries.value.map(lib => lib.id))
const filters = ref({ source: '', method: '', library: '', query: '', from: '', to: '', include_evaluation: false })
const k = ref(10), vector = ref('active'), collapse = ref(false), rerank = ref('library')
const detail = ref<EvaluationRun>(), comparison = ref<Comparison>(), before = ref(''), after = ref('')
let version = 0
const selectedSet = computed(() => sets.value.find(s => s.id === selected.value))
const comparisonRuns = ref<Record<string, EvaluationRun>>({})
const completedRuns = computed(() => Object.values(comparisonRuns.value))
function libraryNames(ids: string[]) { return ids.map(id => libraries.value.find(l => l.id === id)?.name || id).join(', ') }
function vectorName(v: VectorSet) { return `${v.selector === 'active' ? t('ikb19.current') : v.selector.startsWith('candidate:') ? t('ikb19.candidate') : t('ikb19.archive')} · ${v.model_id || t('ikb19.libraryDefault')} ${v.dimension || ''}` }
function runName(r: EvaluationRun) { return `${new Date(r.created_at).toLocaleString()} · K=${r.options.k} · ${r.id.slice(0, 8)}` }
function timestamp(value: string) { return value ? new Date(value).toISOString() : undefined }
async function loadSets() { sets.value = await read<EvaluationSet[]>('evaluation/sets', { limit: 50, offset: setOffset.value }) }
async function loadPane() {
  const ticket = ++version, setId = selected.value
  loading.value = true; error.value = ''
  try {
    if (tab.value === 'questions') { const rows = setId ? await read<Question[]>(`evaluation/sets/${setId}/questions`, { status: status.value, limit: 50, offset: questionOffset.value }) : []; if (ticket === version) questions.value = rows }
    if (tab.value === 'records') { const rows = await read<SearchRecord[]>('search/records', { ...filters.value, from: timestamp(filters.value.from), to: timestamp(filters.value.to), limit: 50, offset: recordOffset.value }); if (ticket === version) records.value = rows }
    if (tab.value === 'runs') {
      const [rows, available] = await Promise.all([read<EvaluationRun[]>('evaluation/runs', { set_id: setId, limit: 50, offset: runOffset.value }), read<VectorSet[]>('evaluation/vector-sets')])
      if (ticket === version) { runs.value = rows; for (const run of rows) { if (run.status === 'completed' && run.set_id === setId) comparisonRuns.value[run.id] = run }; vectors.value = available; if (!available.some(v => v.selector === vector.value)) vector.value = available[0]?.selector || '' }
    }
  } catch (e: any) { if (ticket === version) error.value = e?.message || t('ikb19.loadFailed') }
  finally { if (ticket === version) loading.value = false }
}
async function initialize() {
  loading.value = true; error.value = ''
  try {
    const [, libs, mods] = await Promise.all([loadSets(), read<{ id: string; name: string }[]>('libraries'), read<{ id: string; name: string; type: string }[]>('models')])
    libraries.value = libs; models.value = mods
    if (!selected.value) selected.value = sets.value[0]?.id || ''
    await nextTick()
    if (typeof route.query.run === 'string') await openRun(route.query.run)
    else await loadPane()
  } catch (e: any) { error.value = e?.message || t('ikb19.loadFailed') }
  finally { loading.value = false }
}
async function action(work: () => Promise<void>, running = false) {
  if (busy.value) return
  busy.value = true; error.value = ''; message.value = ''
  try { await work() }
  catch (e: any) { error.value = running && e?.status === 503 ? t('ikb19.graphUnavailable') : e?.message || t('ikb19.actionFailed') }
  finally { busy.value = false }
}
async function saveSet() {
  const draft = setForm.value
  if (!draft?.name.trim()) return
  await action(async () => {
    const saved = await write<EvaluationSet>(`evaluation/sets${draft.id ? `/${draft.id}` : ''}`, { name: draft.name.trim(), description: draft.description }, !!draft.id)
    setForm.value = undefined; setOffset.value = 0; await loadSets(); selected.value = saved.id; message.value = t('ikb19.saved'); await loadPane()
  })
}
async function deleteItem() {
  const target = deletion.value
  if (!target) return
  await action(async () => { await remove(target.path); deletion.value = undefined; if (target.kind === 'set') { await loadSets(); selected.value = sets.value[0]?.id || '' }; await loadPane() })
}
function chooseRecord(record: SearchRecord) { chosenRecord.value = record; recordAnswers.value = [] }
function toggleRecordAnswer(id: string) { recordAnswers.value = recordAnswers.value.some(a => a.entry_id === id) ? recordAnswers.value.filter(a => a.entry_id !== id) : [...recordAnswers.value, { entry_id: id, grade: 1 }] }
async function questionFromRecord() {
  if (!chosenRecord.value || !selected.value) return
  await action(async () => { await write(`evaluation/sets/${selected.value}/from-record`, { record_id: chosenRecord.value!.id, answers: recordAnswers.value }); chosenRecord.value = undefined; tab.value = 'questions'; status.value = 'confirmed'; questionOffset.value = 0; message.value = t('ikb19.recordCreated'); await loadPane() })
}
async function generate() {
  if (!selected.value || !effectiveCandidateLibraries.value.length || !candidateEntries.value.length) return
  await action(async () => {
    await write(`evaluation/sets/${selected.value}/candidates`, { libraries: effectiveCandidateLibraries.value, entry_ids: candidateEntries.value.map(a => a.entry_id), count: candidateCount.value, ...(candidateModel.value ? { model_id: candidateModel.value } : {}) })
    tab.value = 'questions'; status.value = 'pending'; questionOffset.value = 0; message.value = t('ikb19.generated'); await loadPane()
  })
}
async function confirm(q: Question) { await action(async () => { await write(`evaluation/sets/${selected.value}/questions/${q.id}/confirm`, {}); await loadPane(); message.value = t('ikb19.confirmedMessage') }) }
async function startRun() {
  if (!selected.value || !vector.value) return
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
async function compare() { if (before.value && after.value && before.value !== after.value) await action(async () => { comparison.value = await read<Comparison>('evaluation/compare', { before: before.value, after: after.value }) }) }
async function page(kind: string, direction: number) {
  if (kind === 'sets') { setOffset.value = Math.max(0, setOffset.value + direction * 50); await action(async () => { await loadSets(); selected.value = sets.value[0]?.id || '' }); return }
  const offset = kind === 'questions' ? questionOffset : kind === 'records' ? recordOffset : runOffset
  offset.value = Math.max(0, offset.value + direction * 50); await loadPane()
}
watch([selected, tab, status], () => { editing.value = undefined; chosenRecord.value = undefined; deletion.value = undefined; error.value = ''; message.value = ''; questionOffset.value = 0; recordOffset.value = 0; runOffset.value = 0; void loadPane() })
watch(selected, () => { comparisonRuns.value = {}; before.value = ''; after.value = ''; comparison.value = undefined; detail.value = undefined })
watch([before, after], () => { comparison.value = undefined })
onMounted(initialize)
</script>
<template>
  <main class="insights" :aria-busy="loading || busy">
    <h1>{{ t('ikb19.evaluationTitle') }}</h1><p class="muted">{{ t('ikb19.evaluationHint') }}</p>
    <div v-if="error" role="alert" class="notice error">{{ error }}<p>{{ t('ikb19.reviseRetry') }}</p><button :disabled="busy" @click="initialize">{{ t('ikb19.refresh') }}</button></div>
    <p v-if="message" role="status">{{ message }}</p>
    <div class="toolbar">
      <label class="field">{{ t('ikb19.set') }}<select :value="selected" :disabled="busy || loading" @change="selected = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.selectSet') }}</option><option v-for="set in sets" :key="set.id" :value="set.id">{{ set.name }}</option></select></label>
      <button :disabled="busy" @click="setForm = { name: '', description: '' }">{{ t('ikb19.newSet') }}</button>
      <button v-if="selectedSet" :disabled="busy" @click="setForm = { ...selectedSet }">{{ t('ikb19.editSet') }}</button>
      <button v-if="selectedSet" :disabled="busy" @click="deletion = { path: `evaluation/sets/${selected}`, label: selectedSet.name, kind: 'set' }">{{ t('ikb19.deleteSet') }}</button>
      <button :disabled="setOffset === 0 || busy || loading" @click="page('sets', -1)">{{ t('ikb19.previousSets') }}</button><button :disabled="sets.length < 50 || busy || loading" @click="page('sets', 1)">{{ t('ikb19.nextSets') }}</button>
    </div>
    <p v-if="selectedSet?.description" class="muted">{{ selectedSet.description }}</p>
    <form v-if="setForm" class="panel" @submit.prevent="saveSet"><h2>{{ setForm.id ? t('ikb19.editSet') : t('ikb19.newSet') }}</h2><div class="fields"><label class="field">{{ t('ikb19.name') }}<input required :value="setForm.name" @input="setForm.name = ($event.target as HTMLInputElement).value" /></label><label class="field">{{ t('ikb19.description') }}<textarea :value="setForm.description" @input="setForm.description = ($event.target as HTMLTextAreaElement).value" /></label></div><div class="toolbar"><button class="primary" :disabled="busy || !setForm.name.trim()" type="submit">{{ t('ikb19.save') }}</button><button type="button" :disabled="busy" @click="setForm = undefined">{{ t('ikb19.cancel') }}</button></div></form>
    <section v-if="deletion" role="alertdialog" :aria-label="t('ikb19.deleteConfirm')" class="panel notice"><p>{{ t('ikb19.deleteConfirm') }}: {{ deletion.label }}</p><p>{{ deletion.kind === 'set' ? t('ikb19.deleteSetHint') : t('ikb19.deleteQuestionHint') }}</p><div class="actions"><button :disabled="busy" @click="deleteItem">{{ t('ikb19.confirmDelete') }}</button><button :disabled="busy" @click="deletion = undefined">{{ t('ikb19.cancel') }}</button></div></section>
    <p v-if="!loading && !sets.length && !error" class="empty">{{ t('ikb19.noSets') }}</p>
    <div role="tablist" class="toolbar"><button role="tab" :aria-selected="tab === 'questions'" :disabled="busy" @click="tab = 'questions'">{{ t('ikb19.questions') }}</button><button role="tab" :aria-selected="tab === 'records'" :disabled="busy" @click="tab = 'records'">{{ t('ikb19.records') }}</button><button role="tab" :aria-selected="tab === 'candidates'" :disabled="busy" @click="tab = 'candidates'">{{ t('ikb19.aiCandidates') }}</button><button role="tab" :aria-selected="tab === 'runs'" :disabled="busy" @click="tab = 'runs'">{{ t('ikb19.runs') }}</button></div>
    <p v-if="loading" role="status">{{ t('ikb19.loading') }}</p>
    <section v-if="tab === 'questions' && selected">
      <div class="toolbar"><label class="field">{{ t('ikb19.questionStatus') }}<select :value="status" :disabled="busy" @change="status = ($event.target as HTMLSelectElement).value"><option value="confirmed">{{ t('ikb19.confirmed') }}</option><option value="pending">{{ t('ikb19.pending') }}</option></select></label><button :disabled="busy" @click="editing = null">{{ t('ikb19.addQuestion') }}</button></div>
      <QuestionEditor v-if="editing !== undefined" :key="editing?.id || 'new'" :set-id="selected" :question="editing || undefined" :libraries="libraries" @saved="editing = undefined; loadPane()" @cancel="editing = undefined" />
      <p v-if="!loading && !questions.length && !error" class="empty">{{ status === 'pending' ? t('ikb19.noCandidates') : t('ikb19.noQuestions') }}</p>
      <div v-if="questions.length" class="table-scroll"><table><thead><tr><th>{{ t('ikb19.query') }}</th><th>{{ t('ikb19.method') }}</th><th>{{ t('ikb19.libraries') }}</th><th>{{ t('ikb19.answers') }}</th><th>{{ t('ikb19.actions') }}</th></tr></thead><tbody><tr v-for="q in questions" :key="q.id"><td>{{ q.request.query }}<p class="muted">{{ q.source === 'ai' ? t('ikb19.aiSource') : q.source === 'search_record' ? t('ikb19.records') : t('ikb19.manualSource') }} · {{ t('ikb19.revision') }} {{ q.revision }}</p><p v-if="q.answer_deleted" class="notice">{{ t('ikb19.answerDeleted') }}</p></td><td>{{ q.method === 'graph' ? t('ikb19.graph') : t('ikb19.ordinary') }}</td><td>{{ libraryNames(q.request.libraries) }}</td><td><p v-for="a in q.answers" :key="a.entry_id"><a :href="`/platform/entries?entry_id=${encodeURIComponent(a.entry_id)}`">{{ a.entry_id }}</a> ({{ a.grade }})</p></td><td><div class="actions"><button :disabled="busy" @click="editing = q">{{ t('ikb19.edit') }}</button><button v-if="q.status === 'pending'" :disabled="busy || q.answer_deleted" @click="confirm(q)">{{ t('ikb19.confirmCandidate') }}</button><button :disabled="busy" @click="deletion = { path: `evaluation/sets/${selected}/questions/${q.id}`, label: q.request.query, kind: 'question' }">{{ t('ikb19.delete') }}</button></div></td></tr></tbody></table></div>
      <div class="toolbar"><button :disabled="!questionOffset || busy || loading" @click="page('questions', -1)">{{ t('ikb19.previous') }}</button><button :disabled="questions.length < 50 || busy || loading" @click="page('questions', 1)">{{ t('ikb19.next') }}</button></div>
    </section>
    <section v-if="tab === 'records'">
      <div class="fields"><label class="field">{{ t('ikb19.source') }}<select :value="filters.source" @change="filters.source = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.all') }}</option><option value="web">Web</option><option value="http">HTTP</option><option value="mcp">MCP</option><option value="evaluation">{{ t('ikb19.evaluationTitle') }}</option></select></label><label class="field">{{ t('ikb19.method') }}<select :value="filters.method" @change="filters.method = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.all') }}</option><option value="ordinary">{{ t('ikb19.ordinary') }}</option><option value="graph">{{ t('ikb19.graph') }}</option></select></label><label class="field">{{ t('ikb19.libraries') }}<select :value="filters.library" @change="filters.library = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.all') }}</option><option v-for="lib in libraries" :key="lib.id" :value="lib.id">{{ lib.name }}</option></select></label><label class="field">{{ t('ikb19.query') }}<input :value="filters.query" @input="filters.query = ($event.target as HTMLInputElement).value" /></label><label class="field">{{ t('ikb19.from') }}<input type="datetime-local" :value="filters.from" @input="filters.from = ($event.target as HTMLInputElement).value" /></label><label class="field">{{ t('ikb19.to') }}<input type="datetime-local" :value="filters.to" @input="filters.to = ($event.target as HTMLInputElement).value" /></label></div>
      <div class="toolbar"><label><input type="checkbox" :checked="filters.include_evaluation" @change="filters.include_evaluation = ($event.target as HTMLInputElement).checked" /> {{ t('ikb19.includeEvaluation') }}</label><button :disabled="busy || loading" @click="recordOffset = 0; chosenRecord = undefined; loadPane()">{{ t('ikb19.apply') }}</button></div>
      <p v-if="!loading && !records.length && !error" class="empty">{{ t('ikb19.noRecords') }}</p>
      <div class="table-scroll"><table><thead><tr><th>{{ t('ikb19.source') }}</th><th>{{ t('ikb19.method') }}</th><th>{{ t('ikb19.libraries') }}</th><th>{{ t('ikb19.query') }}</th><th>{{ t('ikb19.time') }}</th><th>{{ t('ikb19.actions') }}</th></tr></thead><tbody><tr v-for="r in records" :key="r.id"><td>{{ r.source }}</td><td>{{ r.method === 'graph' ? t('ikb19.graph') : t('ikb19.ordinary') }}</td><td>{{ libraryNames(r.libraries) }}</td><td>{{ r.query }}<p v-if="r.error" class="notice error">{{ r.error }}</p></td><td>{{ new Date(r.created_at).toLocaleString() }}</td><td><button :disabled="busy || !selected" @click="chooseRecord(r)">{{ t('ikb19.fromRecord') }}</button></td></tr></tbody></table></div>
      <section v-if="chosenRecord" class="panel"><h2>{{ t('ikb19.fromRecord') }}: {{ chosenRecord.query }}</h2><p>{{ t('ikb19.recordAnswersHint') }}</p><p v-if="!chosenRecord.results?.length" class="muted">{{ t('ikb19.recordNoResults') }}</p><label v-for="result in chosenRecord.results || []" :key="result.entry_id" class="record-answer"><input type="checkbox" :checked="recordAnswers.some(a => a.entry_id === result.entry_id)" @change="toggleRecordAnswer(result.entry_id)" /> #{{ result.rank }} <a :href="`/platform/entries?entry_id=${encodeURIComponent(result.entry_id)}`">{{ result.entry_id }}</a> ({{ result.score }})</label><div class="toolbar"><button class="primary" :disabled="busy || !selected" @click="questionFromRecord">{{ t('ikb19.saveQuestion') }}</button><button :disabled="busy" @click="chosenRecord = undefined">{{ t('ikb19.cancel') }}</button></div></section>
      <div class="toolbar"><button :disabled="!recordOffset || busy || loading" @click="page('records', -1)">{{ t('ikb19.previous') }}</button><button :disabled="records.length < 50 || busy || loading" @click="page('records', 1)">{{ t('ikb19.next') }}</button></div>
    </section>
    <form v-if="tab === 'candidates' && selected" class="panel" @submit.prevent="generate">
      <h2>{{ t('ikb19.aiCandidates') }}</h2><p class="muted">{{ t('ikb19.candidateHint') }}</p>
      <div class="candidate-scope"><p>{{ t('ikb19.libraries') }}</p><LibraryScopeSelect :model-value="candidateLibraries" :disabled="busy" @update:model-value="candidateLibraries = $event" @loaded="libraries = $event" /></div>
      <EntryPicker :libraries="effectiveCandidateLibraries" :model-value="candidateEntries" @update:model-value="candidateEntries = $event" />
      <div class="fields"><label class="field">{{ t('ikb19.count') }}<input type="number" min="1" max="20" required :value="candidateCount" @input="candidateCount = Number(($event.target as HTMLInputElement).value)" /></label><label class="field">{{ t('ikb19.model') }}<select :value="candidateModel" @change="candidateModel = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.libraryDefault') }}</option><option v-for="m in models.filter(m => m.type === 'KnowledgeQA')" :key="m.id" :value="m.id">{{ m.name }}</option></select></label></div>
      <div class="toolbar"><button type="submit" class="primary" :disabled="busy || !effectiveCandidateLibraries.length || !candidateEntries.length || candidateEntries.length > 50 || candidateCount < 1 || candidateCount > 20">{{ busy ? t('ikb19.generating') : t('ikb19.generate') }}</button></div>
    </form>
    <section v-if="tab === 'runs'">
      <form v-if="selected" class="panel" @submit.prevent="startRun"><h2>{{ t('ikb19.startRun') }}</h2><div class="fields"><label class="field">{{ t('ikb19.k') }}<input type="number" min="1" max="100" required :value="k" @input="k = Number(($event.target as HTMLInputElement).value)" /></label><label class="field">{{ t('ikb19.vectorSet') }}<select :value="vector" @change="vector = ($event.target as HTMLSelectElement).value"><option v-for="v in vectors" :key="v.selector" :value="v.selector">{{ vectorName(v) }}</option></select></label><label class="field">{{ t('ikb19.rerank') }}<select :value="rerank" @change="rerank = ($event.target as HTMLSelectElement).value"><option value="library">{{ t('ikb19.libraryDefault') }}</option><option value="">{{ t('ikb19.disabled') }}</option><option v-for="m in models.filter(m => m.type === 'Rerank')" :key="m.id" :value="m.id">{{ m.name }}</option></select></label><label><input type="checkbox" :checked="collapse" @change="collapse = ($event.target as HTMLInputElement).checked" /> {{ t('ikb19.collapse') }}</label></div><p class="muted">{{ t('ikb19.runHint') }}</p><div class="toolbar"><button type="submit" class="primary" :disabled="busy || loading || !vectors.length || k < 1 || k > 100">{{ busy ? t('ikb19.running') : t('ikb19.startRun') }}</button><button type="button" :disabled="busy || loading" @click="loadPane">{{ t('ikb19.refresh') }}</button></div></form>
      <p v-if="!loading && !runs.length && !error" class="empty">{{ t('ikb19.noRuns') }}</p>
      <div class="table-scroll"><table><thead><tr><th>{{ t('ikb19.set') }}</th><th>{{ t('ikb19.time') }}</th><th>{{ t('ikb19.configuration') }}</th><th>{{ t('ikb19.runStatus') }}</th><th>{{ t('ikb19.actions') }}</th></tr></thead><tbody><tr v-for="r in runs" :key="r.id"><td>{{ r.set_snapshot.name }}</td><td>{{ new Date(r.created_at).toLocaleString() }}</td><td>K={{ r.options.k }} · {{ r.options.vector_set }}</td><td>{{ r.status === 'completed' ? t('ikb19.completed') : r.status === 'failed' ? t('ikb19.failed') : t('ikb19.running') }}<p v-if="r.error" class="notice error">{{ r.error }} {{ t('ikb19.runFailedNext') }}</p></td><td><button :disabled="busy" @click="openRun(r.id)">{{ t('ikb19.details') }}</button></td></tr></tbody></table></div>
      <div class="toolbar"><button :disabled="!runOffset || busy || loading" @click="page('runs', -1)">{{ t('ikb19.previous') }}</button><button :disabled="runs.length < 50 || busy || loading" @click="page('runs', 1)">{{ t('ikb19.next') }}</button></div>
      <div v-if="selected" class="panel"><h2>{{ t('ikb19.comparison') }}</h2><p class="muted">{{ t('ikb19.compareHint') }}</p><div class="fields"><label class="field">{{ t('ikb19.before') }}<select :value="before" @change="before = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.selectRun') }}</option><option v-for="r in completedRuns" :key="r.id" :value="r.id">{{ runName(r) }}</option></select></label><label class="field">{{ t('ikb19.after') }}<select :value="after" @change="after = ($event.target as HTMLSelectElement).value"><option value="">{{ t('ikb19.selectRun') }}</option><option v-for="r in completedRuns" :key="r.id" :value="r.id">{{ runName(r) }}</option></select></label></div><div class="toolbar"><button :disabled="busy || !before || !after || before === after" @click="compare">{{ t('ikb19.compare') }}</button></div></div>
      <div v-if="detail" class="toolbar"><button :disabled="busy" @click="openRun(detail.id)">{{ t('ikb19.refreshDetail') }}</button></div>
      <RunResults :run="detail" :comparison="comparison" />
    </section>
  </main>
</template>
<style scoped>.record-answer { display: block; margin: 10px 0; overflow-wrap: anywhere; } .candidate-scope { margin: 16px 0; min-width: 0; }</style>
