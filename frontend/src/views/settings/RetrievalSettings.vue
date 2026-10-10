<template>
  <div class="retrieval-settings">
    <div class="section-header">
      <h2>{{ t('retrievalSettings.title') }}</h2>
      <p class="section-description">{{ t('retrievalSettings.description') }}</p>
    </div>

    <div class="settings-group">
      <!-- Embedding Top K -->
      <div class="setting-item">
        <div class="setting-label-row">
          <span>{{ t('retrievalSettings.embeddingTopKLabel') }}</span>
          <span class="value-display">{{ localConfig.embedding_top_k }}</span>
        </div>
        <t-slider
          v-model="localConfig.embedding_top_k"
          :min="1"
          :max="100"
          :step="1"

          @change="handleParamChange"
        />
      </div>

      <!-- Vector Threshold -->
      <div class="setting-item">
        <div class="setting-label-row">
          <span>{{ t('retrievalSettings.vectorThresholdLabel') }}</span>
          <span class="value-display">{{ localConfig.vector_threshold.toFixed(2) }}</span>
        </div>
        <t-slider
          v-model="localConfig.vector_threshold"
          :min="0"
          :max="1"
          :step="0.05"

          @change="handleParamChange"
        />
      </div>

      <!-- Keyword Threshold -->
      <div class="setting-item">
        <div class="setting-label-row">
          <span>{{ t('retrievalSettings.keywordThresholdLabel') }}</span>
          <span class="value-display">{{ localConfig.keyword_threshold.toFixed(2) }}</span>
        </div>
        <t-slider
          v-model="localConfig.keyword_threshold"
          :min="0"
          :max="1"
          :step="0.05"

          @change="handleParamChange"
        />
      </div>

      <!-- Rerank Top K -->
      <div class="setting-item">
        <div class="setting-label-row">
          <span>{{ t('retrievalSettings.rerankTopKLabel') }}</span>
          <span class="value-display">{{ localConfig.rerank_top_k }}</span>
        </div>
        <t-slider
          v-model="localConfig.rerank_top_k"
          :min="1"
          :max="100"
          :step="1"

          @change="handleParamChange"
        />
      </div>

      <!-- Rerank Threshold -->
      <div class="setting-item">
        <div class="setting-label-row">
          <span>{{ t('retrievalSettings.rerankThresholdLabel') }}</span>
          <span class="value-display">{{ localConfig.rerank_threshold.toFixed(2) }}</span>
        </div>
        <t-slider
          v-model="localConfig.rerank_threshold"
          :min="-10"
          :max="10"
          :step="0.1"

          @change="handleParamChange"
        />
      </div>
      <div class="setting-item">
        <div class="setting-label-row"><span>{{ t('retrievalSettings.similarReportsThresholdLabel') }}</span></div>
        <t-input-number v-model="localConfig.similar_reports_threshold" :min="0.01" :max="1" :step="0.01" :decimal-places="2" :aria-label="t('retrievalSettings.similarReportsThresholdLabel')" @change="handleParamChange" />
      </div>
      <div class="setting-item">
        <div class="setting-label-row"><span>{{ t('retrievalSettings.similarReportsWindowDaysLabel') }}</span></div>
        <t-input-number v-model="localConfig.similar_reports_window_days" :min="1" :step="1" :decimal-places="0" :aria-label="t('retrievalSettings.similarReportsWindowDaysLabel')" @change="handleParamChange" />
        <p class="setting-desc">{{ t('retrievalSettings.similarReportsHint') }}</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, computed, onMounted, nextTick } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import {
  getTenantRetrievalConfig,
  updateTenantRetrievalConfig,
  type RetrievalConfig,
} from '@/api/retrieval'

const { t } = useI18n()
const defaultConfig: RetrievalConfig = {
  embedding_top_k: 50,
  vector_threshold: 0.15,
  keyword_threshold: 0.3,
  rerank_top_k: 10,
  rerank_threshold: 0.2,
  similar_reports_threshold: 0.85,
  similar_reports_window_days: 3,
}

const localConfig = reactive<RetrievalConfig>({ ...defaultConfig })
let initialConfig: RetrievalConfig = { ...defaultConfig }
let isInitializing = true

const loadConfig = async () => {
  try {
    const response = await getTenantRetrievalConfig()
    if (response.data) {
      const cfg = response.data
      Object.assign(localConfig, {
        ...(cfg.rrf_k !== undefined ? { rrf_k: cfg.rrf_k } : {}),
        ...(cfg.rrf_vector_weight !== undefined ? { rrf_vector_weight: cfg.rrf_vector_weight } : {}),
        ...(cfg.rrf_keyword_weight !== undefined ? { rrf_keyword_weight: cfg.rrf_keyword_weight } : {}),
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
    console.error('Failed to load retrieval config:', error)
  } finally {
    await nextTick()
    await nextTick()
    setTimeout(() => { isInitializing = false }, 100)
  }
}

const hasConfigChanged = (): boolean => {
  return JSON.stringify(localConfig) !== JSON.stringify(initialConfig)
}

const saveConfig = async () => {
  if (!hasConfigChanged()) return
  try {
    const response = await updateTenantRetrievalConfig({ ...localConfig })
    if (response.data) {
      initialConfig = { ...localConfig }
    }
    MessagePlugin.success(t('retrievalSettings.toasts.saveSuccess'))
  } catch (error: any) {
    console.error('Failed to save retrieval config:', error)
    const errorMessage = error?.message || 'Unknown error'
    MessagePlugin.error(t('retrievalSettings.toasts.saveFailed', { message: errorMessage }))
  }
}

let saveTimer: number | null = null
const debouncedSave = () => {
  if (isInitializing) return
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = window.setTimeout(() => {
    saveConfig().catch(() => {})
  }, 500)
}

const handleParamChange = () => debouncedSave()
onMounted(async () => {
  isInitializing = true
  await loadConfig()
})
</script>

<style lang="less" scoped>
.retrieval-settings {
  width: 100%;
}

.section-header {
  margin-bottom: 24px;

  h2 {
    font-size: 20px;
    font-weight: 600;
    color: var(--td-text-color-primary);
    margin: 0 0 6px 0;
  }

  .section-description {
    font-size: 13px;
    color: var(--td-text-color-secondary);
    margin: 0;
    line-height: 1.5;
  }
}

.settings-group {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.setting-item {
  padding: 16px 0;
  border-bottom: 1px solid var(--td-component-stroke);

  &:last-child {
    border-bottom: none;
  }
}

.setting-label {
  font-size: 14px;
  font-weight: 500;
  color: var(--td-text-color-primary);
  margin-bottom: 4px;
}

.setting-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 14px;
  font-weight: 500;
  color: var(--td-text-color-primary);
  margin-bottom: 10px;
}

.setting-desc {
  font-size: 12px;
  color: var(--td-text-color-secondary);
  margin: 0 0 8px 0;
  line-height: 1.5;
}

.required-mark {
  color: var(--td-error-color);
}

.warning-text {
  color: var(--td-warning-color) !important;
}

.setting-control-full {
  width: 100%;
}

.value-display {
  font-size: 13px;
  font-weight: 600;
  color: var(--td-brand-color);
  font-family: var(--app-font-family-mono);
}
</style>
