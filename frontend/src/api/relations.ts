import { get, del } from '@/utils/request'
import type { SourceLocation } from '@/types/search'
import type { Entity } from './entities'
export interface RelationSource { entry_id: string; title: string; body_hash: string; evidence: Array<{ locations: SourceLocation[]; speaker?: { entity_id?: string; name: string } }> }
export interface EntityRelation { id: string; relation_type: string; direction: 'incoming' | 'outgoing'; peer: Entity; library_id: string; library_name: string; source_count: number; sources: RelationSource[] }
export interface RelationGroup { relation_type: string; relations: EntityRelation[] }
export const listEntityRelations = (id: string) => get<{ success: boolean; data: RelationGroup[]; message?: string }>(`/api/v1/entities/${encodeURIComponent(id)}/relations`)
export const deleteEntityRelation = (id: string) => del<{ success: boolean; message?: string }>(`/api/v1/relations/${encodeURIComponent(id)}`)
