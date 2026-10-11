import { get } from '@/utils/request'

export const OPERATION_TYPES = ['review_approve', 'review_reject', 'review_retry', 'review_discard', 'star', 'unstar', 'correct_content_type', 'correct_topics', 'merge_entities', 'create_topic', 'update_topic', 'delete_topic', 'ingest', 'write_report', 'processing_review_confirm', 'processing_review_correct', 'processing_review_ignore', 'review_supplement_url', 'review_admit_unverified', 'review_convert_to_mine'] as const
export type OperationType = typeof OPERATION_TYPES[number]
export interface OperationLog { id: string; actor: string; actor_name?: string; operation_type: OperationType; object_id: string; before: unknown; after: unknown; created_at: string }
export interface OperationLogsData { items: OperationLog[]; total: number; page: number; page_size: number }
export const listOperationLogs = (input: { operation_type?: string; page: number; page_size: number }) => {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(input)) if (value !== undefined && value !== '') query.set(key, String(value))
  return get<{ success: boolean; data: OperationLogsData; message?: string }>(`/api/v1/operation-logs?${query}`)
}
