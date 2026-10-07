import { MODEL_TIERS, type ModelTier, type ModelTierConfig, type ResolvedModelTiers } from '../types/modelTiers'

export function copyModelTiers(config?: ModelTierConfig): ModelTierConfig {
  return Object.fromEntries(MODEL_TIERS.map(tier => [tier, config?.[tier]?.trim() || '']))
}
export function modelTypeForTier(tier: ModelTier): 'KnowledgeQA' | 'Embedding' | 'Rerank' {
  return tier === 'embedding' ? 'Embedding' : tier === 'rerank' ? 'Rerank' : 'KnowledgeQA'
}
export function previewModelTiers(overrides: ModelTierConfig, inherited: ResolvedModelTiers, scope: string): ResolvedModelTiers {
  return Object.fromEntries(MODEL_TIERS.map(tier => [tier,
    overrides[tier]?.trim() ? { model_id: overrides[tier]!.trim(), source: scope } : inherited[tier] || { model_id: '', source: 'unconfigured' },
  ]))
}
