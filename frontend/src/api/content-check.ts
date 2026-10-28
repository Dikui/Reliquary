import { get, put, post } from '@/utils/request'
import type { SourceLocation } from '@/types/search'
export interface CheckSettings { enabled: boolean; model_id: string; weekday: number; time: string; next_run_at?: string | null }
export interface CheckEvidence { entry_id: string; title: string; library_id: string; quote: string; current_result?: string; location: SourceLocation }
export interface CheckFinding { kind: 'duplicate' | 'conflict' | 'result_error'; description: string; evidence: CheckEvidence[] }
export interface CheckRun { id: string; model_id: string; previous_id: string; status: string; total: number; targets?: string[]; completed: number; results?: { entry_id: string; findings: CheckFinding[] }[]; error: string; created_at: string; finished_at?: string }
const path = '/api/v1/diagnostics/content-checks'
interface Reply<T> { success: boolean; data: T; message?: string }
async function data<T>(request: Promise<Reply<T>>) { const result = await request; if (!result.success) throw new Error(result.message || 'Request failed'); return result.data }
export const checkSettings = () => data(get<Reply<CheckSettings>>(`${path}/settings`))
export const saveCheckSettings = (settings: CheckSettings) => data(put<Reply<CheckSettings>>(`${path}/settings`, settings))
export const checkRuns = (page: number) => data(get<Reply<{items: CheckRun[]; total: number}>>(path, { params: { page } }))
export const checkRun = (id: string) => data(get<Reply<CheckRun>>(`${path}/${encodeURIComponent(id)}`))
export const recheck = (id: string) => data(post<Reply<CheckRun>>(`${path}/${encodeURIComponent(id)}/recheck`))
