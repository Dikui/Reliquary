import { get, post } from '@/utils/request'
import type { ClassifiedEntry } from '@/types/entryClassification'
import type { IntakeSourceInfo } from '@/api/library'

export type ReviewKind = 'intake'
export type IntakeReviewAction = 'approve' | 'reject' | 'retry' | 'discard' | 'supplement_url' | 'admit_unverified'
export interface ReviewEntry extends ClassifiedEntry {
  id: string
  created_at?: string
  title?: string
  file_name?: string
  parse_status?: string
  review_kind?: ReviewKind
  metadata?: { intake?: { decision?: string; reason?: string; source?: string; original_path?: string; source_info?: IntakeSourceInfo } }
}
export interface ReviewEnvelope { success: boolean; data: ReviewEntry; message?: string }
export interface PendingReviewEnvelope { success: boolean; data: ReviewEntry[]; total: number; page: number; page_size: number; message?: string }
export function listPendingEntry(input: { libraries?: string[]; kind?: ReviewKind; page: number; page_size: number }) {
  const query = new URLSearchParams({ page: String(input.page), page_size: String(input.page_size) })
  if (input.kind) query.set('kind', input.kind)
  if (input.libraries?.length) query.set('libraries', input.libraries.join(','))
  return get<PendingReviewEnvelope>(`/api/v1/entries/pending?${query}`)
}
export const resolveIntakeReview = (id: string, input: { action: IntakeReviewAction; source_info?: IntakeSourceInfo; confirmed?: boolean }) =>
  post<ReviewEnvelope>(`/api/v1/entries/${encodeURIComponent(id)}/review`, input, { timeout: 300000 })
