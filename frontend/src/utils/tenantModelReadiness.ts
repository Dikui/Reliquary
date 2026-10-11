import { listModels, type ModelConfig } from '@/api/model'

export interface TenantModelReadiness {
  generationCount: number
  embeddingCount: number
  hasGeneration: boolean
  hasEmbedding: boolean
  /** 文档库默认开启向量/关键词检索时所需的模型是否齐备 */
  isReadyForDocumentKb: boolean
}

export function evaluateTenantModelReadiness(models: ModelConfig[]): TenantModelReadiness {
  const generationCount = models.filter((m) => m.type === 'KnowledgeQA').length
  const embeddingCount = models.filter((m) => m.type === 'Embedding').length
  const hasGeneration = generationCount > 0
  const hasEmbedding = embeddingCount > 0
  return {
    generationCount,
    embeddingCount,
    hasGeneration,
    hasEmbedding,
    isReadyForDocumentKb: hasGeneration && hasEmbedding,
  }
}

export async function fetchTenantModelReadiness(): Promise<TenantModelReadiness> {
  try {
    const models = await listModels()
    return evaluateTenantModelReadiness(models || [])
  } catch {
    return evaluateTenantModelReadiness([])
  }
}
