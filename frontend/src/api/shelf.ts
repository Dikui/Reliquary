import { get, post } from '@/utils/request'
import type { Entity } from './entities'

export const SALE_STATUSES = ['GA', 'DEV', 'Planning', 'EOM', '待定'] as const
export interface ShelfCondition { module_type: string; part_ids: string[] }
export interface ShelfSaleStatus { region: string; status: string; planned_eom: boolean; notes: string; raw: string; conditions: ShelfCondition[] | null }
export interface ShelfNode {
  entity: Entity & { kind_label?: string }
  recorded: boolean
  as_of: string
  facts: { entity_id: string; field: string; value: unknown }[]
  shelf?: {
    sale_statuses: ShelfSaleStatus[]
    composition: { module_type: string; optional: boolean; parts: Entity[] }[]
  }
}
export interface ShelfQueryItem extends ShelfNode { partial_options: boolean; matching_conditions: ShelfCondition[][] }
export interface ShelfPage<T> { items: T[]; total: number; truncated: boolean; as_of?: string }
export interface ShelfQuery {
  ancestor_id?: string
  sale_statuses?: { region?: string; status?: string; planned_eom?: boolean; no_record?: boolean }[]
  parameters?: { field: string; min?: number; max?: number }[]
  certification?: { standard: string }
  document_gaps?: boolean
  limit?: number
  offset?: number
  as_of?: string
}
export interface ShelfStatusOperation { id: string; type: 'set_sale_statuses'; entity_id: string; revision: number; sale_statuses: ShelfSaleStatus[] }
export interface ShelfChangeset { id: string; revision: number; status: 'pending' | 'confirmed'; items: { id: string; conflicts: { type: string; message?: string }[] }[] }
export interface ShelfResponse<T> { success: boolean; data: T; message?: string }
const base = (library: string) => `/api/v1/libraries/${encodeURIComponent(library)}/shelf`
export const queryShelf = (library: string, input: ShelfQuery) => post<ShelfResponse<ShelfPage<ShelfQueryItem>>>(`${base(library)}/query`, input)
export const findShelf = (library: string, q: string, offset = 0) => get<ShelfResponse<ShelfPage<Entity & { kind_label?: string }>>>(`${base(library)}/find?${new URLSearchParams({ q, limit: '50', offset: String(offset) })}`)
export const getShelfNode = (library: string, id: string, asOf?: string) => get<ShelfResponse<ShelfNode>>(`${base(library)}/nodes/${encodeURIComponent(id)}${asOf ? `?${new URLSearchParams({ as_of: asOf })}` : ''}`)
export const submitShelfStatuses = (library: string, operations: ShelfStatusOperation[], confirmed: boolean) => post<ShelfResponse<ShelfChangeset>>(`${base(library)}/changesets`, {
  source: { type: 'web' }, operations, confirmed,
  ...(confirmed ? { decisions: operations.map(operation => ({ item_id: operation.id, action: 'apply' })) } : {}),
})
