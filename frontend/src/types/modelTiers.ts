export const MODEL_TIERS = ['light', 'entity_resolution', 'generation', 'embedding', 'rerank'] as const
export type ModelTier = typeof MODEL_TIERS[number]
export type ModelTierConfig = Partial<Record<ModelTier, string>>
export interface ResolvedModelTier { model_id: string; source: string }
export type ResolvedModelTiers = Partial<Record<ModelTier, ResolvedModelTier>>
export interface ModelTierSettings { overrides: ModelTierConfig; resolved: ResolvedModelTiers }
