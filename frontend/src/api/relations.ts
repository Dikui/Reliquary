import { get, del, post, patch } from '@/utils/request'
import type { SourceLocation } from '@/types/search'
import type { Entity } from './entities'
export interface RelationSource { entry_id: string; title: string; body_hash: string; evidence: Array<{ locations: SourceLocation[]; speaker?: { entity_id?: string; name: string } }> }
export interface EntityRelation { id: string; version?: number; relation_type: string; direction: 'incoming' | 'outgoing'; peer: Entity; library_id: string; library_name: string; source_count: number; sources: RelationSource[] }
export interface RelationGroup { relation_type: string; relations: EntityRelation[] }
export const listEntityRelations = (id: string) => get<{ success: boolean; data: RelationGroup[]; message?: string }>(`/api/v1/entities/${encodeURIComponent(id)}/relations`)
export const deleteEntityRelation = (id: string, version?: number) => del<{ success: boolean; message?: string }>(`/api/v1/relations/${encodeURIComponent(id)}${version === undefined ? '' : `?version=${version}`}`)

export interface ManagedRelationInput { subject_id: string; object_id: string; relation_type: string }
export const createManagedRelation = (library: string, data: ManagedRelationInput) => post<{ success: boolean; message?: string }>(`/api/v1/libraries/${encodeURIComponent(library)}/relations`, data)
export const updateManagedRelation = (id: string, data: ManagedRelationInput & { version: number }) => patch<{ success: boolean; message?: string }>(`/api/v1/relations/${encodeURIComponent(id)}`, data)
