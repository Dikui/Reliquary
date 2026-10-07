import { del, get, post, put } from '@/utils/request'
import type { ModelTierConfig } from '@/types/modelTiers'
import type { KnowledgeFact } from './knowledge-context'

export interface DomainField {
  key: string
  label: string
  aliases: string[]
  value_type: 'number' | 'text' | 'date' | 'boolean'
  unit_family: string
  entity_types: string[]
}
export interface DomainRelation {
  key: string
  label: string
  aliases: string[]
  subject_types: string[]
  object_types: string[]
}
export interface DomainUnit { name: string; aliases: string[]; factor: number }
export interface DomainUnitFamily { key: string; canonical_unit: string; units: DomainUnit[] }
export interface KnowledgeDomainInput {
  model_tiers?: ModelTierConfig
  name: string
  description: string
  entity_types: string[]
  fields: DomainField[]
  relations: DomainRelation[]
  unit_families: DomainUnitFamily[]
  extraction_hints: string
}
export interface KnowledgeDomain extends KnowledgeDomainInput {
  id: string
  tenant_id: number
  is_builtin: boolean
  created_by: string
  created_at: string
  updated_at: string
}
export interface DomainOpenField { name: string; count: number; example_facts: KnowledgeFact[] }
export interface DomainEnvelope<T> { success: boolean; data: T; error?: string; message?: string }
const base = '/api/v1/knowledge-domains'
const path = (id: string) => `${base}/${encodeURIComponent(id)}`
export const listKnowledgeDomains = () => get<DomainEnvelope<KnowledgeDomain[]>>(base)
export const getKnowledgeDomain = (id: string) => get<DomainEnvelope<KnowledgeDomain>>(path(id))
export const createKnowledgeDomain = (input: KnowledgeDomainInput) => post<DomainEnvelope<KnowledgeDomain>>(base, input)
export const updateKnowledgeDomain = (id: string, input: KnowledgeDomainInput) => put<DomainEnvelope<KnowledgeDomain>>(path(id), input)
export const deleteKnowledgeDomain = (id: string) => del<DomainEnvelope<null>>(path(id))
export const listDomainOpenFields = (id: string) => get<DomainEnvelope<DomainOpenField[]>>(`${path(id)}/open-fields`)
export const mapDomainOpenField = (id: string, openField: string, fieldKey: string) =>
  post<DomainEnvelope<KnowledgeDomain>>(`${path(id)}/field-mappings`, { open_field: openField, field_key: fieldKey })
export const createDomainField = (id: string, field: DomainField) =>
  post<DomainEnvelope<KnowledgeDomain>>(`${path(id)}/fields`, field)
