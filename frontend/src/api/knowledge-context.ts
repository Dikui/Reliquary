import { get, post } from '@/utils/request'

export interface KnowledgeContextEnvelope<T> {
  success: boolean
  data: T
  error?: string
}

export interface KnowledgeContextActionEnvelope {
  success: boolean
  error?: string
}

export interface KnowledgeEntity {
  id: string
  tenant_id: number
  knowledge_base_id: string
  canonical_name: string
  normalized_name?: string
  entity_type: string
  description?: string
  merged_into_id?: string
  aliases?: string[]
  created_at: string
  updated_at: string
}

export interface KnowledgeEvidence {
  id: string
  tenant_id: number
  knowledge_base_id: string
  knowledge_id: string
  version: number
  body_hash: string
  superseded_by_id?: string
  title: string
  body: string
  author?: string
  publisher?: string
  original_url?: string
  published_at?: string
  image_refs?: string[]
  status: 'current' | 'superseded' | string
  extraction_status: 'pending' | 'complete' | 'failed' | string
  extraction_error?: string
  source_kind?: 'roundup' | string
  created_at: string
}

export interface KnowledgeFact {
  id: string
  tenant_id: number
  knowledge_base_id: string
  subject_entity_id: string
  kind: 'attribute' | 'relation' | string
  predicate: string
  original_predicate?: string
  value?: unknown
  value_type?: string
  unit?: string
  object_entity_id?: string
  effective_from?: string
  effective_to?: string
  evidence_id: string
  quote: string
  status: 'current' | 'superseded' | 'corrected' | string
  supersedes_id?: string
  corrected_by?: string
  correction_note?: string
  created_at: string
}

export interface KnowledgeEvent {
  id: string
  tenant_id: number
  knowledge_base_id: string
  title: string
  event_type?: string
  description?: string
  occurred_at?: string
  entity_ids?: string[]
  evidence_ids?: string[]
  source_count: number
  heat: number
  created_at: string
  updated_at: string
  relations?: Array<{ relation: string; related_event_id?: string; related_title?: string; evidence_id?: string }>
}

export interface KnowledgeAttention {
  id: string
  tenant_id: number
  knowledge_base_id: string
  evidence_id: string
  event_id?: string
  novelty: number
  impact: number
  credibility: number
  relevance: number
  actionability: number
  score: number
  reason: string
  created_at: string
}

export interface KnowledgeContextQuery {
  knowledge_ids?: string[]
  entity_id?: string
  entity?: string
  predicate?: string
  relation?: string
  object_entity_id?: string
  min_value?: number
  max_value?: number
  unit?: string
  event_id?: string
  since?: string
  until?: string
  include_history?: boolean
  limit?: number
  offset?: number
}

export interface KnowledgeContextResult {
  entities: KnowledgeEntity[]
  facts: KnowledgeFact[]
  events: KnowledgeEvent[]
  evidence: KnowledgeEvidence[]
  attention: KnowledgeAttention[]
}

export interface KnowledgeFactCorrection {
  value?: unknown
  value_type?: string
  unit?: string
  effective_from?: string
  effective_to?: string
  quote: string
  correction_note: string
}

const base = (kbId: string) => `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/context`
export interface KnowledgeContextListParams {
  entity?: string
  entity_id?: string
  predicate?: string
  relation?: string
  object_entity_id?: string
  event_id?: string
  since?: string
  until?: string
  include_history?: boolean
  limit?: number
  offset?: number
}

function queryString(params?: KnowledgeContextListParams): string {
  if (!params) return ''
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') query.set(key, String(value))
  }
  const serialized = query.toString()
  return serialized ? `?${serialized}` : ''
}

export const queryKnowledgeContext = (kbId: string, query: KnowledgeContextQuery) =>
  post<KnowledgeContextEnvelope<KnowledgeContextResult>>(`${base(kbId)}/query`, query)

export const listKnowledgeContextEntities = (kbId: string, params?: KnowledgeContextListParams) =>
  get<KnowledgeContextEnvelope<KnowledgeEntity[]>>(`${base(kbId)}/entities${queryString(params)}`)

export const listKnowledgeContextEvents = (kbId: string, params?: KnowledgeContextListParams) =>
  get<KnowledgeContextEnvelope<KnowledgeEvent[]>>(`${base(kbId)}/events${queryString(params)}`)

export const listKnowledgeContextEvidence = (kbId: string, params?: KnowledgeContextListParams) =>
  get<KnowledgeContextEnvelope<KnowledgeEvidence[]>>(`${base(kbId)}/evidence${queryString(params)}`)

export const getKnowledgeEvidence = (kbId: string, evidenceId: string) =>
  get<KnowledgeContextEnvelope<KnowledgeEvidence>>(`${base(kbId)}/evidence/${encodeURIComponent(evidenceId)}`)

export const correctKnowledgeFact = (kbId: string, factId: string, correction: KnowledgeFactCorrection) =>
  post<KnowledgeContextEnvelope<KnowledgeFact>>(
    `${base(kbId)}/facts/${encodeURIComponent(factId)}/correction`,
    correction,
  )

export const mergeKnowledgeEntities = (
  kbId: string,
  input: { keep_entity_id: string; merge_entity_id: string },
) => post<KnowledgeContextActionEnvelope>(`${base(kbId)}/entities/merge`, input)

export const rebuildKnowledgeContext = (kbId: string, knowledgeIds?: string[]) =>
  post<KnowledgeContextEnvelope<{ queued: number }>>(`${base(kbId)}/rebuild`, {
    knowledge_ids: knowledgeIds,
  })
