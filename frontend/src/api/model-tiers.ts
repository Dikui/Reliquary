import { get, put } from '@/utils/request'

import type { ModelTierConfig, ModelTierSettings } from '@/types/modelTiers'
export * from '@/types/modelTiers'
export interface ModelTierEnvelope { success: boolean; data: ModelTierSettings; error?: string; message?: string }
export interface ModelTierScope { knowledge_base_id?: string; domain_id?: string }
export function modelTierSettingsPath(scope: ModelTierScope = {}): string {
  const query = new URLSearchParams()
  if (scope.knowledge_base_id) query.set('knowledge_base_id', scope.knowledge_base_id)
  else if (scope.domain_id) query.set('domain_id', scope.domain_id)
  return `/api/v1/tenants/model-tiers${query.size ? `?${query}` : ''}`
}
export const getModelTierSettings = (scope: ModelTierScope = {}) => get<ModelTierEnvelope>(modelTierSettingsPath(scope))
export const updateTeamModelTiers = (config: ModelTierConfig) => put<ModelTierEnvelope>(modelTierSettingsPath(), config)
