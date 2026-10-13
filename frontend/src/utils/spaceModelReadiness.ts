import { listModels, type ModelConfig } from '@/api/model'

export interface SpaceModelReadiness {
  generationCount: number
  embeddingCount: number
  hasGeneration: boolean
  hasEmbedding: boolean
  /** 文档库默认开启向量/关键词检索时所需的模型是否齐备 */
  isReadyForDocumentLibrary: boolean
}

export function evaluateSpaceModelReadiness(models: ModelConfig[]): SpaceModelReadiness {
  const generationCount = models.filter((m) => m.type === 'KnowledgeQA').length
  const embeddingCount = models.filter((m) => m.type === 'Embedding').length
  const hasGeneration = generationCount > 0
  const hasEmbedding = embeddingCount > 0
  return {
    generationCount,
    embeddingCount,
    hasGeneration,
    hasEmbedding,
    isReadyForDocumentLibrary: hasGeneration && hasEmbedding,
  }
}

export async function fetchSpaceModelReadiness(): Promise<SpaceModelReadiness> {
  try {
    const models = await listModels()
    return evaluateSpaceModelReadiness(models || [])
  } catch {
    return evaluateSpaceModelReadiness([])
  }
}
