import { get, post } from '@/utils/request'

import { ENTITY_TYPES, type EntityScope, type EntityType } from './entity-scope'
export { ENTITY_TYPES, entityScopeParams } from './entity-scope'
export type { EntityScope, EntityType } from './entity-scope'
export interface EntityEnvelope<T> { success: boolean; data: T; error?: string }
export interface EntityActionEnvelope { success: boolean; error?: string }
export interface Entity {
  id: string
  tenant_id: number
  canonical_name: string
  entity_type: EntityType
  description?: string
  merged_into_id?: string
  aliases?: string[]
  created_at: string
  updated_at: string
}
export interface EntityArticle {
  id: string
  knowledge_base_id: string
  title?: string
  file_name?: string
  created_at: string
}
export interface EntityDetail { entity: Entity; knowledge: EntityArticle[] }
export interface EntityListParams {
  scope?: EntityScope
  knowledge_base_id?: string
  entity?: string
  entity_type?: EntityType
  limit?: number
  offset?: number
}
function queryString(params?: EntityListParams & { status?: string }): string {
  if (params?.scope !== undefined && !['knowledge_base', 'all'].includes(params.scope)) throw new TypeError('scope must be knowledge_base or all')
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params || {})) if (value !== undefined && value !== '') query.set(key, String(value))
  return query.size ? `?${query}` : ''
}
const base = '/api/v1/entities'
export const listEntities = (params?: EntityListParams) => get<EntityEnvelope<Entity[]>>(`${base}${queryString(params)}`)
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
  knowledge_id?: string
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
