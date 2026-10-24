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
function queryString(params?: EntityListParams & { status?: string }): string {
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
export interface EntityInput { canonical_name: string; entity_type: EntityType; aliases: string[] }
export const createEntity = (input: EntityInput) => post<EntityEnvelope<Entity>>(base, input)
export const updateEntity = (id: string, input: EntityInput) => patch<EntityEnvelope<Entity>>(`${base}/${encodeURIComponent(id)}`, input)
export const deleteEntity = (id: string) => del<EntityActionEnvelope>(`${base}/${encodeURIComponent(id)}`)
export const setEntryEntities = (id: string, entity_ids: string[]) => put<EntityEnvelope<{ entities: Entity[]; entities_manual: boolean }>>(`/api/v1/entries/${encodeURIComponent(id)}/entities`, { entity_ids })
