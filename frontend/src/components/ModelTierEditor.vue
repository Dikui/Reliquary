<template>
  <section class="model-tier-editor settings-form" :aria-label="t('modelTiers.title')">
    <div class="model-tier-heading"><div><h3 v-if="!hideTitle">{{ t('modelTiers.title') }}</h3><p>{{ t(scope === 'space' ? 'modelTiers.scopeHint.space' : 'modelTiers.scopeHint.library') }}</p></div><Button variant="ghost" :loading="loading" :disabled="disabled" @click="refresh">{{ t('common.refresh') }}</Button></div>
    <div v-if="error" role="alert" class="settings-error">{{ error }}<div class="settings-actions"><Button variant="ghost" @click="refresh">{{ t('modelTiers.retry') }}</Button></div></div>
    <div v-for="tier in tiers" :key="tier" class="model-tier-row">
      <div class="model-tier-info"><label :id="`${id}-${tier}`">{{ t(`modelTiers.names.${tier}`) }}</label><p>{{ t(`modelTiers.uses.${tier}`) }}</p></div>
      <div class="model-tier-control">
        <ModelSelector :model-type="modelTypeForTier(tier)" :selected-model-id="modelValue[tier] || ''" :all-models="models"
          :disabled="disabled || loading || (tier === 'embedding' && embeddingLocked)" :aria-label="t(`modelTiers.names.${tier}`)"
          :placeholder="t('modelTiers.useDefault')"
          @update:selected-model-id="value => setTier(tier, value)" @add-model="openModels(tier)" />
        <p v-if="loading" class="model-tier-current" role="status">{{ t('common.loading') }}</p>
        <p v-else-if="!error" class="model-tier-current">{{ t('modelTiers.effective', { model: modelName(resolved[tier]?.model_id), source: sourceName(resolved[tier]?.source) }) }}</p>
        <p v-if="tier === 'embedding' && embeddingLocked" class="model-tier-current">{{ t('libraryEditor.models.embeddingLocked') }}</p>
      </div>
    </div>
    <p v-if="scope === 'library' && (tiers.includes('light') || tiers.includes('entity_resolution'))" class="model-tier-footnote">{{ t('modelTiers.explicitChoice') }}</p>
  </section>
</template>
<script setup lang="ts">
import { Button } from '@/components/ui'
import '@/styles/settings-form.css'
import { computed, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MODEL_TIERS, type ModelTier, type ModelTierConfig, type ResolvedModelTiers } from '@/api/model-tiers'
import { type ModelConfig } from '@/api/model'
import { modelTypeForTier, previewModelTiers } from '@/utils/modelTiers'
import { useResourcesStore } from '@/stores/resources'
import { useUIStore } from '@/stores/ui'
import ModelSelector from './ModelSelector.vue'
const props = withDefaults(defineProps<{ modelValue: ModelTierConfig; allModels?: ModelConfig[]; hideTitle?: boolean; beforeNavigate?: () => Promise<boolean>; disabled?: boolean; embeddingLocked?: boolean; tiers?: readonly ModelTier[]; scope?: 'library' | 'space' }>(), { disabled: false, embeddingLocked: false, tiers: () => MODEL_TIERS, scope: 'library' })
const emit = defineEmits<{ 'update:modelValue': [value: ModelTierConfig]; resolved: [value: ResolvedModelTiers]; refresh: [] }>()
const { t, te } = useI18n()
const resources = useResourcesStore()
const ui = useUIStore()
const id = useId()
const loading = ref(false), error = ref('')
const models = computed(() => props.allModels || resources.allModels)
const resolved = computed(() => {
  const generation = props.modelValue.generation || ''
  const inherited: ResolvedModelTiers = generation ? Object.fromEntries(['light', 'entity_resolution'].map(tier => [tier, { model_id: generation, source: 'generation' }])) : {}
  return previewModelTiers(props.modelValue, props.scope === 'library' ? inherited : {}, props.scope)
})
function modelName(modelId?: string): string { const model = models.value?.find(item => item.id === modelId); return model?.display_name || model?.name || modelId || t('modelTiers.unconfigured') }
function sourceName(source?: string): string { const key = `modelTiers.sources.${source || 'unconfigured'}`; return te(key) ? t(key) : t('modelTiers.sources.default') }
function setTier(tier: ModelTier, value: string) { if (props.disabled || loading.value || (tier === 'embedding' && props.embeddingLocked)) return; emit('update:modelValue', { ...props.modelValue, [tier]: value || '' }) }
async function openModels(tier: ModelTier) { if (props.beforeNavigate && !await props.beforeNavigate()) return; ui.openSettings('models', modelTypeForTier(tier).toLowerCase()) }
watch(resolved, value => emit('resolved', value), { immediate: true })
async function refresh() {
  if (loading.value) return
  loading.value = true; error.value = ''
  try { await resources.ensureModels(true); emit('refresh') }
  catch { error.value = t('modelTiers.loadFailed') }
  finally { loading.value = false }
}
</script>

<style scoped>
 .model-tier-heading { display: flex; gap: var(--space-3); justify-content: space-between; }
.model-tier-heading p, .model-tier-info p, .model-tier-current, .model-tier-footnote { color: var(--muted); font-size: var(--text-sm); }
.model-tier-row { display: grid; grid-template-columns: 224px minmax(0, 1fr); gap: var(--space-6); padding: var(--space-4) 0; border-top: 2px solid var(--rule); }
.model-tier-control { min-width: 0; }
.model-tier-info label { font-weight: 800; }
@container settings-content (max-width: 720px) { .model-tier-row { grid-template-columns: minmax(0, 1fr); gap: var(--space-2); } }
@media (max-width: 640px) { .model-tier-row { grid-template-columns: minmax(0, 1fr); gap: var(--space-2); } }
</style>
