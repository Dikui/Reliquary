import { get, post } from '@/utils/request'
import type { IncompleteEntry, MissingMaterial } from '@/types/ingest'

export interface IncompleteEnvelope {
  success: boolean
  data: IncompleteEntry[]
  total: number
  page: number
  page_size: number
  message?: string
}

export interface IngestActionEnvelope {
  success: boolean
  data: { entry_id: string; status: string; message: string; completeness: MissingMaterial[] }
  message?: string
}

export function listIncompleteEntrys(input: { libraries?: string[]; page: number; page_size: number; include_attempted?: boolean }) {
  const query = new URLSearchParams({ page: String(input.page), page_size: String(input.page_size), include_attempted: String(input.include_attempted ?? true) })
  if (input.libraries?.length) query.set('libraries', input.libraries.join(','))
  return get<IncompleteEnvelope>(`/api/v1/ingest/incomplete?${query}`)
}

export const acceptIncomplete = (id: string) =>
  post<IngestActionEnvelope>(`/api/v1/ingest/${encodeURIComponent(id)}/accept-incomplete`)

export const discardIncomplete = (id: string, reason?: string) =>
  post<IngestActionEnvelope>(`/api/v1/ingest/${encodeURIComponent(id)}/discard`, reason ? { reason } : {})
