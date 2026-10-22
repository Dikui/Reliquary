<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { listModels, type ModelConfig } from '@/api/model'
import { checkSettings, saveCheckSettings, checkRuns, checkRun, recheck, type CheckRun, type CheckSettings, type CheckEvidence } from '@/api/content-check'
const { t, locale } = useI18n(), route = useRoute(), router = useRouter()
const settings = ref<CheckSettings>({ enabled: false, model_id: '', weekday: 1, time: '' }), models = ref<ModelConfig[]>([])
const items = ref<CheckRun[]>([]), total = ref(0), detail = ref<CheckRun>(), loading = ref(false), busy = ref(false), error = ref(''), configError = ref(''), configReady = ref(false), message = ref('')
const selected = computed(() => typeof route.query.run === 'string' ? route.query.run : '')
const page = computed(() => Math.max(1, Number(route.query.page) || 1))
const modelOptions = computed(() => models.value.filter(m => m.status === 'active').map(m => ({ label: m.name, value: m.id! })))
const days = computed(() => Array.from({ length: 7 }, (_, value) => ({ value, label: t(`contentCheck.day${value}`) })))
const kinds = ['duplicate', 'conflict', 'result_error'] as const
const findings = computed(() => detail.value?.results?.flatMap(r => r.findings) || [])
const modelName = (id: string) => models.value.find(m => m.id === id)?.name || id
const stamp = (s: string) => new Date(s).toLocaleString(locale.value)
const evidenceRoute = (e: CheckEvidence) => ({ path: '/platform/entries', query: { entry_id: e.entry_id, reference: JSON.stringify(e.location) } })
const statusName = (s: string) => t(`contentCheck.${s}`)
let version = 0, disposed = false, timer: ReturnType<typeof setTimeout> | undefined
function showError(e: unknown) { error.value = (e && typeof e === 'object' && 'message' in e) ? String(e.message) : t('contentCheck.loadFailed') }
async function load() {
  const ticket = ++version, id = selected.value
  loading.value = true; error.value = ''
  try {
    const [list, run] = await Promise.all([checkRuns(page.value), id ? checkRun(id) : Promise.resolve(undefined)])
    if (disposed || ticket !== version) return
    items.value = list.items; total.value = list.total; detail.value = run
    const last = Math.max(1, Math.ceil(list.total / 20))
    if (page.value > last) void router.replace({ query: { ...route.query, page: String(last) } })
  } catch (e) { if (!disposed && ticket === version) showError(e) }
  finally { if (!disposed && ticket === version) loading.value = false }
}
async function save() {
  if (busy.value || !configReady.value) return
  busy.value = true; error.value = ''; message.value = ''
  try { settings.value = await saveCheckSettings(settings.value); message.value = t('contentCheck.saved') }
  catch (e) { showError(e) } finally { busy.value = false }
}
async function retry() {
  if (!detail.value || busy.value) return
  busy.value = true; error.value = ''
  try { const run = await recheck(detail.value.id); await router.push({ query: { ...route.query, run: run.id } }) }
  catch (e) { showError(e) } finally { busy.value = false }
}
async function poll() {
  await load()
  if (!disposed) timer = setTimeout(poll, 5000)
}
function changePage(value: number) { return router.replace({ query: { ...route.query, page: String(value) } }) }
function select(run?: string) { return router.push({ query: { ...route.query, run } }) }
watch(() => [route.query.page, route.query.run], () => { detail.value = undefined; void load() })
watch(locale, () => { document.title = `${t('contentCheck.title')} · Reliquary` }, { immediate: true })
async function loadConfig() {
  configError.value = ''
  try {
    const [config, choices] = await Promise.all([checkSettings(), listModels('KnowledgeQA')])
    if (!disposed) { settings.value = config; models.value = choices; configReady.value = true }
  } catch (e) { if (!disposed) configError.value = (e && typeof e === 'object' && 'message' in e) ? String(e.message) : t('contentCheck.loadFailed') }
}
onMounted(() => { void loadConfig(); void poll() })
onUnmounted(() => { disposed = true; version++; clearTimeout(timer) })
</script>

<template>
  <main class="check-page">
    <header><h1>{{ t('contentCheck.title') }}</h1><p>{{ t('contentCheck.description') }}</p><router-link to="/platform/diagnostics">{{ t('ingestDiagnostics.diagnostics') }}</router-link></header>
    <section v-if="!selected" class="settings">
      <h2>{{ t('contentCheck.settings') }}</h2>
      <form novalidate @submit.prevent="save">
        <label><span>{{ t('contentCheck.model') }}</span><t-select v-model="settings.model_id" :options="modelOptions" :aria-label="t('contentCheck.model')" clearable :disabled="busy || !configReady" /></label>
        <label><span>{{ t('contentCheck.day') }}</span><t-select v-model="settings.weekday" :options="days" :aria-label="t('contentCheck.day')" :disabled="busy || !configReady" /></label>
        <label><span>{{ t('contentCheck.time') }}</span><t-input v-model="settings.time" placeholder="HH:MM" :aria-label="t('contentCheck.time')" :disabled="busy || !configReady" /></label>
        <label class="enable"><t-switch v-model="settings.enabled" :disabled="busy || !configReady" :aria-label="t('contentCheck.enable')" /><span>{{ t('contentCheck.enable') }}</span></label>
        <t-button type="submit" :loading="busy" :disabled="!configReady">{{ t('contentCheck.save') }}</t-button>
      </form>
      <p class="muted">{{ t('contentCheck.hint') }}</p><p v-if="settings.next_run_at">{{ t('contentCheck.next') }} {{ stamp(settings.next_run_at) }}</p>
    </section>
    <div v-if="configError" class="error" role="alert">{{ configError }} <t-button variant="text" @click="loadConfig">{{ t('common.retry') }}</t-button></div>
    <p v-if="message" role="status">{{ message }}</p>
    <div v-if="error" class="error" role="alert">{{ error }} <t-button variant="text" @click="load">{{ t('common.retry') }}</t-button></div>
    <div class="toolbar"><t-button v-if="selected" variant="outline" @click="select()">{{ t('contentCheck.back') }}</t-button><t-button variant="outline" :loading="loading" @click="load">{{ t('contentCheck.refresh') }}</t-button></div>
    <section v-if="!selected" :aria-busy="loading">
      <t-loading :loading="loading"><ol v-if="items.length" class="runs"><li v-for="run in items" :key="run.id">
        <div class="heading"><time :datetime="run.created_at">{{ stamp(run.created_at) }}</time><strong>{{ statusName(run.status) }}</strong></div>
        <p>{{ modelName(run.model_id) }} · {{ run.completed }}/{{ run.total }}</p><p v-if="run.error" class="error" role="alert">{{ run.error }}</p>
        <t-button variant="outline" @click="select(run.id)">{{ t('contentCheck.details') }}</t-button>
      </li></ol><t-empty v-else-if="!loading && !error" :description="t('contentCheck.empty')" /></t-loading>
      <t-pagination v-if="total" :current="page" :total="total" :page-size="20" :show-page-size="false" @current-change="changePage" />
    </section>
    <section v-else-if="detail" :aria-busy="loading">
      <h2>{{ statusName(detail.status) }} · {{ detail.completed }}/{{ detail.total }}</h2>
      <p><code>{{ detail.id }}</code> · {{ modelName(detail.model_id) }}</p>
      <p v-if="detail.previous_id"><router-link :to="{ query: { ...route.query, run: detail.previous_id } }">{{ t('contentCheck.original') }}</router-link></p>
      <div v-if="detail.status === 'failed'" class="error" role="alert"><p>{{ detail.error }}</p><p>{{ t('contentCheck.unfinished', { count: detail.total - detail.completed }) }}</p><t-button :loading="busy" @click="retry">{{ t('contentCheck.recheck') }}</t-button></div>
      <section v-for="kind in kinds" :key="kind">
        <h3>{{ t(`contentCheck.${kind}`) }}</h3>
        <article v-for="(finding, index) in findings.filter(f => f.kind === kind)" :key="index" class="finding">
          <h4>{{ t('contentCheck.problem') }}</h4><p>{{ finding.description }}</p>
          <h4>{{ t('contentCheck.materials') }}</h4><ul><li v-for="e in finding.evidence" :key="e.entry_id">{{ e.title }} · <code>{{ e.library_id }} / {{ e.entry_id }}</code></li></ul>
          <h4>{{ t('contentCheck.evidence') }}</h4><div v-for="(e, i) in finding.evidence" :key="i"><p v-if="e.current_result">{{ t('contentCheck.current') }} {{ e.current_result }}</p><blockquote>{{ e.quote }}</blockquote><router-link :to="evidenceRoute(e)">{{ t('contentCheck.openSource') }} · {{ e.title }}</router-link></div>
        </article>
        <p v-if="!findings.some(f => f.kind === kind)" class="muted">{{ t(detail.status === 'completed' ? 'contentCheck.none' : 'contentCheck.incomplete') }}</p>
      </section>
    </section>
  </main>
</template>

<style scoped>
.check-page { flex: 1; min-height: 0; overflow-y: auto; padding: 24px; color: var(--td-text-color-primary); overflow-wrap: anywhere; }
header, .settings, .toolbar { margin-bottom: 20px; }
h1 { font-size: 24px; } h2 { font-size: 18px; } h3 { font-size: 16px; } h4 { margin-bottom: 8px; }
form { display: flex; flex-wrap: wrap; gap: 16px; align-items: end; }
label { display: flex; flex-direction: column; gap: 8px; min-width: 160px; } .enable { flex-direction: row; align-items: center; }
.toolbar, .heading { display: flex; gap: 16px; align-items: center; justify-content: space-between; }
.runs { list-style: none; padding: 0; } .runs li, .finding { background: var(--td-bg-color-container); border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); padding: 20px; margin-bottom: 16px; }
.error { color: var(--td-error-color); } .muted { color: var(--td-text-color-secondary); } blockquote { white-space: pre-wrap; margin: 12px 0; padding-left: 12px; border-left: 3px solid var(--td-component-stroke); }
a { color: var(--td-brand-color); cursor: pointer; } a:hover { text-decoration: underline; } a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
@media (max-width: 640px) { .check-page { flex: 1; min-height: 0; overflow-y: auto; padding: 16px; } form, .heading { align-items: stretch; flex-direction: column; } }
</style>
