import { get, post } from '@/utils/request'
export type EntryBatchAction = 'delete' | 'reprocess' | 'review' | 'star' | 'content_type' | 'topics'
export interface EntryBatchFilters {
  libraries?: string[]; entity_ids?: string[]; topic_ids?: string[]; published_from?: string; published_to?: string
  include_rejected?: boolean; keyword?: string; file_type?: string; content_type?: string; starred?: boolean
  incomplete?: boolean; parse_status?: string; source?: string; source_kind?: string; start_time?: string; end_time?: string
}
export interface EntryBatchSelection { ids?: string[]; filters?: EntryBatchFilters }
export interface EntryBatchRequest { action: EntryBatchAction; selection: EntryBatchSelection; starred?: boolean; content_type?: string; topic_ids?: string[]; topic_mode?: 'add' | 'remove'; preview_fingerprint?: string }
export interface EntryBatchPreview { fingerprint: string; matched: number; eligible: number; skipped: number; blocked: number; reasons: { code: string; count: number }[]; samples: { id: string; title: string; library_id: string }[] }
export interface EntryBatch { batch_id: string; action: EntryBatchAction; status: 'pending' | 'running' | 'completed' | 'partial' | 'failed'; total: number; succeeded: number; failed: number; skipped: number; pending: number; running: number; warnings: number; items: { entry_id: string; title: string; library_id: string; status: 'pending' | 'running' | 'waiting' | 'succeeded' | 'failed' | 'skipped' | 'warning'; message?: string; warning?: string; task_id?: string }[]; page: number; page_size: number }
interface Envelope<T> { success: boolean; data: T; message?: string }
const base = '/api/v1/entries/batches'
export const previewEntryBatch = (input: EntryBatchRequest) => post<Envelope<EntryBatchPreview>>(`${base}/preview`, input)
export const submitEntryBatch = (input: EntryBatchRequest) => post<Envelope<EntryBatch>>(base, input)
export const getEntryBatch = (id: string, page = 1) => get<Envelope<EntryBatch>>(`${base}/${encodeURIComponent(id)}?page=${page}&page_size=20`)
export const retryFailedEntryBatch = (id: string) => post<Envelope<EntryBatch>>(`${base}/${encodeURIComponent(id)}/retry-failed`, {})
