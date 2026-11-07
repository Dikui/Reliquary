import { get } from '@/utils/request'

export const OPERATION_TYPES = [
  'configure_shelf', 'create_entity', 'update_entity', 'delete_entity', 'update_pending_entity_attributes', 'create_relation', 'update_relation', 'set_entity_link',
  'switch_entity_library', 'merge_managed_entities', 'split_managed_entity',
  'ingest',
  'write_report',
  'delete_document',
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
  'graph_check',
  'graph_rebuild',
  'follow',
  'unfollow',
  'add_relation_type',
  'delete_relation',
  'merge_relation_types',
] as const
export type OperationType = typeof OPERATION_TYPES[number]
export interface OperationSource { type?: 'web' | 'agent' | 'import'; agent_name?: string; import_batch_id?: string; file_date?: string }
export interface OperationLog { source?: OperationSource; changeset_id?: string; id: string; actor: string; actor_name?: string; operation_type: OperationType; object_id: string; before: unknown; after: unknown; created_at: string }
export interface OperationLogsData { items: OperationLog[]; total: number; page: number; page_size: number }
export const listOperationLogs = (input: { operation_type?: string; source_type?: string; agent_name?: string; import_batch_id?: string; page: number; page_size: number }) => {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(input)) if (value !== undefined && value !== '') query.set(key, String(value))
  return get<{ success: boolean; data: OperationLogsData; message?: string }>(`/api/v1/operation-logs?${query}`)
}
