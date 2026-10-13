import { get, post } from '@/utils/request'
import type { ContentType, ClassifiedEntry } from '@/types/entryClassification'
import type { IntakeSourceInfo } from '@/api/library'

export type ReviewKind = 'intake' | 'processing'
export type IntakeReviewAction = 'approve' | 'reject' | 'retry' | 'discard' | 'supplement_url' | 'admit_unverified' | 'convert_to_mine'
export type ProcessingReviewAction = 'confirm' | 'correct' | 'ignore'
export interface ReviewEntry extends ClassifiedEntry {
  id: string
  title?: string
  file_name?: string
  parse_status?: string
  review_kind?: ReviewKind
  processing_review_status?: string
  processing_review_content_type?: string
  processing_review_reason?: string
  metadata?: { intake?: { decision?: string; reason?: string; source?: string; original_path?: string; source_info?: IntakeSourceInfo } }
}
export interface ReviewEnvelope { success: boolean; data: ReviewEntry; message?: string }
export interface PendingReviewEnvelope { success: boolean; data: ReviewEntry[]; total: number; page: number; page_size: number; message?: string }
export function listPendingEntry(input: { kind?: ReviewKind; page: number; page_size: number }) {
  const query = new URLSearchParams({ page: String(input.page), page_size: String(input.page_size) })
  if (input.kind) query.set('kind', input.kind)
  return get<PendingReviewEnvelope>(`/api/v1/entries/pending?${query}`)
}
export const resolveIntakeReview = (id: string, input: { action: IntakeReviewAction; source_info?: IntakeSourceInfo; confirmed?: boolean }) =>
  post<ReviewEnvelope>(`/api/v1/entries/${encodeURIComponent(id)}/review`, input, { timeout: 300000 })
export const resolveProcessingReview = (id: string, input: { action: ProcessingReviewAction; content_type?: ContentType; confirm_remove_body?: boolean }) =>
  post<ReviewEnvelope>(`/api/v1/entries/${encodeURIComponent(id)}/processing-review`, input, { timeout: 300000 })
