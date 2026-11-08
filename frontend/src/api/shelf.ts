import { get, post } from '@/utils/request'
import type { Entity } from './entities'
import type { OperationSource } from './operation-logs'

export const SALE_STATUSES = ['GA', 'DEV', 'Planning', 'EOM', '待定'] as const
export interface ShelfCondition { module_type: string; part_ids: string[] }
export interface ShelfSaleStatus { region: string; status: string; planned_eom: boolean; notes: string; raw: string; conditions: ShelfCondition[] | null }
export interface ShelfCertification {
  entity: Entity
  standard_name: string
  origin: 'self' | 'component' | 'ancestor'
  certification: { owner_id: string; standard: string; year?: number; certification_object: string; cell_part_id?: string; region: string; agency?: string; progress?: string; start_date?: string; obtained_date?: string; issued_date?: string; validity?: string; number?: string; cloud_url?: string; conditions: ShelfCondition[] | null }
}
export interface ShelfDocumentVersion {
  id: string; file_id: string; owner_id: string; requirement_id?: string; attachment_id?: string; cloud_url?: string; entry_id?: string
  version_label: string; stage: 'draft' | 'final'; ordinal: number; parse_status: string; parse_error?: string; file_category?: string
  conditions: ShelfCondition[] | null; occurred_at: string; source: OperationSource
}
export interface ShelfRequirement { name_zh?: string; name_en?: string; file_category?: string; stage_label?: string; due_text?: string; draft_due_stage?: string; final_due_stage?: string; owner?: string; priority?: string; source_list?: string; source_version?: string }
export interface ShelfChecklistItem {
  requirement: Entity; status: string; has_file: boolean; document?: ShelfDocumentVersion; document_name?: string; owner_id?: string
  origin?: string; conditions: ShelfCondition[] | null; ambiguous?: boolean
}
export interface ShelfOtherDocument { entity: Entity; document: ShelfDocumentVersion; owner: Entity; origin: string; conditions: ShelfCondition[] | null }
export interface ShelfNode {
  entity: Entity & { kind_label?: string }
  recorded: boolean
  as_of: string
  facts: { entity_id: string; field: string; value: unknown; conditions?: ShelfCondition[] | null; origin?: string }[]
  shelf?: {
    sale_statuses: ShelfSaleStatus[]
    composition: { module_type: string; optional: boolean; parts: Entity[] }[]
    current_stage?: string
    stage_owner_id?: string
    certifications?: ShelfCertification[]
    checklist?: ShelfChecklistItem[]
    parameters?: Record<string, { value: unknown; conditions: ShelfCondition[] | null }[]>
    other_documents?: ShelfOtherDocument[]
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
export interface ShelfExportInput { product_line_id?: string; query: ShelfQuery }
const base = (library: string) => `/api/v1/libraries/${encodeURIComponent(library)}/shelf`
export const exportShelf = (library: string, input: ShelfExportInput, signal?: AbortSignal) => post<Blob>(`${base(library)}/export`, input, { responseType: 'blob', timeout: 120000, signal })
export const queryShelf = (library: string, input: ShelfQuery) => post<ShelfResponse<ShelfPage<ShelfQueryItem>>>(`${base(library)}/query`, input)
export const findShelf = (library: string, q: string, offset = 0) => get<ShelfResponse<ShelfPage<Entity & { kind_label?: string }>>>(`${base(library)}/find?${new URLSearchParams({ q, limit: '50', offset: String(offset) })}`)
export const getShelfNode = (library: string, id: string, asOf?: string, combination?: Record<string, string>) => {
  const query = new URLSearchParams()
  if (asOf) query.set('as_of', asOf)
  if (combination) query.set('combination', JSON.stringify(combination))
  return get<ShelfResponse<ShelfNode>>(`${base(library)}/nodes/${encodeURIComponent(id)}${query.size ? `?${query}` : ''}`)
}
export const submitShelfStatuses = (library: string, operations: ShelfStatusOperation[], confirmed: boolean) => post<ShelfResponse<ShelfChangeset>>(`${base(library)}/changesets`, {
  source: { type: 'web' }, operations, confirmed,
  ...(confirmed ? { decisions: operations.map(operation => ({ item_id: operation.id, action: 'apply' })) } : {}),
})
