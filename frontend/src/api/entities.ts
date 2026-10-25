import { get, post, patch, put, del } from '@/utils/request'

import { ENTITY_TYPES, type EntityType } from './entity-types'
export { ENTITY_TYPES } from './entity-types'
export type { EntityType } from './entity-types'
export interface EntityEnvelope<T> { success: boolean; data: T; error?: string }
export interface EntityActionEnvelope { success: boolean; error?: string }
export interface Entity {
  id: string
  space_id: number
  canonical_name: string
  entity_type: EntityType
  merged_into_id?: string
  aliases?: string[]
  created_at: string
  updated_at: string
}
export interface EntityEntry {
  id: string
  library_id: string
  library_name?: string
  title?: string
  file_name?: string
  created_at: string
}
export interface EntityDetail { entity: Entity; entries: EntityEntry[] }
export interface EntityListParams {
  entity?: string
  entity_type?: EntityType
  limit?: number
  offset?: number
}
function queryString(params?: EntityListParams | EntityFactParams): string {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params || {})) if (value !== undefined && value !== '') query.set(key, String(value))
  return query.size ? `?${query}` : ''
}
const base = '/api/v1/entities'
export const listEntities = (params?: EntityListParams) => get<EntityEnvelope<Entity[]>>(`${base}${queryString(params)}`)
export const getEntity = (id: string) => get<EntityEnvelope<EntityDetail>>(`${base}/${encodeURIComponent(id)}`)
export interface EntityFactParams {
  from?: string
  to?: string
  other_entity_id?: string
  direction?: 'incoming' | 'outgoing'
  limit?: number
  offset?: number
}
export interface EntityGraphEdge {
  subject_entity_id: string
  object_entity_id: string
  latest_action: string
  fact_count: number
}
export interface EntityGraph { center: Entity; nodes: Entity[]; edges: EntityGraphEdge[] }
export interface EntityFactPage { items: import('./stories').StoryFact[]; total: number }
export const getEntityGraph = (id: string, params?: Pick<EntityFactParams, 'from' | 'to'>) =>
  get<EntityEnvelope<EntityGraph>>(`${base}/${encodeURIComponent(id)}/graph${queryString(params)}`)
export const getEntityFacts = (id: string, params?: EntityFactParams) =>
  get<EntityEnvelope<EntityFactPage>>(`${base}/${encodeURIComponent(id)}/facts${queryString(params)}`)
export const mergeEntities = (input: { keep_entity_id: string; merge_entity_id: string }) => post<EntityActionEnvelope>(`${base}/merge`, input)
export interface EntityMergeRecord { id: string; keep_entity_id: string; merge_entity_id: string; actor?: string; created_at: string }
export const listEntityMergeRecords = (id: string) => get<EntityEnvelope<EntityMergeRecord[]>>(`${base}/${encodeURIComponent(id)}/merge-records`)
export interface EntityInput { canonical_name: string; entity_type: EntityType; aliases: string[] }
export const createEntity = (input: EntityInput) => post<EntityEnvelope<Entity>>(base, input)
export const updateEntity = (id: string, input: EntityInput) => patch<EntityEnvelope<Entity>>(`${base}/${encodeURIComponent(id)}`, input)
export const deleteEntity = (id: string) => del<EntityActionEnvelope>(`${base}/${encodeURIComponent(id)}`)
export const setEntryEntities = (id: string, entity_ids: string[]) => put<EntityEnvelope<{ entities: Entity[]; entities_manual: boolean }>>(`/api/v1/entries/${encodeURIComponent(id)}/entities`, { entity_ids })

export type CandidateStatus = 'pending' | 'ignored' | 'approved' | 'aliased'
export interface EntityCandidate {
  id: string
  name: string
  entity_type: EntityType
  status: CandidateStatus
  entity_id: string | null
  occurrence_count: number
}
export interface CandidatePage { items: EntityCandidate[]; total: number }
export interface CandidateFact {
  fact: Omit<import('./stories').StoryFact, 'sources'>
  endpoint: 'subject' | 'object'
  created_at: string
}
export interface CandidateDetail { candidate: EntityCandidate; occurrences: CandidateFact[]; total: number }
export const listEntityCandidates = (status: CandidateStatus = 'pending', limit = 50, offset = 0) =>
  get<EntityEnvelope<CandidatePage>>(`/api/v1/entity-candidates?status=${status}&limit=${limit}&offset=${offset}`)
export const getEntityCandidate = (id: string, limit = 50, offset = 0) =>
  get<EntityEnvelope<CandidateDetail>>(`/api/v1/entity-candidates/${encodeURIComponent(id)}?limit=${limit}&offset=${offset}`)
export const reviewEntityCandidate = (id: string, action: 'approve' | 'alias' | 'ignore', entityId?: string) =>
  post<EntityEnvelope<EntityCandidate>>(`/api/v1/entity-candidates/${encodeURIComponent(id)}/${action}`, entityId ? { entity_id: entityId } : {})
