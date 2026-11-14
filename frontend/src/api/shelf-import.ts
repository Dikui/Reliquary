import { get, post, postUpload } from '@/utils/request'
import type { ShelfResponse } from './shelf'
import type { ShelfReviewChangeset, ShelfReviewPage } from './shelf-review'

export const IMPORT_KINDS = ['product_map', 'certification_progress', 'certification_report', 'overseas_sales', 'atq'] as const
export type ShelfImportKind = typeof IMPORT_KINDS[number]
export interface ShelfImportColumn { column: string; field: string; key?: string }
export interface ShelfImportSheet {
  worksheet: string; ignore?: boolean; product_line_id?: string; header_rows: number; columns: ShelfImportColumn[]
  // range limits the table to a source region; transpose reads records from columns.
  range?: string; transpose?: boolean
  owner_kind?: string; module_type?: string; progress?: string; check_values?: Record<string, boolean>
}
export interface ShelfImportDefinition {
  sheets: ShelfImportSheet[]; model_separators?: string[]; missing_values?: string[]
  status_values?: Record<string, { status: string; notes?: string }>; fill_meanings?: Record<string, string>
  market_values?: Record<string, 'required' | 'optional' | 'not_needed'>
  deadlines?: Record<string, { draft_stage?: string; final_stage: string }>
}
export interface ShelfImportPlanInput extends ShelfImportDefinition { key: string; name: string; file_kind: ShelfImportKind; revision: number }
export interface ShelfImportPlan { key: string; name: string; file_kind: ShelfImportKind; revision: number; definition: ShelfImportDefinition; last_file_name?: string }
const base = (library: string) => `/api/v1/libraries/${encodeURIComponent(library)}/shelf`
export const listShelfImportPlans = (library: string) => get<ShelfResponse<ShelfImportPlan[]>>(`${base(library)}/import-plans`)
export const listShelfImportBatches = (library: string, page = 1) => get<ShelfResponse<ShelfReviewPage>>(`${base(library)}/changesets?${new URLSearchParams({ source_type: 'import', page: String(page), page_size: '20' })}`)
export function uploadShelfImport(library: string, file: File, key: string, date: string) {
  const data = new FormData(); data.append('file', file); data.append('plan_key', key)
  if (date) data.append('file_date', date)
  return postUpload(`${base(library)}/imports`, data, undefined, { timeout: 120000 }) as Promise<ShelfResponse<ShelfReviewChangeset>>
}
export function suggestShelfImportPlan(library: string, file: File, key: string, kind: ShelfImportKind) {
  const data = new FormData(); data.append('file', file); data.append('plan_key', key); data.append('file_kind', kind)
  return postUpload(`${base(library)}/import-plans/suggest`, data, undefined, { timeout: 120000 }) as Promise<ShelfResponse<ShelfReviewChangeset>>
}
export const saveShelfImportPlan = (library: string, plan: ShelfImportPlanInput) => post<ShelfResponse<ShelfReviewChangeset>>(`${base(library)}/changesets`, {
  source: { type: 'web' }, confirmed: true, operations: [{ id: 'plan', type: 'save_import_plan', import_plan: plan }], decisions: [{ item_id: 'plan', action: 'apply' }],
})
