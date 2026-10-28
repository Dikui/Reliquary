<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watchPostEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { listPromptSettings, updatePromptSetting, resetPromptSetting, getSystemSetting, updateSystemSetting, type SystemSettingItem } from '@/api/system'
import { listLibraries, type LibraryCatalogItem } from '@/api/library'
import SafeTextarea from '@/components/SafeTextarea.vue'

const { t } = useI18n()
const rows = ref<SystemSettingItem[]>([])
const libraries = ref<LibraryCatalogItem[]>([])
const libraryId = ref('')
const catalogLoading = ref(false)
const catalogError = ref('')
const selectedKey = ref('')
const draft = ref('')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const invalid = ref(false)
const qualityEnabled = ref(false)
const qualitySaving = ref(false)
const editor = ref<InstanceType<typeof SafeTextarea>>()
const scopeSelector = ref<{ $el?: HTMLElement }>()
let generation = 0
let pendingLeave: Promise<boolean> | undefined
let dismissLeave: (() => void) | undefined
let dismissReset: (() => void) | undefined
let disposed = false

const definitions = computed(() => [
  { key: 'prompt.article_processing', name: t('promptSettings.articleProcessing'), description: t('promptSettings.articleProcessingHint') },
  { key: 'prompt.quality_scoring', name: t('promptSettings.qualityScoring'), description: t('promptSettings.qualityScoringHint') },
  { key: 'prompt.summary_merge', name: t('promptSettings.summaryMerge'), description: t('promptSettings.summaryMergeHint') },
  { key: 'prompt.fact_grouping', name: t('promptSettings.factGrouping'), description: t('promptSettings.factGroupingHint') },
  { key: 'prompt.story_consolidation', name: t('promptSettings.storyConsolidation'), description: t('promptSettings.storyConsolidationHint') },
  { key: 'prompt.output_correction', name: t('promptSettings.outputCorrection'), description: t('promptSettings.outputCorrectionHint') },
  { key: 'prompt.evaluation_questions', name: t('promptSettings.evaluationQuestions'), description: t('promptSettings.evaluationQuestionsHint') },
  { key: 'prompt.evaluation_questions_multi_library', name: t('promptSettings.multiLibraryEvaluation'), description: t('promptSettings.multiLibraryEvaluationHint') },
  { key: 'prompt.model_connection_test', name: t('promptSettings.modelConnectionTest'), description: t('promptSettings.modelConnectionTestHint') },
  { key: 'prompt.model_maintenance_probe', name: t('promptSettings.modelMaintenanceProbe'), description: t('promptSettings.modelMaintenanceProbeHint') },
])
const scopeName = computed(() => {
  const library = libraries.value.find(item => item.id === libraryId.value)
  return library ? library.display_name || library.name : t('promptSettings.globalScope')
})
const scopeOptions = computed(() => [
  ...libraries.value.map(item => ({ value: item.id, label: item.display_name || item.name })),
  { value: '', label: t('promptSettings.globalScope') },
])
const templates = computed(() => definitions.value.flatMap(definition => {
  const row = rows.value.find(item => item.key === definition.key)
  return row && typeof row.value === 'string' ? [{ ...definition, row }] : []
}))
const selected = computed(() => templates.value.find(item => item.key === selectedKey.value))
// The per-library quality scoring switch is stored beside the library's prompts.
const qualitySwitchKey = computed(() => libraryId.value ? `prompt.library.${libraryId.value}.quality_scoring_enabled` : '')
const dirty = computed(() => !!selected.value && draft.value !== selected.value.row.value)
// TDesign places fallthrough ARIA attributes on its wrapper instead of the input.
watchPostEffect(() => {
  const scopeInput = scopeSelector.value?.$el?.querySelector('input')
  scopeInput?.setAttribute('id', 'prompt-scope')
  scopeInput?.setAttribute('aria-label', t('promptSettings.scope'))
  const field = editor.value?.$el?.querySelector('textarea') as HTMLTextAreaElement | undefined
  field?.setAttribute('aria-invalid', String(invalid.value))
  field?.setAttribute('aria-describedby', invalid.value ? 'prompt-hint prompt-field-error' : 'prompt-hint')
})

function reset() {
  draft.value = String(selected.value?.row.value ?? '')
  error.value = ''
  invalid.value = false
}

async function load() {
  if (saving.value || disposed) return
  const request = ++generation
  loading.value = true
  error.value = ''
  try {
    const [result, quality] = await Promise.all([
      listPromptSettings(libraryId.value || undefined),
      qualitySwitchKey.value ? getSystemSetting(qualitySwitchKey.value) : Promise.resolve(undefined),
    ])
    if (request !== generation) return
    rows.value = result
    qualityEnabled.value = quality?.value === true
    if (!templates.value.some(item => item.key === selectedKey.value)) selectedKey.value = templates.value[0]?.key ?? ''
    reset()
  } catch (cause: any) {
    if (request === generation) error.value = cause?.message || t('promptSettings.loadFailed')
  } finally {
    if (request === generation) loading.value = false
  }
}

async function initialize() {
  if (catalogLoading.value || !await confirmLeave()) return
  catalogLoading.value = true
  catalogError.value = ''
  try {
    const result = await listLibraries()
    if (disposed) return
    if (!result.success) throw new Error(result.message || t('promptSettings.librariesFailed'))
    libraries.value = result.data || []
    if (!libraries.value.some(item => item.id === libraryId.value)) libraryId.value = libraries.value[0]?.id ?? ''
  } catch (cause: any) {
    if (!disposed) catalogError.value = cause?.message || t('promptSettings.librariesFailed')
  } finally {
    if (!disposed) catalogLoading.value = false
  }
  if (!disposed) await load()
}

function confirmLeave(): Promise<boolean> {
  if (saving.value) return Promise.resolve(false)
  if (!dirty.value) return Promise.resolve(true)
  if (pendingLeave) return pendingLeave
  pendingLeave = new Promise(resolve => {
    let settled = false
    const finish = (leave: boolean) => {
      if (settled) return
      settled = true
      if (leave) reset()
      dialog.destroy()
      pendingLeave = undefined
      dismissLeave = undefined
      resolve(leave)
    }
    const dialog = DialogPlugin.confirm({
      header: t('common.discardTitle'),
      body: t('common.discardHint'),
      confirmBtn: t('common.discard'),
      cancelBtn: t('common.cancel'),
      onConfirm: () => finish(true),
      onCancel: () => finish(false),
      onClose: () => finish(false),
    })
    dismissLeave = () => finish(false)
  })
  return pendingLeave
}

async function selectTemplate(key: string) {
  if (key === selectedKey.value || !await confirmLeave()) return
  selectedKey.value = key
  reset()
}

async function selectScope(id: string) {
  if (id === libraryId.value || catalogLoading.value || !await confirmLeave()) return
  libraryId.value = id
  rows.value = []
  selectedKey.value = ''
  reset()
  await load()
}

async function save() {
  if (saving.value || loading.value || !selected.value || !dirty.value) return
  error.value = ''
  invalid.value = !draft.value.trim() || new TextEncoder().encode(draft.value).length > 65536
  if (invalid.value) {
    error.value = !draft.value.trim() ? t('promptSettings.required') : t('promptSettings.tooLong')
    editor.value?.focus()
    return
  }
  const key = selectedKey.value
  const request = generation
  const scope = libraryId.value || undefined
  const name = scopeName.value
  saving.value = true
  try {
    const updated = await updatePromptSetting(key.slice('prompt.'.length), draft.value, scope)
    if (request !== generation) return
    rows.value = rows.value.map(item => item.key === key ? updated : item)
    reset()
    MessagePlugin.success(t('promptSettings.saved', { scope: name }))
  } catch (cause: any) {
    if (request === generation) error.value = cause?.message || t('promptSettings.saveFailed')
  } finally {
    if (request === generation) saving.value = false
  }
}

async function setQualityEnabled(value: boolean) {
  const key = qualitySwitchKey.value
  if (!key || qualitySaving.value) return
  const previous = qualityEnabled.value
  qualityEnabled.value = value
  qualitySaving.value = true
  try {
    await updateSystemSetting(key, value)
  } catch (cause: any) {
    if (qualitySwitchKey.value === key) qualityEnabled.value = previous
    MessagePlugin.error(cause?.message || t('promptSettings.qualityScoringSwitchFailed'))
  } finally {
    qualitySaving.value = false
  }
}

function restoreDefault() {
  if (!selected.value?.row.id || loading.value || saving.value || dismissReset) return
  const key = selectedKey.value
  const request = generation
  const scope = libraryId.value || undefined
  const name = scopeName.value
  const close = () => { dialog.destroy(); dismissReset = undefined }
  const dialog = DialogPlugin.confirm({
    header: t('promptSettings.restoreDefault'),
    body: t('promptSettings.restoreConfirm', { scope: name, name: selected.value.name }),
    confirmBtn: t('promptSettings.restoreDefault'),
    cancelBtn: t('common.cancel'),
    onCancel: close,
    onClose: close,
    onConfirm: async () => {
      close()
      if (saving.value || request !== generation) return
      saving.value = true
      error.value = ''
      try {
        await resetPromptSetting(key.slice('prompt.'.length), scope)
        const result = await listPromptSettings(scope)
        if (request !== generation) return
        rows.value = result
        reset()
        MessagePlugin.success(t('promptSettings.restored', { scope: name }))
      } catch (cause: any) {
        if (request === generation) error.value = cause?.message || t('promptSettings.restoreFailed')
      } finally {
        if (request === generation) saving.value = false
      }
    },
  })
  dismissReset = close
}

function beforeUnload(event: BeforeUnloadEvent) {
  if (dirty.value) { event.preventDefault(); event.returnValue = '' }
}
defineExpose({ confirmLeave })
onMounted(() => { void initialize(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { disposed = true; generation++; dismissLeave?.(); dismissReset?.(); window.removeEventListener('beforeunload', beforeUnload) })
</script>

<template>
  <section class="prompt-settings" :aria-label="t('promptSettings.title')" :aria-busy="loading || catalogLoading">
    <header class="section-header">
      <h2>{{ t('promptSettings.title') }}</h2>
      <p>{{ t('promptSettings.description') }}</p>
    </header>
    <div class="prompt-scope">
      <label for="prompt-scope">{{ t('promptSettings.scope') }}</label>
      <t-select ref="scopeSelector" :value="libraryId" :options="scopeOptions"
        :aria-label="t('promptSettings.scope')" :disabled="saving || catalogLoading"
        @change="selectScope($event as string)" />
      <p class="prompt-hint">{{ t(libraryId ? 'promptSettings.libraryScopeHint' : 'promptSettings.globalScopeHint') }}</p>
    </div>
    <div class="prompt-feedback" aria-live="polite">
      <p v-if="catalogError" class="prompt-error" role="alert">{{ catalogError }}</p>
      <t-button v-if="catalogError" variant="text" :loading="catalogLoading" @click="initialize">{{ t('common.retry') }}</t-button>
      <p v-if="error && !invalid" class="prompt-error" role="alert">{{ error }}</p>
      <t-button v-if="error && !templates.length" variant="text" :loading="loading" @click="load">{{ t('common.retry') }}</t-button>
    </div>
    <div v-if="loading && !templates.length" class="prompt-loading"><t-loading :text="t('common.loading')" /></div>
    <t-empty v-else-if="!templates.length && !error" :description="t('promptSettings.empty')" />
    <div v-else-if="templates.length" class="prompt-workspace">
      <nav class="prompt-list" :aria-label="t('promptSettings.choose')">
        <button v-for="item in templates" :key="item.key" type="button"
          :aria-pressed="item.key === selectedKey" :disabled="saving" @click="selectTemplate(item.key)">
          {{ item.name }}
        </button>
      </nav>
      <form v-if="selected" class="prompt-editor" novalidate @submit.prevent="save">
        <div class="prompt-heading">
          <h3>{{ selected.name }}</h3>
          <t-tag variant="light" size="small">{{ selected.row.id ? t('promptSettings.customized') : t('promptSettings.default') }}</t-tag>
        </div>
        <p class="prompt-description">{{ selected.description }}</p>
        <div v-if="libraryId && selected.key === 'prompt.quality_scoring'" class="prompt-switch">
          <label>{{ t('promptSettings.qualityScoringEnabled') }}<t-switch :value="qualityEnabled" :loading="qualitySaving"
            :disabled="saving || loading" :aria-label="t('promptSettings.qualityScoringEnabled')"
            @change="setQualityEnabled($event as boolean)" /></label>
          <p class="prompt-hint">{{ t('promptSettings.qualityScoringEnabledHint') }}</p>
        </div>
        <label class="prompt-field">
          <span>{{ t('promptSettings.content') }}</span>
          <SafeTextarea ref="editor" v-model="draft" name="prompt-content" :disabled="saving"
            :status="invalid ? 'error' : 'default'" :autosize="{ minRows: 12, maxRows: 26 }" />
        </label>
        <p v-if="invalid" id="prompt-field-error" class="prompt-error" role="alert">{{ error }}</p>
        <p id="prompt-hint" class="prompt-hint">{{ t('promptSettings.effectHint') }}</p>
        <div class="prompt-actions">
          <span class="prompt-state" role="status">{{ dirty ? t('promptSettings.unsaved') : t('promptSettings.active') }}</span>
          <t-button variant="text" :disabled="!selected.row.id || saving || loading" @click="restoreDefault">{{ t('promptSettings.restoreDefault') }}</t-button>
          <t-button variant="outline" :disabled="!dirty || saving" @click="reset">{{ t('common.cancel') }}</t-button>
          <t-button type="submit" theme="primary" :loading="saving" :disabled="!dirty || loading">{{ t('common.save') }}</t-button>
        </div>
      </form>
    </div>
  </section>
</template>

<style scoped lang="less">
@import '../../assets/settings-layout.less';
.section-header h2 { margin: 0 0 8px; font-size: 20px; }
.section-header p, .prompt-description, .prompt-hint { color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.6; margin: 0; }
.prompt-scope { display: grid; grid-template-columns: auto minmax(160px, 320px); align-items: center; gap: 8px 12px; margin-top: 20px; }
.prompt-scope .prompt-hint { grid-column: 1 / -1; }
.prompt-scope :deep(.t-select__wrap) { min-width: 0; }
.prompt-feedback { min-height: 32px; display: flex; align-items: center; gap: 8px; }
.prompt-error { color: var(--td-error-color); margin: 0; overflow-wrap: anywhere; }
.prompt-loading { min-height: 300px; display: grid; place-items: center; }
.prompt-workspace { display: grid; grid-template-columns: 176px minmax(0, 1fr); gap: 24px; }
.prompt-list { display: flex; flex-direction: column; gap: 4px; }
.prompt-list button { padding: 11px 12px; border: 1px solid transparent; border-radius: var(--td-radius-medium); background: transparent; color: var(--td-text-color-primary); font: inherit; text-align: left; cursor: pointer; overflow-wrap: anywhere; }
.prompt-list button:hover { background: var(--td-bg-color-container-hover); }
.prompt-list button[aria-pressed='true'] { color: var(--td-brand-color); border-color: var(--td-brand-color); background: var(--td-brand-color-light); }
.prompt-list button:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.prompt-list button:disabled { cursor: not-allowed; opacity: .6; }
.prompt-editor { min-width: 0; }
.prompt-heading { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; }
.prompt-heading h3 { margin: 0; font-size: 16px; }
.prompt-description { margin: 8px 0 20px; }
.prompt-switch { margin: -8px 0 20px; }
.prompt-switch label { display: inline-flex; align-items: center; gap: 12px; font-weight: 500; }
.prompt-switch .prompt-hint { margin-top: 4px; }
.prompt-field { display: block; }
.prompt-field > span { display: block; margin-bottom: 8px; font-weight: 500; }
.prompt-field :deep(textarea) { font-family: var(--app-font-family-mono); line-height: 1.6; resize: none; }
.prompt-hint { margin-top: 10px; }
.prompt-actions { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 12px; margin-top: 20px; }
.prompt-state { flex: 1; color: var(--td-text-color-secondary); font-size: 13px; }
@container settings-content (max-width: @settings-narrow-width) {
  .prompt-scope { grid-template-columns: minmax(0, 1fr); }
  .prompt-workspace { grid-template-columns: minmax(0, 1fr); gap: 20px; }
  .prompt-list { flex-direction: row; flex-wrap: wrap; }
  .prompt-list button { padding: 8px 10px; }
}
</style>
