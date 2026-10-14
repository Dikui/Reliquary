import { get } from '@/utils/request'

export const OPERATION_TYPES = [
  'ingest',
  'write_report',
  'delete_document',
  'ingest_supplement',
  'ingest_conflict',
  'accept_incomplete',
  'ingest_discard',
  'review_approve',
  'review_reject',
  'review_retry',
  'review_discard',
  'review_supplement_url',
  'review_admit_unverified',
  'star',
  'unstar',
  'correct_content_type',
  'correct_topics',
  'merge_entities',
  'create_topic',
  'update_topic',
  'delete_topic',
  'follow',
  'unfollow',
  'add_relation_type',
  'delete_relation',
  'merge_relation_types',
] as const
export type OperationType = typeof OPERATION_TYPES[number]
export interface OperationLog { id: string; actor: string; actor_name?: string; operation_type: OperationType; object_id: string; before: unknown; after: unknown; created_at: string }
export interface OperationLogsData { items: OperationLog[]; total: number; page: number; page_size: number }
export const listOperationLogs = (input: { operation_type?: string; page: number; page_size: number }) => {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(input)) if (value !== undefined && value !== '') query.set(key, String(value))
  return get<{ success: boolean; data: OperationLogsData; message?: string }>(`/api/v1/operation-logs?${query}`)
}
