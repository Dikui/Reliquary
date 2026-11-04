import { get } from '@/utils/request'

export interface FailureMaterial { id: string; name: string; type: string; size: number; sha256: string; source?: string }
export interface IngestFailure {
  id: string; request_id: string; created_at: string; expires_at: string; transport: string
  api_key_id?: string; api_key_name?: string; library_id?: string; library?: string; item_index: number
  entry_id?: string; stage: string; error_code: string; reason: string; retryable: boolean; outcome: string
  package_status: string; missing: string[]; materials: FailureMaterial[]; version?: string; config?: Record<string, unknown>
  recovery_entry_id?: string; recovery_request_id?: string
  resolved: boolean
}
export interface FailurePage { items: IngestFailure[]; total: number; page: number; page_size: number }
export interface FailureMaterialRead { material: FailureMaterial; content?: string; offset: number; length: number; total_length: number; next_offset?: number | null }
export interface FailureQuery { page: number; page_size: number; from?: string; to?: string; api_key_id?: string; library_id?: string; error_code?: string; request_id?: string; id?: string; resolved?: boolean }
interface Response<T> { success: boolean; data: T; message?: string }
const path = '/api/v1/diagnostics/ingest-failures'
export const listIngestFailures = (query: FailureQuery) => get<Response<FailurePage>>(path, { params: query })
export const getIngestFailure = (id: string) => get<Response<IngestFailure>>(`${path}/${encodeURIComponent(id)}`)
const materialPath = (id: string, materialID: string) => `${path}/${encodeURIComponent(id)}/materials/${encodeURIComponent(materialID)}`
export const readFailureText = (id: string, materialID: string, offset = 0) => get<Response<FailureMaterialRead>>(materialPath(id, materialID), { params: { offset, length: 16000 } })
export const readFailureImage = (id: string, materialID: string) => get<Blob>(materialPath(id, materialID), { responseType: 'blob' })
