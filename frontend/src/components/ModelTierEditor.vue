<template>
  <section class="model-tier-editor" :aria-label="t('modelTiers.title')">
    <div class="model-tier-heading"><div><h3>{{ t('modelTiers.title') }}</h3><p>{{ t(`modelTiers.scopeHint.${scope}`) }}</p></div><t-button variant="text" :loading="loading" :disabled="disabled" @click="refresh">{{ t('common.refresh') }}</t-button></div>
    <t-alert v-if="error" theme="error" :message="error" role="alert"><template #operation><t-button variant="text" @click="refresh">{{ t('modelTiers.retry') }}</t-button></template></t-alert>
    <div v-for="tier in MODEL_TIERS" :key="tier" class="model-tier-row">
      <div class="model-tier-info"><label :id="`${id}-${tier}`">{{ t(`modelTiers.names.${tier}`) }}</label><p>{{ t(`modelTiers.uses.${tier}`) }}</p></div>
      <div class="model-tier-control">
        <ModelSelector :model-type="modelTypeForTier(tier)" :selected-model-id="modelValue[tier] || ''" :all-models="models"
          :disabled="disabled || loading || (tier === 'embedding' && embeddingLocked)" clearable :aria-label="t(`modelTiers.names.${tier}`)"
          :placeholder="t(scope === 'global' ? 'modelTiers.useDefault' : 'modelTiers.inherit')"
          @update:selected-model-id="value => setTier(tier, value)" @add-model="openModels(tier)" />
        <p v-if="loading" class="model-tier-current" role="status">{{ t('common.loading') }}</p>
        <p v-else-if="!error" class="model-tier-current">{{ t('modelTiers.effective', { model: modelName(resolved[tier]?.model_id), source: sourceName(resolved[tier]?.source) }) }}</p>
        <p v-if="tier === 'embedding' && embeddingLocked" class="model-tier-current">{{ t('knowledgeEditor.models.embeddingLocked') }}</p>
      </div>
    </div>
    <p class="model-tier-footnote">{{ t('modelTiers.explicitChoice') }}</p>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getModelTierSettings, MODEL_TIERS, type ModelTier, type ModelTierConfig, type ResolvedModelTiers } from '@/api/model-tiers'
import { type ModelConfig } from '@/api/model'
import { modelTypeForTier, previewModelTiers } from '@/utils/modelTiers'
import { useResourcesStore } from '@/stores/resources'
import { useUIStore } from '@/stores/ui'
import ModelSelector from './ModelSelector.vue'
const props = withDefaults(defineProps<{ modelValue: ModelTierConfig; scope: 'global' | 'knowledge_base'; allModels?: ModelConfig[]; legacyTiers?: ModelTierConfig; beforeNavigate?: () => Promise<boolean>; disabled?: boolean; embeddingLocked?: boolean }>(), { disabled: false, embeddingLocked: false })
const emit = defineEmits<{ 'update:modelValue': [value: ModelTierConfig]; resolved: [value: ResolvedModelTiers]; refresh: [] }>()
const { t, te } = useI18n()
const resources = useResourcesStore()
const ui = useUIStore()
const id = useId()
const loading = ref(false), error = ref('')
const inherited = ref<ResolvedModelTiers>({})
let request = 0
const models = computed(() => props.allModels || resources.allModels)
const resolved = computed(() => {
  const parent = { ...inherited.value }
  for (const tier of MODEL_TIERS) if (!parent[tier]?.model_id && props.legacyTiers?.[tier]) parent[tier] = { model_id: props.legacyTiers[tier]!, source: 'legacy' }
  return previewModelTiers(props.modelValue, parent, props.scope)
})
function modelName(modelId?: string): string { const model = models.value?.find(item => item.id === modelId); return model?.display_name || model?.name || modelId || t('modelTiers.unconfigured') }
function sourceName(source?: string): string { const key = `modelTiers.sources.${source || 'unconfigured'}`; return te(key) ? t(key) : t('modelTiers.sources.default') }
function setTier(tier: ModelTier, value: string) { if (props.disabled || loading.value || (tier === 'embedding' && props.embeddingLocked)) return; emit('update:modelValue', { ...props.modelValue, [tier]: value || '' }) }
async function openModels(tier: ModelTier) { if (props.beforeNavigate && !await props.beforeNavigate()) return; ui.openSettings('models', modelTypeForTier(tier).toLowerCase()) }
async function loadInherited() {
  const current = ++request
  loading.value = true; error.value = ''; inherited.value = {}
  try {
    const [response] = await Promise.all([getModelTierSettings({}), props.allModels ? Promise.resolve() : resources.ensureModels()])
    if (!response.success) throw new Error(response.error || response.message || t('modelTiers.loadFailed'))
    if (current === request) inherited.value = Object.fromEntries(Object.entries(response.data?.resolved || {}).filter(([, value]) => props.scope !== 'global' || value.source !== 'global'))
  } catch (cause) { if (current === request) error.value = cause instanceof Error ? cause.message : t('modelTiers.loadFailed') }
  finally { if (current === request) loading.value = false }
}
watch(() => props.scope, loadInherited, { immediate: true })
watch(resolved, value => emit('resolved', value), { immediate: true })
function refresh() { emit('refresh'); void loadInherited() }
onBeforeUnmount(() => { request++ })
</script>
<style scoped lang="less">
@import '../assets/settings-layout.less';

.model-tier-editor { color: var(--td-text-color-primary); }
.model-tier-heading { display: flex; justify-content: space-between; gap: 12px; align-items: flex-start; }
.model-tier-heading h3 { font-size: 16px; margin: 0 0 6px; }
.model-tier-heading p, .model-tier-info p, .model-tier-footnote { color: var(--td-text-color-secondary); line-height: 1.6; font-size: 13px; margin: 0; }
.model-tier-row { display: grid; grid-template-columns: minmax(0, 2fr) minmax(0, 3fr); gap: 24px; border-bottom: 1px solid var(--td-component-stroke); padding: 16px 0; }
.model-tier-info label { display: block; font-weight: 500; margin-bottom: 4px; }
.model-tier-current { font-size: 12px; color: var(--td-text-color-secondary); min-height: 20px; margin: 6px 0 0; overflow-wrap: anywhere; }
.model-tier-control { min-width: 0; }
.model-tier-footnote { margin-top: 12px; }
@container settings-content (max-width: @settings-narrow-width) {
  .model-tier-row { grid-template-columns: minmax(0, 1fr); gap: 12px; }
  .model-tier-heading { flex-wrap: wrap; }
}
@media (max-width: 600px) { .model-tier-row { grid-template-columns: 1fr; gap: 8px; } }
</style>
