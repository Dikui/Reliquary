<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Button, Checkbox, Dialog, EmptyState, ErrorState, Field, Input, Select, StatusSquare, Tag, confirm, toast } from '@/components/ui'
import type { ConfirmHandle } from '@/components/ui/feedback'
import { listModels, type ModelConfig } from '@/api/model'
import { checkSettings, saveCheckSettings, checkRuns, checkRun, recheck, type CheckRun, type CheckSettings } from '@/api/content-check'
import { entryDetailHref } from '@/utils/bodyReference'

const { t, locale } = useI18n(), route = useRoute(), router = useRouter()
const settings = ref<CheckSettings>({ enabled: false, model_id: '', weekday: 1, time: '' })
const draft = ref<CheckSettings>({ ...settings.value }), models = ref<ModelConfig[]>([])
const saveAttempted = ref(false)
const configOpen = ref(false), configReady = ref(false), configLoading = ref(false), configError = ref(''), saving = ref(false), saveError = ref('')
const items = ref<CheckRun[]>([]), total = ref(0), detail = ref<CheckRun>(), loading = ref(false), listError = ref(''), detailError = ref(''), operationError = ref(''), retrying = ref(false)
const evidenceFocus = ref('')
const selected = computed(() => typeof route.query.run === 'string' ? route.query.run : items.value[0]?.id || '')
const page = computed(() => { const value = Number(route.query.page); return Number.isInteger(value) && value > 0 ? value : 1 })
const modelOptions = computed(() => models.value.filter(model => model.status === 'active' && model.id))
const modelError = computed(() => saveAttempted.value && draft.value.enabled && !modelOptions.value.some(model => model.id === draft.value.model_id) ? t('contentCheck.invalidModel') : '')
const timeError = computed(() => saveAttempted.value && draft.value.enabled && !/^([01]\d|2[0-3]):[0-5]\d$/.test(draft.value.time) ? t('contentCheck.invalidTime') : '')
const days = computed(() => Array.from({ length: 7 }, (_, value) => ({ value, label: t(`contentCheck.day${value}`) })))
const kinds = ['duplicate', 'conflict', 'result_error'] as const
const findings = computed(() => detail.value?.results?.flatMap(result => result.findings || []) || [])
const groups = computed(() => kinds.map(kind => ({ kind, findings: findings.value.filter(finding => finding.kind === kind) })).filter(group => group.findings.length))
const modelName = (id: string) => models.value.find(model => model.id === id)?.name || id
const stamp = (value: string) => new Date(value).toLocaleString(locale.value)
const runHref = (id: string) => router.resolve({ query: { ...route.query, run: id } }).href
const shortID = (id: string) => id.length > 12 ? `${id.slice(0, 8)}…` : id
const statusName = (status: string) => ['pending', 'running', 'completed', 'failed'].includes(status) ? t(`contentCheck.${status}`) : status
const problemCount = (run: CheckRun) => {
  const results = detail.value?.id === run.id ? detail.value.results : run.results
  return results ? results.reduce((count, result) => count + (result.findings?.length || 0), 0) : null
}
let generation = 0, configGeneration = 0, disposed = false, timer: ReturnType<typeof setTimeout> | undefined, confirmation: ConfirmHandle | undefined
function errorMessage(cause: unknown) {
  const value = cause as { status?: number; code?: number; error?: { code?: number }; message?: string }
  const code = value?.error?.code ?? value?.code
  return value?.status === 404 || code === 1003 ? t('contentCheck.notFound') : value?.status === 409 || code === 1005 ? t('contentCheck.changed') : value?.message || t('contentCheck.loadFailed')
}
async function load() {
  const ticket = ++generation
  loading.value = true; listError.value = ''; detailError.value = ''
  try {
    const list = await checkRuns(page.value)
    if (disposed || ticket !== generation) return
    items.value = list.items || []; total.value = list.total
    const last = Math.max(1, Math.ceil(total.value / 20))
    if (page.value > last) { await changePage(last); return }
  } catch (cause) { if (!disposed && ticket === generation) listError.value = errorMessage(cause) }
  if (disposed || ticket !== generation) return
  const id = selected.value
  if (detail.value?.id !== id) detail.value = undefined
  if (id) {
    try {
      const run = await checkRun(id)
      if (!disposed && ticket === generation) detail.value = run
    } catch (cause) { if (!disposed && ticket === generation) { detail.value = undefined; detailError.value = errorMessage(cause) } }
  } else detail.value = undefined
  if (!disposed && ticket === generation) loading.value = false
}
async function loadConfig() {
  const ticket = ++configGeneration
  configLoading.value = true; configError.value = ''
  try {
    const [config, choices] = await Promise.all([checkSettings(), listModels('KnowledgeQA')])
    if (!disposed && ticket === configGeneration) { settings.value = config; models.value = choices; if (!configReady.value && configOpen.value) draft.value = { ...config }; configReady.value = true }
  } catch (cause) { if (!disposed && ticket === configGeneration) configError.value = errorMessage(cause) }
  finally { if (!disposed && ticket === configGeneration) configLoading.value = false }
}
function openConfig() { draft.value = { ...settings.value }; saveError.value = ''; saveAttempted.value = false; configOpen.value = true }
async function save() {
  if (saving.value || !configReady.value) return
  saveAttempted.value = true
  if (modelError.value || timeError.value) {
    await nextTick()
    if (typeof document !== 'undefined') document.getElementById(modelError.value ? 'check-model' : 'check-time')?.focus()
    return
  }
  saving.value = true; saveError.value = ''
  try {
    const saved = await saveCheckSettings({ enabled: draft.value.enabled, model_id: draft.value.model_id, weekday: draft.value.weekday, time: draft.value.time })
    if (disposed) return
    settings.value = saved; configOpen.value = false; toast.success(t('contentCheck.saved'))
  } catch (cause) { if (!disposed) saveError.value = errorMessage(cause) }
  finally { if (!disposed) saving.value = false }
}
async function retry() {
  if (!detail.value || detail.value.status !== 'failed' || retrying.value) return
  const id = detail.value.id, count = Math.max(0, detail.value.total - detail.value.completed)
  retrying.value = true; operationError.value = ''
  confirmation = confirm({ header: t('contentCheck.recheck'), body: t('contentCheck.recheckConfirm', { count }), confirmBtn: t('contentCheck.recheck'), cancelBtn: t('common.cancel') })
  const accepted = await confirmation.result
  confirmation = undefined
  if (!accepted || disposed) { retrying.value = false; return }
  try {
    const run = await recheck(id)
    if (disposed) return
    toast.success(t('contentCheck.recheckQueued'))
    await router.push({ query: { ...route.query, page: undefined, run: run.id } })
  } catch (cause) { if (!disposed) operationError.value = errorMessage(cause) }
  finally { if (!disposed) retrying.value = false }
}
function changePage(value: number) { return router.replace({ query: { ...route.query, run: undefined, page: value > 1 ? String(value) : undefined } }) }
function select(id: string) { if (!retrying.value) return router.push({ query: { ...route.query, run: id } }) }
async function poll() { await load(); if (!disposed) timer = setTimeout(poll, 5000) }
watch(() => [route.query.page, route.query.run], (next, previous) => {
  if (next[0] !== previous[0]) { items.value = []; total.value = 0 }
  detail.value = undefined; evidenceFocus.value = ''; operationError.value = ''; void load()
})
watch(locale, () => { if (typeof document !== 'undefined') document.title = `${t('contentCheck.title')} · Reliquary` }, { immediate: true })
onMounted(() => { void loadConfig(); void poll() })
onUnmounted(() => { disposed = true; generation++; configGeneration++; clearTimeout(timer); confirmation?.destroy() })
</script>

<template>
  <main class="rq-ui diagnostics-page">
    <div class="diagnostics-wrap checks-wrap">
      <header class="diagnostics-heading"><h1>{{ t('contentCheck.title') }}</h1><div class="diagnostics-actions"><span v-if="configReady" class="check-plan"><StatusSquare :status="settings.enabled ? 'success' : 'neutral'" :label="settings.enabled ? t('contentCheck.enabled') : t('contentCheck.disabled')" /><template v-if="settings.enabled"> · {{ days[settings.weekday]?.label }} {{ settings.time }} · {{ modelName(settings.model_id) }} <span>{{ t('contentCheck.scheduleZone') }}</span></template></span><Button :disabled="configLoading" @click="openConfig">{{ t('contentCheck.settings') }}</Button><Button :loading="loading" @click="load">{{ t('contentCheck.refresh') }}</Button></div></header>
      <p class="diagnostics-muted">{{ t('contentCheck.description') }}</p>
      <p v-if="settings.next_run_at" class="diagnostics-muted diagnostics-small">{{ t('contentCheck.next') }} {{ stamp(settings.next_run_at) }}</p>
      <ErrorState v-if="configError" :title="configError"><Button :loading="configLoading" @click="loadConfig">{{ t('common.retry') }}</Button></ErrorState>
      <ErrorState v-if="listError" :title="listError"><Button :loading="loading" @click="load">{{ t('common.retry') }}</Button></ErrorState>
      <p v-if="loading && !items.length" role="status">{{ t('common.loading') }}</p>
      <div v-if="items.length || selected" class="checks-layout" :class="{ 'checks-layout--detail-only': !items.length }" :aria-busy="loading">
        <aside v-if="items.length" class="check-history" :aria-label="t('contentCheck.batches')">
          <ul class="check-runs"><li v-for="run in items" :key="run.id"><button type="button" class="check-run" :aria-current="selected === run.id ? 'true' : undefined" :data-run-id="run.id" :disabled="retrying" @click="select(run.id)">
            <time class="diagnostics-mono" :datetime="run.created_at">{{ stamp(run.created_at) }}</time><span class="check-run-count diagnostics-mono">{{ problemCount(run)?.toLocaleString(locale) ?? `${run.completed}/${run.total}` }}</span>
            <span class="check-run-hint"><template v-if="problemCount(run) !== null">{{ t('contentCheck.problemUnit') }} · </template>{{ t('contentCheck.runProgress', { completed: run.completed, total: run.total, count: Math.max(0, run.total - run.completed) }) }}</span><Tag :variant="run.status === 'failed' ? 'danger' : 'plain'">{{ statusName(run.status) }}</Tag>
          </button></li></ul>
          <nav v-if="total > 20" class="diagnostics-pagination" :aria-label="t('entries.pagination')"><span>{{ t('contentCheck.batchCount', { count: total }) }}</span><Button :disabled="page <= 1 || loading || retrying" @click="changePage(page - 1)">{{ t('entries.previous') }}</Button><Button :disabled="page * 20 >= total || loading || retrying" @click="changePage(page + 1)">{{ t('entries.next') }}</Button></nav>
        </aside>
        <section class="check-findings" :aria-label="t('contentCheck.details')">
          <ErrorState v-if="detailError" :title="detailError"><Button :loading="loading" @click="load">{{ t('common.retry') }}</Button></ErrorState>
          <p v-else-if="!detail && loading" role="status">{{ t('common.loading') }}</p>
          <template v-if="detail">
            <header class="diagnostics-heading"><h2>{{ statusName(detail.status) }} · {{ detail.completed }}/{{ detail.total }}</h2><span class="diagnostics-muted" :title="detail.id">{{ shortID(detail.id) }} · {{ modelName(detail.model_id) }}</span></header>
            <a v-if="detail.previous_id" :href="runHref(detail.previous_id)" @click.prevent="select(detail.previous_id)">{{ t('contentCheck.original') }}</a>
            <div v-if="detail.status === 'failed'" class="check-unfinished" role="status"><div><strong>{{ t('contentCheck.unfinished', { count: Math.max(0, detail.total - detail.completed) }) }}</strong><p v-if="detail.error">{{ detail.error }}</p><details v-if="detail.targets?.slice(detail.completed).length"><summary>{{ t('contentCheck.unfinishedMaterials') }}</summary><ul><li v-for="id in detail.targets.slice(detail.completed)" :key="id"><a :href="entryDetailHref(id)" :title="id">{{ shortID(id) }}</a></li></ul></details></div><Button :loading="retrying" @click="retry">{{ t('contentCheck.recheck') }}</Button></div>
            <ErrorState v-if="operationError" :title="operationError" />
            <section v-for="group in groups" :key="group.kind" class="check-group"><h2>{{ t(`contentCheck.${group.kind}`) }} <span class="diagnostics-mono">{{ group.findings.length }}</span></h2>
              <ol class="check-problems"><li v-for="(finding, index) in group.findings" :key="index" class="check-problem"><div><h3>{{ finding.description }}</h3><p class="diagnostics-muted">{{ t('contentCheck.materials') }} <template v-for="(evidence, i) in finding.evidence" :key="i"><span v-if="i"> · </span><a :href="entryDetailHref(evidence.entry_id, evidence.location)" :title="evidence.entry_id">{{ evidence.title || shortID(evidence.entry_id) }}</a></template></p>
                <div v-for="(evidence, i) in finding.evidence" :key="i" class="check-evidence"><p v-if="evidence.current_result">{{ t('contentCheck.current') }} {{ evidence.current_result }}</p><blockquote tabindex="0" :class="{ 'rq-focus-row': evidenceFocus === `${group.kind}-${index}-${i}` }" @focus="evidenceFocus = `${group.kind}-${index}-${i}`" @mouseenter="evidenceFocus = `${group.kind}-${index}-${i}`"><a class="diagnostics-mono" :href="entryDetailHref(evidence.entry_id, evidence.location)" :title="evidence.entry_id">{{ shortID(evidence.entry_id) }} · ¶{{ evidence.location.paragraph }}</a><p>{{ evidence.quote }}</p></blockquote></div>
              </div></li></ol>
            </section>
            <EmptyState v-if="!findings.length" :title="t(detail.status === 'completed' ? 'contentCheck.none' : 'contentCheck.incomplete')" />
          </template>
        </section>
      </div>
      <EmptyState v-else-if="!loading && !listError" :title="t('contentCheck.empty')"><Button :disabled="!configReady" @click="openConfig">{{ t('contentCheck.settings') }}</Button></EmptyState>
    </div>
    <Dialog v-model="configOpen" :title="t('contentCheck.settings')" content-class="check-settings-dialog" :closable="!saving" :close-on-overlay-click="!saving" :close-on-esc-keydown="!saving">
      <ErrorState v-if="configError" :title="configError"><Button :loading="configLoading" @click="loadConfig">{{ t('common.retry') }}</Button></ErrorState>
      <form id="check-settings-form" class="check-settings" novalidate @submit.prevent="save">
        <label class="check-enable"><Checkbox v-model="draft.enabled" :disabled="saving || !configReady" />{{ t('contentCheck.enable') }}</label>
        <Field id="check-model" :label="t('contentCheck.model')" :error="modelError"><Select v-model="draft.model_id" :aria-label="t('contentCheck.model')" :disabled="saving || !configReady"><option value="">{{ t('contentCheck.selectModel') }}</option><option v-for="model in modelOptions" :key="model.id" :value="model.id">{{ model.name }}</option></Select></Field>
        <div class="check-schedule-row"><Field :label="t('contentCheck.day')"><Select :model-value="draft.weekday" :disabled="saving || !configReady" @update:model-value="draft.weekday = Number($event)"><option v-for="day in days" :key="day.value" :value="day.value">{{ day.label }}</option></Select></Field><Field id="check-time" :label="t('contentCheck.time')" :error="timeError"><Input v-model="draft.time" type="time" :disabled="saving || !configReady" /></Field></div>
        <p class="diagnostics-muted">{{ t('contentCheck.hint') }}</p><p v-if="saveError" class="diagnostics-error" role="alert">{{ saveError }}</p>
      </form>
      <template #footer><Button :disabled="saving" @click="configOpen = false">{{ t('common.cancel') }}</Button><Button type="submit" form="check-settings-form" variant="primary" :loading="saving" :disabled="!configReady">{{ t('contentCheck.save') }}</Button></template>
    </Dialog>
  </main>
</template>

<style src="./diagnostics.css"></style>
