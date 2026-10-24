import { get, post } from '@/utils/request'

export type EmbeddingRebuildStatus = 'pending' | 'running' | 'paused' | 'completed' | 'completed_with_errors' | 'failed' | 'cancelled'
export type EmbeddingRebuildKind = 'article_chunk' | 'article' | 'image' | 'mine_chunk' | 'report_chunk'
export type EmbeddingRebuildAction = 'start' | 'pause' | 'resume' | 'retry_failed' | 'switch' | 'delete_old' | 'cancel'
export interface EmbeddingRebuildRun {
  id: string
  candidate?: boolean
  switched_at?: string | null
  status: EmbeddingRebuildStatus
  model_id: string
  model_name: string
  dimension: number
  total_count: number
  completed_count: number
  failed_count: number
  error?: string
  started_at?: string | null
  finished_at?: string | null
}
export interface EmbeddingRebuildFailure { id: string; kind: EmbeddingRebuildKind; source_id: string; error: string; attempts: number }
export interface EmbeddingRebuildData { latest_run: EmbeddingRebuildRun | null; failed_items: EmbeddingRebuildFailure[]; current_model_matches?: boolean }
interface Envelope { success: boolean; data: EmbeddingRebuildData; message?: string }

export const getEmbeddingRebuild = () => get<Envelope>('/api/v1/embeddings/rebuild')
export const controlEmbeddingRebuild = (action: EmbeddingRebuildAction, runId?: string, modelId?: string) => post<Envelope>('/api/v1/embeddings/rebuild', {
  action, ...(runId ? { run_id: runId } : {}), ...(modelId ? { model_id: modelId } : {}),
})
