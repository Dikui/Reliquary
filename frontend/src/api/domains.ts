import { get, post, patch, put, del } from '@/utils/request'
import type { EntityEnvelope, EntityActionEnvelope } from './entities'

export interface Domain {
  id: string
  name: string
  definition: string
}
export interface DomainInput { name: string; definition: string }
const base = '/api/v1/domains'
export const listDomains = () => get<EntityEnvelope<Domain[]>>(base)
export const createDomain = (input: DomainInput) => post<EntityEnvelope<Domain>>(base, input)
export const updateDomain = (id: string, input: DomainInput) => patch<EntityEnvelope<Domain>>(`${base}/${encodeURIComponent(id)}`, input)
export const deleteDomain = (id: string) => del<EntityActionEnvelope>(`${base}/${encodeURIComponent(id)}`)
export const setEntryDomains = (id: string, domain_ids: string[]) => put<EntityEnvelope<{ domains: Domain[]; domains_manual: boolean }>>(`/api/v1/entries/${encodeURIComponent(id)}/domains`, { domain_ids })
