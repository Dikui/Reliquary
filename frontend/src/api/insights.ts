import { get, post, put, del } from '@/utils/request'

export interface Answer { entry_id: string; grade: number }
export interface RankedEntry { entry_id: string; rank: number; score: number }
export interface EvaluationSet { id: string; name: string; description: string }
export interface Question {
  id: string; method: string; source: string; status: string; revision: number; answer_deleted: boolean
  request: { query: string; libraries: string[]; collapse_similar?: boolean; graph_parameters?: unknown; [key: string]: unknown }
  answers: Answer[]; suggested_results?: RankedEntry[]
}
export interface SearchRecord { id: string; source: string; method: string; libraries: string[]; query: string; created_at: string; results: RankedEntry[]; error: string }
import { metricKeys } from './insightMetrics'
export { metricKeys, metricLabels } from './insightMetrics'
export type Metrics = Record<typeof metricKeys[number], number>
export interface RunOptions { k: number; vector_set: string; collapse_similar: boolean; rerank_model_id?: string }
export interface RunResult { question_id: string; question: Question; snapshot: unknown; metrics: Metrics; hits: string[]; results?: { results: { entry_id?: string; id?: string; title?: string; score?: number }[] }; error: string }
export interface EvaluationRun { id: string; set_id: string; set_snapshot: EvaluationSet; options: RunOptions; status: string; summary: Metrics; snapshot: unknown; created_at: string; error: string; questions?: RunResult[] }
export interface Comparison { before_summary: Metrics; after_summary: Metrics; delta: Metrics; questions: { question_id: string; change: string; question_changed: boolean; before?: RunResult; after?: RunResult; delta: Metrics; added_hits: string[]; lost_hits: string[] }[] }
export interface VectorSet { selector: string; model_id: string; dimension: number }
export interface CallSummary {
  model_id: string; purpose: string; period: string; currency: string; calls: number; failures: number; failure_rate: number; average_duration_ms: number
  input_tokens: number | null; output_tokens: number | null; cost: number | null
  unknown_input_calls: number; unknown_output_calls: number; unknown_cost_calls: number; not_applicable_cost_calls: number
}
export interface ModelCall { processing_attempt?: number; span_id?: string; id: string; requested_at: string; model_id: string; model_name: string; purpose: string; entry_id: string | null; duration_ms: number; success: boolean; error_category: string; input_tokens: number | null; text_input_tokens?: number | null; image_input_tokens?: number | null; output_tokens: number | null; cost: number | null; cost_status: string; currency: string; attempt: number; retry_number: number }
interface Envelope<T> { success: boolean; data: T; total?: number }
export const read = async <T>(path: string, params = {}): Promise<T> => (await get<Envelope<T>>(`/api/v1/${path}`, { params })).data
export const write = async <T>(path: string, body: unknown, update = false): Promise<T> =>
  (await (update ? put : post)<Envelope<T>>(`/api/v1/${path}`, body as object, { timeout: 300000 })).data
export const remove = (path: string) => del(`/api/v1/${path}`)
export const callDetails = (params: Record<string, unknown>) => get<Envelope<ModelCall[]>>('/api/v1/model-calls', { params })

export interface ModelCallContent { id: string; request: string; response: string; request_bytes: number; response_bytes: number; response_type: string; http_status: number; complete: boolean }
