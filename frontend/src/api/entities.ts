import { get, post, patch, put } from '@/utils/request'

import { ENTITY_TYPES, type EntityType } from './entity-types'
export { ENTITY_TYPES } from './entity-types'
export type { EntityType } from './entity-types'
export interface EntityEnvelope<T> { success: boolean; data: T; error?: string }
export interface EntityActionEnvelope { success: boolean; error?: string }
export interface Entity {
  kind?: string
  code?: string
  attributes?: Record<string, unknown>
  revision?: number
  archived_at?: string | null
  id: string
  space_id: number
  library_id: string
  canonical_name: string
  entity_type: EntityType
  description?: string
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
  content_type?: string
  created_at: string
}
export interface EntityDetail { entity: Entity; entries: EntityEntry[] }
export interface EntityListParams {
  library_id: string
  entity?: string
  entity_type?: EntityType
  kind?: string
  include_archived?: boolean
  limit?: number
  offset?: number
}
function queryString(params?: EntityListParams & { status?: string }): string {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params || {})) if (value !== undefined && value !== '') query.set(key, String(value))
  return query.size ? `?${query}` : ''
}
const base = '/api/v1/entities'
export const listEntities = (params: EntityListParams) => get<EntityEnvelope<Entity[]>>(`${base}${queryString(params)}`)
export const getEntity = (id: string) => get<EntityEnvelope<EntityDetail>>(`${base}/${encodeURIComponent(id)}`)
export const mergeEntities = (input: { keep_entity_id: string; merge_entity_id: string }) => post<EntityActionEnvelope>(`${base}/merge`, input)
export interface EntityMergeRecord { id: string; keep_entity_id: string; merge_entity_id: string; actor?: string; created_at: string }
export const listEntityMergeRecords = (id: string) => get<EntityEnvelope<EntityMergeRecord[]>>(`${base}/${encodeURIComponent(id)}/merge-records`)
export interface EntityMergeSuggestion {
  id: string
  new_entity_id: string
  candidate_entity_id: string
  new_entity_name?: string
  candidate_entity_name?: string
  reason: string
  entry_id?: string
  status: 'pending' | 'accepted' | 'rejected'
  created_at: string
  decided_at?: string
}
export const listEntityMergeSuggestions = (params: EntityListParams & { status?: string }) => get<EntityEnvelope<EntityMergeSuggestion[]>>(`${base}/merge-suggestions${queryString(params)}`)
export const acceptEntityMergeSuggestion = (id: string) => post<EntityActionEnvelope>(`${base}/merge-suggestions/${encodeURIComponent(id)}/accept`, {})
export const rejectEntityMergeSuggestion = (id: string) => post<EntityActionEnvelope>(`${base}/merge-suggestions/${encodeURIComponent(id)}/reject`, {})
export interface EntityNameEmbeddingRebuildRun {
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
export interface EntityNameEmbeddingRebuildStatus { pending_count: number; latest_run?: EntityNameEmbeddingRebuildRun | null }
const rebuildPath = `${base}/name-embeddings/rebuild`
export const getEntityNameEmbeddingRebuildStatus = () => get<EntityEnvelope<EntityNameEmbeddingRebuildStatus>>(rebuildPath)
export const rebuildEntityNameEmbeddings = () => post<EntityEnvelope<EntityNameEmbeddingRebuildStatus>>(rebuildPath, {})

export interface ManagedEntityInput { name: string; kind: string; code: string; description: string; aliases: string[]; attributes: Record<string, unknown> }
export const createManagedEntity = (library: string, data: ManagedEntityInput) => post<EntityEnvelope<Entity>>(`/api/v1/libraries/${encodeURIComponent(library)}/entities`, data)
export const updateManagedEntity = (id: string, data: Partial<ManagedEntityInput> & { revision: number; archived?: boolean }) => patch<EntityEnvelope<Entity>>(`${base}/${encodeURIComponent(id)}`, data)
export const entityHistory = (id: string, page: number) => get<EntityEnvelope<import('./operation-logs').OperationLogsData>>(`${base}/${encodeURIComponent(id)}/history?page=${page}&page_size=20`)
export interface EntryEntityLink { entity: Entity; origin: string; excluded: boolean; evidence: import('@/types/search').SourceLocation[]; body_hash: string }
export const listEntryEntityLinks = (id: string) => get<EntityEnvelope<EntryEntityLink[]>>(`/api/v1/entries/${encodeURIComponent(id)}/entity-links`)
export const setEntryEntityLink = (id: string, entity: string, excluded: boolean) => put<EntityActionEnvelope>(`/api/v1/entries/${encodeURIComponent(id)}/entity-links/${encodeURIComponent(entity)}`, { excluded })
