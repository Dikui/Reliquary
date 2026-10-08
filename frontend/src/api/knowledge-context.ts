import { del, get, post, put } from '@/utils/request'
import { knowledgeContextBasePath, type KnowledgeContextScopeParams } from './knowledge-context-scope'
export type { KnowledgeContextScope } from './knowledge-context-scope'

export interface KnowledgeContextEnvelope<T> {
  success: boolean
  data: T
  error?: string
  excluded_missing_published_at_count?: number
}

export interface KnowledgeContextActionEnvelope {
  success: boolean
  error?: string
}

export interface KnowledgeEntity {
  id: string
  tenant_id: number
  knowledge_base_id?: string | null
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
  knowledge_base_name?: string
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
  published_at_missing?: boolean
  repost_sources?: KnowledgeRepostSource[]
  image_refs?: string[]
  status: 'current' | 'superseded' | string
  extraction_status: 'pending' | 'complete' | 'failed' | string
  extraction_error?: string
  extraction_coverage?: 'complete' | 'partial'
  extraction_mode?: 'template_only' | 'full'
  processed_segments?: number
  total_segments?: number
  model_calls?: KnowledgeModelCall[]
  source_kind?: 'roundup' | string
  created_at: string
}

export interface KnowledgeFact {
  id: string
  tenant_id: number
  knowledge_base_id: string
  knowledge_base_name?: string
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
  correction_actor?: string
  created_at: string
}

export interface KnowledgeEvent {
  id: string
  tenant_id: number
  knowledge_base_id: string
  knowledge_base_name?: string
  title: string
  event_type?: string
  description?: string
  occurred_at?: string
  entity_ids?: string[]
  evidence_ids?: string[]
  sources?: Array<{ knowledge_base_id: string; knowledge_base_name: string }>
  source_count: number
  excluded_missing_published_at_count?: number
  repost_sources?: KnowledgeRepostSource[]
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

export interface KnowledgeContextQuery extends KnowledgeContextScopeParams {
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
  template_only_message_count?: number
  excluded_missing_published_at_count?: number
  published_at_missing_source_ids?: string[]
  entities: KnowledgeEntity[]
  facts: KnowledgeFact[]
  events: KnowledgeEvent[]
  evidence: KnowledgeEvidence[]
  attention: KnowledgeAttention[]
}

export interface KnowledgeFocusEntity extends KnowledgeEntity {
  workflow_ids?: string[]
  knowledge_base_ids?: string[]
}

export interface KnowledgeFocusTopic {
  id: string
  name: string
  kind: 'topic' | 'object' | 'event' | string
  knowledge_base_id: string
  knowledge_base_name?: string
  workflow_ids?: string[]
}

export interface KnowledgeFocusScope {
  starred_entities: KnowledgeFocusEntity[]
  starred_topics: KnowledgeFocusTopic[]
  tracked_entities: KnowledgeFocusEntity[]
  tracked_topics: KnowledgeFocusTopic[]
  available_topics: KnowledgeFocusTopic[]
}

export interface KnowledgeRepostSource {
  id: string
  knowledge_id: string
  original_url?: string
  publisher?: string
  author?: string
  published_at?: string
  published_at_missing?: boolean
  match_method: string
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
export interface KnowledgeContextListParams extends KnowledgeContextScopeParams {
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

export const getKnowledgeFocusScope = (params: KnowledgeContextScopeParams) =>
  get<KnowledgeContextEnvelope<KnowledgeFocusScope>>(`/api/v1/knowledge-context/focus${queryString(params)}`)

export const starKnowledgeEntity = (entityId: string) =>
  put<KnowledgeContextActionEnvelope>(`/api/v1/knowledge-context/entities/${encodeURIComponent(entityId)}/star`)
export const unstarKnowledgeEntity = (entityId: string) =>
  del<KnowledgeContextActionEnvelope>(`/api/v1/knowledge-context/entities/${encodeURIComponent(entityId)}/star`)
export const starKnowledgeTopic = (tagId: string) =>
  put<KnowledgeContextActionEnvelope>(`/api/v1/knowledge-context/topics/${encodeURIComponent(tagId)}/star`)
export const unstarKnowledgeTopic = (tagId: string) =>
  del<KnowledgeContextActionEnvelope>(`/api/v1/knowledge-context/topics/${encodeURIComponent(tagId)}/star`)

export const queryKnowledgeContext = (kbId: string, query: KnowledgeContextQuery) =>
  post<KnowledgeContextEnvelope<KnowledgeContextResult>>(`${knowledgeContextBasePath(kbId, query.scope)}/query`, query)

export const listKnowledgeContextEntities = (kbId: string, params?: KnowledgeContextListParams) =>
  get<KnowledgeContextEnvelope<KnowledgeEntity[]>>(`${knowledgeContextBasePath(kbId, params?.scope)}/entities${queryString(params)}`)

export const listKnowledgeContextEvents = (kbId: string, params?: KnowledgeContextListParams) =>
  get<KnowledgeContextEnvelope<KnowledgeEvent[]>>(`${knowledgeContextBasePath(kbId, params?.scope)}/events${queryString(params)}`)

export const listKnowledgeContextEvidence = (kbId: string, params?: KnowledgeContextListParams) =>
  get<KnowledgeContextEnvelope<KnowledgeEvidence[]>>(`${knowledgeContextBasePath(kbId, params?.scope)}/evidence${queryString(params)}`)

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

export const getGlobalKnowledgeEntityContext = (entityId: string, includeHistory = true) =>
  get<KnowledgeContextEnvelope<KnowledgeContextResult>>(`/api/v1/knowledge-context/entities/${encodeURIComponent(entityId)}?include_history=${includeHistory}`)
export const getGlobalKnowledgeEvidence = (evidenceId: string) =>
  get<KnowledgeContextEnvelope<KnowledgeEvidence>>(`/api/v1/knowledge-context/evidence/${encodeURIComponent(evidenceId)}`)
export const correctGlobalKnowledgeFact = (factId: string, correction: KnowledgeFactCorrection) =>
  post<KnowledgeContextEnvelope<KnowledgeFact>>(`/api/v1/knowledge-context/facts/${encodeURIComponent(factId)}/correction`, correction)
export const mergeGlobalKnowledgeEntities = (input: { keep_entity_id: string; merge_entity_id: string }) =>
  post<KnowledgeContextActionEnvelope>('/api/v1/knowledge-context/entities/merge', input)

export interface KnowledgeModelCall { stage: string; tier: string; model_id?: string; calls: number }
export interface KnowledgeMergeSuggestion {
  id: string; new_entity_id: string; candidate_entity_id: string; new_entity_name?: string; candidate_entity_name?: string;
  reason: string; evidence_id: string; quote?: string; knowledge_base_id: string; status: 'pending' | 'accepted' | 'rejected'; created_at: string; decided_at?: string
}
export const listKnowledgeMergeSuggestions = (params: KnowledgeContextListParams & { status?: string }) =>
  get<KnowledgeContextEnvelope<KnowledgeMergeSuggestion[]>>(`/api/v1/knowledge-context/merge-suggestions${queryString(params)}`)
export const acceptKnowledgeMergeSuggestion = (id: string) =>
  post<KnowledgeContextActionEnvelope>(`/api/v1/knowledge-context/merge-suggestions/${encodeURIComponent(id)}/accept`, {})
export const rejectKnowledgeMergeSuggestion = (id: string) =>
  post<KnowledgeContextActionEnvelope>(`/api/v1/knowledge-context/merge-suggestions/${encodeURIComponent(id)}/reject`, {})

export interface KnowledgeNameEmbeddingRebuildRun {
  id: string
  status: 'pending' | 'running' | 'completed' | 'failed' | string
  reason: string
  error?: string
  model_id: string
  dimension: number
  processed_count: number
  total_count: number
  model_calls: number
  attempts: number
  created_at: string
  updated_at: string
  started_at?: string
  finished_at?: string
}

export interface KnowledgeNameEmbeddingRebuildStatus {
  pending_count: number
  latest_run?: KnowledgeNameEmbeddingRebuildRun | null
}

const nameEmbeddingRebuildPath = '/api/v1/knowledge-context/name-embeddings/rebuild'
export const getKnowledgeNameEmbeddingRebuildStatus = () =>
  get<KnowledgeContextEnvelope<KnowledgeNameEmbeddingRebuildStatus>>(nameEmbeddingRebuildPath)
export const rebuildKnowledgeNameEmbeddings = () =>
  post<KnowledgeContextEnvelope<KnowledgeNameEmbeddingRebuildStatus>>(nameEmbeddingRebuildPath, {})
