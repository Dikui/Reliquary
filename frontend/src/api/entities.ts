import { get, post, patch, put, del } from '@/utils/request'

export interface EntityEnvelope<T> { success: boolean; data: T; error?: string }
export interface EntityActionEnvelope { success: boolean; error?: string }
export interface Entity {
  id: string
  space_id: number
  canonical_name: string
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
export interface EntityFactPage { items: import('./stories').StoryFact[]; total: number }
export const getEntityFacts = (id: string, params?: EntityFactParams) =>
  get<EntityEnvelope<EntityFactPage>>(`${base}/${encodeURIComponent(id)}/facts${queryString(params)}`)
export const mergeEntities = (input: { keep_entity_id: string; merge_entity_id: string }) => post<EntityActionEnvelope>(`${base}/merge`, input)
export interface EntityMergeRecord { id: string; keep_entity_id: string; merge_entity_id: string; actor?: string; created_at: string }
export const listEntityMergeRecords = (id: string) => get<EntityEnvelope<EntityMergeRecord[]>>(`${base}/${encodeURIComponent(id)}/merge-records`)
export interface EntityInput { canonical_name: string; aliases: string[] }
export const createEntity = (input: EntityInput) => post<EntityEnvelope<Entity>>(base, input)
export const updateEntity = (id: string, input: EntityInput) => patch<EntityEnvelope<Entity>>(`${base}/${encodeURIComponent(id)}`, input)
export const deleteEntity = (id: string) => del<EntityActionEnvelope>(`${base}/${encodeURIComponent(id)}`)
export const setEntryEntities = (id: string, entity_ids: string[]) => put<EntityEnvelope<{ entities: Entity[]; entities_manual: boolean }>>(`/api/v1/entries/${encodeURIComponent(id)}/entities`, { entity_ids })
