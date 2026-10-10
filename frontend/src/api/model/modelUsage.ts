export const MODEL_IN_USE_ERROR_CODE = 2300

export const KNOWN_MODEL_USAGE_BINDINGS = [
  'embedding_model',
  'summary_model',
  'image_processing_model',
  'vlm_model',
  'rerank_model',
  'light_model_tier',
  'entity_resolution_model_tier',
  'generation_model_tier',
  'embedding_model_tier',
  'rerank_model_tier',
] as const

export type KnownModelUsageBinding = typeof KNOWN_MODEL_USAGE_BINDINGS[number]

export interface ModelUsageResource {
  id: string
  name: string
  bindings: string[]
}

export interface ModelUsageDetails {
  model_tiers?: Array<ModelUsageResource & { scope: 'global' }>
  knowledge_bases: ModelUsageResource[]
  knowledge_base_total: number
}

export class ModelInUseError extends Error {
  readonly details: ModelUsageDetails

  constructor(details: ModelUsageDetails) {
    super('model is in use')
    this.name = 'ModelInUseError'
    this.details = details
  }
}

type UnknownRecord = Record<string, unknown>

function isRecord(value: unknown): value is UnknownRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function parseBindings(value: unknown): string[] | null {
  if (!Array.isArray(value) || value.some(binding => typeof binding !== 'string' || binding.trim() === '')) {
    return null
  }
  return [...value]
}

function parseResources(value: unknown): ModelUsageResource[] | null {
  if (!Array.isArray(value)) return null

  const resources: ModelUsageResource[] = []
  for (const item of value) {
    if (!isRecord(item) || typeof item.id !== 'string' || item.id.trim() === '' || typeof item.name !== 'string') {
      return null
    }
    const bindings = parseBindings(item.bindings)
    if (!bindings || bindings.length === 0) return null
    resources.push({ id: item.id, name: item.name, bindings })
  }
  return resources
}

function parseTotal(value: unknown, listed: number): number {
  if (typeof value === 'number' && Number.isFinite(value) && value >= listed) {
    return Math.floor(value)
  }
  return listed
}

export function parseModelUsageDetails(value: unknown): ModelUsageDetails | null {
  if (!isRecord(value)) return null

  const knowledgeBases = parseResources(value.knowledge_bases)
  if (!knowledgeBases) return null

  let tiers: ModelUsageDetails['model_tiers']
  if (value.model_tiers !== undefined) {
    const resources = parseResources(value.model_tiers)
    if (!resources || !Array.isArray(value.model_tiers)) return null
    tiers = []
    for (let index = 0; index < resources.length; index++) {
      const original = value.model_tiers[index]
      if (!isRecord(original) || (original.scope !== 'global' && original.scope !== 'team')) return null
      tiers.push({ ...resources[index]!, scope: original.scope === 'team' ? 'global' : original.scope })
    }
  }
  const details: ModelUsageDetails = {
    knowledge_bases: knowledgeBases,
    knowledge_base_total: parseTotal(value.knowledge_base_total, knowledgeBases.length),
  }
  if (tiers) details.model_tiers = tiers
  if (knowledgeBases.length === 0 && !tiers?.length) {
    return null
  }
  return details
}

export function modelInUseErrorFromRequest(error: unknown): ModelInUseError | null {
  if (!isRecord(error) || !isRecord(error.error) || error.error.code !== MODEL_IN_USE_ERROR_CODE) {
    return null
  }
  const details = parseModelUsageDetails(error.error.details)
  return details ? new ModelInUseError(details) : null
}

const knownBindings = new Set<string>(KNOWN_MODEL_USAGE_BINDINGS)

export function modelUsageBindingI18nKey(binding: string): string {
  const key = knownBindings.has(binding) ? binding : 'unknown'
  return `modelSettings.usage.bindings.${key}`
}

export function modelUsageResourceCount(resources: readonly ModelUsageResource[], total?: number): number {
  return typeof total === 'number' && Number.isFinite(total) && total >= resources.length
    ? Math.floor(total)
    : resources.length
}

export function modelUsageListTruncated(resources: readonly ModelUsageResource[], total?: number): boolean {
  return modelUsageResourceCount(resources, total) > resources.length
}

export type ModelUsageResourceKind = 'knowledge_base'

export type KnowledgeBaseModelUsageSection = 'models' | 'multimodal'

const knowledgeBaseUsageSections: Partial<Record<KnownModelUsageBinding, KnowledgeBaseModelUsageSection>> = {
  embedding_model: 'models',
  summary_model: 'models',
  image_processing_model: 'multimodal',
  vlm_model: 'multimodal',
}

export function modelUsageKnowledgeBaseSection(
  bindings: readonly string[],
): KnowledgeBaseModelUsageSection {
  for (const binding of bindings) {
    const section = knowledgeBaseUsageSections[binding as KnownModelUsageBinding]
    if (section) return section
  }
  return 'models'
}

export function modelUsageResourceRoute(
  kind: ModelUsageResourceKind,
  id: string,
  bindings: readonly string[] = [],
) {
  return { path: `/platform/knowledge-bases/${encodeURIComponent(id)}` }
}
