<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watchPostEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { listSystemSettings, updateSystemSetting, type SystemSettingItem } from '@/api/system'
import SafeTextarea from '@/components/SafeTextarea.vue'

const { t } = useI18n()
const rows = ref<SystemSettingItem[]>([])
const selectedKey = ref('')
const draft = ref('')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const invalid = ref(false)
const editor = ref<InstanceType<typeof SafeTextarea>>()
let generation = 0
let pendingLeave: Promise<boolean> | undefined
let dismissLeave: (() => void) | undefined

const definitions = computed(() => [
  { key: 'prompt.article_processing', name: t('promptSettings.articleProcessing'), description: t('promptSettings.articleProcessingHint') },
  { key: 'prompt.summary_merge', name: t('promptSettings.summaryMerge'), description: t('promptSettings.summaryMergeHint') },
  { key: 'prompt.library_media', name: t('promptSettings.libraryMedia'), description: t('promptSettings.libraryMediaHint') },
  { key: 'prompt.library_reference', name: t('promptSettings.libraryReference'), description: t('promptSettings.libraryReferenceHint') },
  { key: 'prompt.library_own', name: t('promptSettings.libraryOwn'), description: t('promptSettings.libraryOwnHint') },
  { key: 'prompt.library_institutional', name: t('promptSettings.libraryInstitutional'), description: t('promptSettings.libraryInstitutionalHint') },
  { key: 'prompt.evaluation_questions', name: t('promptSettings.evaluationQuestions'), description: t('promptSettings.evaluationQuestionsHint') },
])
const templates = computed(() => definitions.value.flatMap(definition => {
  const row = rows.value.find(item => item.key === definition.key)
  return row && typeof row.value === 'string' ? [{ ...definition, row }] : []
}))
const selected = computed(() => templates.value.find(item => item.key === selectedKey.value))
const dirty = computed(() => !!selected.value && draft.value !== selected.value.row.value)
// TDesign places fallthrough ARIA attributes on its wrapper instead of the input.
watchPostEffect(() => {
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
  if (loading.value || saving.value) return
  const request = ++generation
  loading.value = true
  error.value = ''
  try {
    const result = await listSystemSettings()
    if (request !== generation) return
    rows.value = result
    selectedKey.value = templates.value[0]?.key ?? ''
    reset()
  } catch (cause: any) {
    if (request === generation) error.value = cause?.message || t('promptSettings.loadFailed')
  } finally {
    if (request === generation) loading.value = false
  }
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
  saving.value = true
  try {
    const updated = await updateSystemSetting(key, draft.value)
    if (request !== generation) return
    rows.value = rows.value.map(item => item.key === key ? updated : item)
    reset()
    MessagePlugin.success(t('promptSettings.saved'))
  } catch (cause: any) {
    if (request === generation) error.value = cause?.message || t('promptSettings.saveFailed')
  } finally {
    if (request === generation) saving.value = false
  }
}

function beforeUnload(event: BeforeUnloadEvent) {
  if (dirty.value) { event.preventDefault(); event.returnValue = '' }
}
defineExpose({ confirmLeave })
onMounted(() => { void load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { generation++; dismissLeave?.(); window.removeEventListener('beforeunload', beforeUnload) })
</script>

<template>
  <section class="prompt-settings" :aria-label="t('promptSettings.title')" :aria-busy="loading">
    <header class="section-header">
      <h2>{{ t('promptSettings.title') }}</h2>
      <p>{{ t('promptSettings.description') }}</p>
    </header>
    <div class="prompt-feedback" aria-live="polite">
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
        <label class="prompt-field">
          <span>{{ t('promptSettings.content') }}</span>
          <SafeTextarea ref="editor" v-model="draft" name="prompt-content" :disabled="saving"
            :status="invalid ? 'error' : 'default'" :autosize="{ minRows: 12, maxRows: 26 }" />
        </label>
        <p v-if="invalid" id="prompt-field-error" class="prompt-error" role="alert">{{ error }}</p>
        <p id="prompt-hint" class="prompt-hint">{{ t('promptSettings.effectHint') }}</p>
        <div class="prompt-actions">
          <span class="prompt-state" role="status">{{ dirty ? t('promptSettings.unsaved') : t('promptSettings.active') }}</span>
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
.prompt-field { display: block; }
.prompt-field > span { display: block; margin-bottom: 8px; font-weight: 500; }
.prompt-field :deep(textarea) { font-family: var(--app-font-family-mono); line-height: 1.6; resize: none; }
.prompt-hint { margin-top: 10px; }
.prompt-actions { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 12px; margin-top: 20px; }
.prompt-state { flex: 1; color: var(--td-text-color-secondary); font-size: 13px; }
@container settings-content (max-width: @settings-narrow-width) {
  .prompt-workspace { grid-template-columns: minmax(0, 1fr); gap: 20px; }
  .prompt-list { flex-direction: row; flex-wrap: wrap; }
  .prompt-list button { padding: 8px 10px; }
}
</style>
