<template>
  <div class="rq-retrieval-settings settings-form">
      <p class="rq-retrieval-description">{{ t('retrievalSettings.description') }}</p>
      <p v-if="loadError" role="alert">{{ loadError }} <Button @click="loadConfig">{{ t('common.retry') }}</Button></p>
      <p v-if="saveError" role="alert">{{ saveError }}</p>
      <form novalidate ref="nativeForm" @submit.prevent>
      <div v-for="field in fields" :key="field.key" class="rq-retrieval-field">
        <label :for="`cmdk-retrieval-${field.key}`">{{ field.label }}</label>
        <div class="rq-retrieval-control">
          <input :id="`cmdk-retrieval-${field.key}`" v-model.number="localConfig[field.key]" :type="field.range ? 'range' : 'number'" :min="field.min" :max="field.max" :step="field.step" required :disabled="configLoading || !!loadError || saving" :aria-describedby="field.hint ? `cmdk-retrieval-${field.key}-hint` : undefined" @change="handleNativeChange" />
          <output v-if="field.range" :for="`cmdk-retrieval-${field.key}`">{{ localConfig[field.key] }}</output>
        </div>
        <p v-if="field.hint" :id="`cmdk-retrieval-${field.key}-hint`">{{ field.hint }}</p>
      </div>
      </form>

  </div>
</template>

<script setup lang="ts">
import { reactive, computed, onMounted, onBeforeUnmount, nextTick, ref } from 'vue'
import { Button, toast } from '@/components/ui'
import { useI18n } from 'vue-i18n'
import {
  getSpaceRetrievalConfig,
  updateSpaceRetrievalConfig,
  type RetrievalConfig,
} from '@/api/retrieval'

const props = withDefaults(defineProps<{ swiss?: boolean }>(), { swiss: false })
const { t } = useI18n()
const configLoading = ref(false), loadError = ref(''), saveError = ref(''), saving = ref(false)
const nativeForm = ref<HTMLFormElement | null>(null)
const fields = computed(() => [
  { key: 'embedding_top_k' as const, label: t('retrievalSettings.embeddingTopKLabel'), min: 1, max: 100, step: 1, range: true },
  { key: 'vector_threshold' as const, label: t('retrievalSettings.vectorThresholdLabel'), min: 0, max: 1, step: 0.05, range: true },
  { key: 'keyword_threshold' as const, label: t('retrievalSettings.keywordThresholdLabel'), min: 0, max: 1, step: 0.05, range: true },
  { key: 'rerank_top_k' as const, label: t('retrievalSettings.rerankTopKLabel'), min: 1, max: 100, step: 1, range: true },
  { key: 'rerank_threshold' as const, label: t('retrievalSettings.rerankThresholdLabel'), min: -10, max: 10, step: 0.1, range: true },
  { key: 'rrf_image_weight' as const, label: t('retrievalSettings.imageWeightLabel'), min: 0, max: 1, step: 0.05, hint: t('retrievalSettings.imageWeightHint') },
  { key: 'similar_reports_threshold' as const, label: t('retrievalSettings.similarReportsThresholdLabel'), min: 0.01, max: 1, step: 0.01 },
  { key: 'similar_reports_window_days' as const, label: t('retrievalSettings.similarReportsWindowDaysLabel'), min: 1, step: 1, hint: t('retrievalSettings.similarReportsHint') },
])
const defaultConfig: RetrievalConfig = {
  embedding_top_k: 50,
  vector_threshold: 0.15,
  keyword_threshold: 0.3,
  rerank_top_k: 10,
  rerank_threshold: 0.2,
  similar_reports_threshold: 0.85,
  similar_reports_window_days: 3,
  rrf_image_weight: 0.2,
}

const localConfig = reactive<RetrievalConfig>({ ...defaultConfig })
let initialConfig: RetrievalConfig = { ...defaultConfig }
let isInitializing = true

const loadConfig = async () => {
  configLoading.value = true; loadError.value = ''
  try {
    const response = await getSpaceRetrievalConfig()
    if (response.data) {
      const cfg = response.data
      Object.assign(localConfig, {
        ...cfg,
        ...(cfg.rrf_k !== undefined ? { rrf_k: cfg.rrf_k } : {}),
        ...(cfg.rrf_vector_weight !== undefined ? { rrf_vector_weight: cfg.rrf_vector_weight } : {}),
        ...(cfg.rrf_keyword_weight !== undefined ? { rrf_keyword_weight: cfg.rrf_keyword_weight } : {}),
        rrf_image_weight: cfg.rrf_image_weight ?? defaultConfig.rrf_image_weight,
        embedding_top_k: cfg.embedding_top_k || defaultConfig.embedding_top_k,
        vector_threshold: cfg.vector_threshold || defaultConfig.vector_threshold,
        keyword_threshold: cfg.keyword_threshold || defaultConfig.keyword_threshold,
        rerank_top_k: cfg.rerank_top_k || defaultConfig.rerank_top_k,
        rerank_threshold: cfg.rerank_threshold ?? defaultConfig.rerank_threshold,
        similar_reports_threshold: cfg.similar_reports_threshold > 0 && cfg.similar_reports_threshold <= 1 ? cfg.similar_reports_threshold : defaultConfig.similar_reports_threshold,
        similar_reports_window_days: cfg.similar_reports_window_days > 0 ? cfg.similar_reports_window_days : defaultConfig.similar_reports_window_days,
      })
      initialConfig = { ...localConfig }
    }
  } catch (error: any) {
    loadError.value = t('retrievalSettings.loadFailed')
  } finally {
    configLoading.value = false
    await nextTick()
    await nextTick()
    setTimeout(() => { isInitializing = false }, 100)
  }
}

const hasConfigChanged = (): boolean => {
  return JSON.stringify(localConfig) !== JSON.stringify(initialConfig)
}

const saveConfig = async () => {
  if (!hasConfigChanged() || saving.value) return
  if (nativeForm.value && !nativeForm.value.checkValidity()) return
  saving.value = true; saveError.value = ''
  const submitted = { ...localConfig }
  try {
    const response = await updateSpaceRetrievalConfig(submitted)
    if (response.data) {
      initialConfig = submitted
    }
    const feedback = toast
    feedback.success(t('retrievalSettings.toasts.saveSuccess'))
  } catch (error: any) {
    const errorMessage = error?.message || t('common.operationFailed')
    saveError.value = t('retrievalSettings.toasts.saveFailed', { message: errorMessage })
    const feedback = toast
    feedback.error(saveError.value)
  } finally {
    saving.value = false
  }
}

let saveTimer: number | null = null
const debouncedSave = () => {
  if (isInitializing) return
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = window.setTimeout(() => {
    saveTimer = null
    saveConfig().catch(() => {})
  }, 500)
}

const handleParamChange = () => debouncedSave()
const handleNativeChange = () => { if (nativeForm.value?.reportValidity()) debouncedSave() }
onBeforeUnmount(() => {
  if (saveTimer) { clearTimeout(saveTimer); if (!loadError.value && !configLoading.value) void saveConfig() }
})
onMounted(async () => {
  isInitializing = true
  await loadConfig()
})
</script>

<style scoped>
.rq-retrieval-settings { color: var(--text); }
.rq-retrieval-settings .rq-retrieval-description { margin: 0 0 var(--space-4); color: var(--muted); font-size: var(--type-note-size); }
.rq-retrieval-settings .rq-retrieval-field { padding-block: var(--space-4); border-bottom: 1px solid var(--border); }
.rq-retrieval-settings .rq-retrieval-field label { font-size: var(--type-label-size); font-weight: var(--type-label-weight); }
.rq-retrieval-settings .rq-retrieval-field p { color: var(--muted); font-size: var(--type-note-size); }
.rq-retrieval-settings .rq-retrieval-control { display: flex; align-items: center; gap: var(--space-3); margin-top: var(--space-2); }
.rq-retrieval-settings .rq-retrieval-control input { min-width: 0; width: 100%; border-radius: 0; accent-color: var(--text); }
.rq-retrieval-settings .rq-retrieval-control input[type="range"] { flex: 1 1 0; width: 0; min-width: 0; }
.rq-retrieval-settings .rq-retrieval-control output { flex: 0 0 56px; min-width: 56px; white-space: nowrap; overflow-wrap: normal; word-break: normal; text-align: right; }
.rq-retrieval-settings .rq-retrieval-control input[type="number"] { padding: var(--space-2); border: 1px solid var(--border); background: var(--surface); color: var(--text); font: var(--text-sm)/1.5 var(--mono); }
.rq-retrieval-settings output { font-family: var(--mono); }
</style>
