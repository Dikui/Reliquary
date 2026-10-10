import type { ModelConfig } from '@/api/model'

export type ModelSelectorType = ModelConfig['type']

export function filterModelsByType(
  allModels: ModelConfig[],
  modelType: ModelSelectorType,
): ModelConfig[] {
  return allModels.filter((m) => m.type === modelType)
}
