<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { controlEmbeddingRebuild, getEmbeddingRebuild, type EmbeddingRebuildAction, type EmbeddingRebuildData } from '@/api/embedding-rebuild'
import type { ModelConfig } from '@/api/model'

const props = withDefaults(defineProps<{ modelId: string; models?: ModelConfig[]; disabled?: boolean }>(), { models: () => [], disabled: false })
const { t, locale } = useI18n()
const data = ref<EmbeddingRebuildData | null>(null)
const loading = ref(false), acting = ref(false), error = ref('')
const run = computed(() => data.value?.latest_run)
const active = computed(() => !!run.value && ['pending', 'running'].includes(run.value.status))
const unfinished = computed(() => !!run.value && ['pending', 'running', 'paused'].includes(run.value.status))
const canResume = computed(() => !!run.value && ['paused', 'failed'].includes(run.value.status))
const modelName = computed(() => {
  const model = props.models.find(item => item.id === props.modelId)
  return model?.display_name || model?.name || props.modelId || t('modelTiers.unconfigured')
})
const progress = computed(() => Math.min(run.value?.total_count || 0, (run.value?.completed_count || 0) + (run.value?.failed_count || 0)))
const runModelMatches = computed(() => !run.value || (run.value.model_id === props.modelId && data.value?.current_model_matches !== false))
const canStart = computed(() => !unfinished.value || (!active.value && !runModelMatches.value))
let generation = 0, disposed = false
let pollTimer: ReturnType<typeof setTimeout> | undefined

function schedulePoll() {
  clearTimeout(pollTimer)
  if (!disposed && active.value) pollTimer = setTimeout(() => { void load() }, 2000)
}
function accept(result: { success: boolean; data: EmbeddingRebuildData; message?: string }) {
  if (!result.success || !result.data) throw new Error(result.message || t('embeddingRebuild.loadFailed'))
  data.value = result.data
}
async function load() {
  if (loading.value || acting.value || disposed) return
  const current = ++generation
  loading.value = true; error.value = ''
  try {
    const result = await getEmbeddingRebuild()
    if (current === generation && !disposed) accept(result)
  } catch (cause: any) {
    if (current === generation && !disposed) error.value = cause?.message || t('embeddingRebuild.loadFailed')
  } finally {
    if (current === generation && !disposed) { loading.value = false; schedulePoll() }
  }
}
async function control(action: EmbeddingRebuildAction) {
  if (acting.value || disposed || (action === 'start' && (props.disabled || !props.modelId || !canStart.value))) return
  if (action !== 'start' && !run.value) return
  if (['resume', 'retry_failed'].includes(action) && (!runModelMatches.value || props.disabled)) return
  const current = ++generation
  clearTimeout(pollTimer)
  loading.value = false; acting.value = true; error.value = ''
  try {
    const result = await controlEmbeddingRebuild(action, action === 'start' ? undefined : run.value?.id)
    if (current === generation && !disposed) accept(result)
  } catch (cause: any) {
    if (current === generation && !disposed) error.value = cause?.message || t('embeddingRebuild.actionFailed')
  } finally {
    if (current === generation && !disposed) { acting.value = false; schedulePoll() }
  }
}
function number(value: number) { return new Intl.NumberFormat(locale.value).format(value) }
onMounted(() => { void load() })
onBeforeUnmount(() => { disposed = true; generation++; clearTimeout(pollTimer) })
</script>

<template>
  <section class="embedding-rebuild" :aria-label="t('embeddingRebuild.title')" :aria-busy="loading || acting">
    <div class="embedding-rebuild-heading">
      <div><h3>{{ t('embeddingRebuild.title') }}</h3><p>{{ t('embeddingRebuild.description') }}</p></div>
      <t-button variant="text" size="small" :disabled="loading || acting" :loading="loading" @click="load">{{ t(error ? 'common.retry' : 'common.refresh') }}</t-button>
    </div>
    <p class="rebuild-model">{{ t('embeddingRebuild.currentModel', { name: modelName }) }}</p>
    <p v-if="disabled" class="rebuild-hint">{{ t('embeddingRebuild.saveFirst') }}</p>
    <p v-else-if="!modelId" class="rebuild-hint">{{ t('embeddingRebuild.configureFirst') }}</p>
    <t-alert v-if="error" theme="error" :message="error" role="alert" />
    <p v-if="loading && !data" role="status">{{ t('common.loading') }}</p>
    <div v-if="run" class="rebuild-progress" role="status" aria-live="polite">
      <p>{{ t('embeddingRebuild.runModel', { name: run.model_name || run.model_id, dimension: run.dimension }) }}</p>
      <p>{{ t(`embeddingRebuild.status.${run.status}`) }}</p>
      <progress :value="progress" :max="run.total_count || 1" :aria-label="t('embeddingRebuild.progress')" />
      <dl class="rebuild-counts"><div><dt>{{ t('embeddingRebuild.total') }}</dt><dd>{{ number(run.total_count) }}</dd></div><div><dt>{{ t('embeddingRebuild.completed') }}</dt><dd>{{ number(run.completed_count) }}</dd></div><div><dt>{{ t('embeddingRebuild.failed') }}</dt><dd>{{ number(run.failed_count) }}</dd></div></dl>
      <p v-if="run.error" class="rebuild-error" role="alert">{{ run.error }}</p>
      <p v-if="!runModelMatches" class="rebuild-hint">{{ t('embeddingRebuild.modelChanged') }}</p>
    </div>
    <p v-else-if="data" class="rebuild-hint">{{ t('embeddingRebuild.noRun') }}</p>
    <div class="rebuild-actions">
      <t-popconfirm v-if="canStart" :content="t('embeddingRebuild.confirm', { name: modelName })" :confirm-btn="{ content: t('embeddingRebuild.start'), theme: 'primary' }" :cancel-btn="{ content: t('common.cancel') }" :disabled="disabled || !modelId || loading || acting || !data" @confirm="control('start')">
        <t-button theme="primary" variant="outline" :disabled="disabled || !modelId || loading || acting || !data" :loading="acting">{{ t('embeddingRebuild.start') }}</t-button>
      </t-popconfirm>
      <t-button v-if="active" variant="outline" :disabled="acting" :loading="acting" @click="control('pause')">{{ t('embeddingRebuild.pause') }}</t-button>
      <t-button v-if="canResume" theme="primary" variant="outline" :disabled="acting || disabled || !runModelMatches" :loading="acting" @click="control('resume')">{{ t('embeddingRebuild.resume') }}</t-button>
      <t-button v-if="run && run.failed_count > 0 && !active" variant="outline" :disabled="acting || disabled || !runModelMatches" :loading="acting" @click="control('retry_failed')">{{ t('embeddingRebuild.retryFailed') }}</t-button>
    </div>
    <p class="rebuild-hint">{{ t('embeddingRebuild.searchHint') }}</p>
    <details v-if="data?.failed_items.length" class="rebuild-failures">
      <summary>{{ t('embeddingRebuild.failures') }}</summary>
      <p v-if="run && run.failed_count > data.failed_items.length" class="rebuild-hint">{{ t('embeddingRebuild.failureLimit', { shown: data.failed_items.length, total: run.failed_count }) }}</p>
      <ul><li v-for="item in data.failed_items" :key="item.id"><strong>{{ t(`embeddingRebuild.kinds.${item.kind}`) }}</strong> <code>{{ item.source_id }}</code><p>{{ item.error }}</p><small>{{ t('embeddingRebuild.attempts', { count: item.attempts }) }}</small></li></ul>
    </details>
  </section>
</template>

<style scoped lang="less">
.embedding-rebuild { margin-top: 24px; padding-top: 24px; border-top: 1px solid var(--td-component-stroke); color: var(--td-text-color-primary); }
.embedding-rebuild-heading { display: flex; justify-content: space-between; gap: 12px; align-items: flex-start; }
h3 { margin: 0 0 8px; font-size: 16px; }
p { margin: 6px 0; line-height: 1.6; overflow-wrap: anywhere; }
.embedding-rebuild-heading p, .rebuild-hint { font-size: 13px; color: var(--td-text-color-secondary); }
.rebuild-model, .rebuild-progress { font-size: 13px; margin-top: 16px; }
progress { display: block; width: 100%; height: 10px; margin: 12px 0; accent-color: var(--td-brand-color); }
.rebuild-counts { display: flex; flex-wrap: wrap; gap: 16px 36px; margin: 12px 0; }
dt { color: var(--td-text-color-secondary); }
dd { margin: 4px 0 0; font-variant-numeric: tabular-nums; }
.rebuild-actions { display: flex; flex-wrap: wrap; gap: 8px; margin: 16px 0; }
.rebuild-error { color: var(--td-error-color); }
.rebuild-failures { margin-top: 16px; font-size: 13px; }
summary { cursor: pointer; color: var(--td-brand-color); border-radius: var(--td-radius-small); }
summary:hover { text-decoration: underline; }
summary:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
ul { padding-left: 20px; max-height: 320px; overflow: auto; scrollbar-gutter: stable; }
li { margin: 12px 0; overflow-wrap: anywhere; }
small { color: var(--td-text-color-secondary); }
@container settings-content (max-width: 720px) { .embedding-rebuild-heading { flex-wrap: wrap; } }
</style>
