import { get, patch, post } from '@/utils/request'
import type { OperationSource } from './operation-logs'
import type { ShelfResponse } from './shelf'

export interface ShelfNameConfirmation { name: string; other_entity_id: string; other_kind: string }
export interface ShelfReviewConflict {
  type: string; message?: string; details?: unknown; manual_before?: Record<string, unknown>; candidates?: ShelfNameConfirmation[]
}
export interface ShelfImportColumnFeedback {
  worksheet: string; column: string; header: string; nonempty_rows: number; examples: string[]
}
export interface ShelfImportCertification {
  name?: string; owner_id: string; standard: string; year?: number
  certification_object: string; region: string; cell_part_id?: string
  [key: string]: unknown
}
export interface ShelfReviewOperation {
  id: string
  type: string
  entity_id?: string
  revision?: number
  certification_owner?: { name: string; kind: string; product_line_id?: string; mapped_fields?: string[] }
  certification?: ShelfImportCertification
  parent_id?: string
  import_location?: { worksheet: string; cells: string[]; values?: Record<string, string> }
  [key: string]: unknown
}
export interface ShelfReviewDecision {
  item_id: string
  action: 'apply' | 'discard' | 'defer' | 'replace' | 'keep' | 'retire' | 'same_entity' | 'return_to_submitter'
  replacements?: ShelfReviewOperation[]
  replacement?: ShelfReviewOperation
  target_entity_id?: string
  target_revision?: number
  name_confirmations?: ShelfNameConfirmation[]
}
export interface ShelfReviewItem {
  id: string; entity_id?: string; operation: ShelfReviewOperation
  before: unknown; after: unknown; conflicts: ShelfReviewConflict[]; decision?: ShelfReviewDecision
}
export interface ShelfReviewChangeset {
  id: string; revision: number; status: 'pending' | 'confirmed'; source: OperationSource
  items: ShelfReviewItem[]; created_at: string; deferred_changeset_id?: string
}
export interface ShelfReviewPage { items: ShelfReviewChangeset[]; total: number; page: number; page_size: number }
export interface ShelfReviewInput { revision: number; decisions: ShelfReviewDecision[] }
const base = (library: string) => `/api/v1/libraries/${encodeURIComponent(library)}/shelf/changesets`
export const listShelfChangesets = (library: string, page = 1) => get<ShelfResponse<ShelfReviewPage>>(`${base(library)}?${new URLSearchParams({ status: 'pending', page: String(page), page_size: '20' })}`)
export const getShelfChangeset = (library: string, id: string) => get<ShelfResponse<ShelfReviewChangeset>>(`${base(library)}/${encodeURIComponent(id)}`)
export const saveShelfReview = (library: string, id: string, input: ShelfReviewInput) => patch<ShelfResponse<ShelfReviewChangeset>>(`${base(library)}/${encodeURIComponent(id)}`, input)
export const confirmShelfChangeset = (library: string, id: string, input: ShelfReviewInput) => post<ShelfResponse<ShelfReviewChangeset>>(`${base(library)}/${encodeURIComponent(id)}/confirm`, input)
