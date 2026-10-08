import { get, getDown, post, put } from '@/utils/request'
import type { KnowledgeContextEnvelope } from './knowledge-context'

export type KnowledgeWorkflowStepType =
  | 'search'
  | 'fact_query'
  | 'report'

export interface KnowledgeWorkflowSchedule {
  cron?: string
  timezone?: string
}

export interface KnowledgeWorkflowStep {
  id: string
  type: KnowledgeWorkflowStepType
  depends_on?: string[]
  config: Record<string, unknown>
}

export interface KnowledgeWorkflowInput {
  name: string
  description?: string
  enabled: boolean
  schedule: KnowledgeWorkflowSchedule
  steps: KnowledgeWorkflowStep[]
}

export interface KnowledgeWorkflow extends KnowledgeWorkflowInput {
  id: string
  tenant_id: number
  knowledge_base_id: string
  owner_id: string
  created_at: string
  updated_at: string
}

export type KnowledgeWorkflowRunStatus =
  | 'queued'
  | 'running'
  | 'paused'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | string

export type KnowledgeWorkflowStepRunStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'
  | string

export interface KnowledgeWorkflowStepRun {
  id: string
  tenant_id: number
  knowledge_base_id: string
  run_id: string
  step_id: string
  step_type: KnowledgeWorkflowStepType
  status: KnowledgeWorkflowStepRunStatus
  attempt: number
  input?: unknown
  output?: unknown
  error?: string
  started_at?: string
  finished_at?: string
}

export interface KnowledgeWorkflowRun {
  id: string
  tenant_id: number
  knowledge_base_id: string
  workflow_id: string
  owner_id: string
  trigger: string
  idempotency_key: string
  status: KnowledgeWorkflowRunStatus
  current_step_id?: string
  error?: string
  started_at?: string
  finished_at?: string
  created_at: string
  updated_at: string
  workflow_snapshot?: KnowledgeWorkflowStep[]
  steps?: KnowledgeWorkflowStepRun[]
}

export interface KnowledgeWorkflowCitation {
  id: string
  kind: string
  knowledge_id?: string
  evidence_id?: string
  title?: string
  url?: string
  published_at?: string
  quote?: string
  role?: 'current_period' | 'background' | 'unknown_date' | string
}

export interface KnowledgeWorkflowReport {
  excluded_missing_published_at_count?: number
  id: string
  tenant_id: number
  knowledge_base_id: string
  workflow_id: string
  run_id: string
  step_id: string
  title: string
  markdown: string
  citations: KnowledgeWorkflowCitation[]
  time_from?: string
  time_to?: string
  created_at: string
}

const workflowBase = (kbId: string) =>
  `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/workflows`
const reportBase = (kbId: string) =>
  `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/reports`

export const listKnowledgeWorkflows = (kbId: string) =>
  get<KnowledgeContextEnvelope<KnowledgeWorkflow[]>>(workflowBase(kbId))

export const createKnowledgeWorkflow = (kbId: string, input: KnowledgeWorkflowInput) =>
  post<KnowledgeContextEnvelope<KnowledgeWorkflow>>(workflowBase(kbId), input)

export const updateKnowledgeWorkflow = (kbId: string, workflowId: string, input: KnowledgeWorkflowInput) =>
  put<KnowledgeContextEnvelope<KnowledgeWorkflow>>(
    `${workflowBase(kbId)}/${encodeURIComponent(workflowId)}`,
    input,
  )

export const listKnowledgeWorkflowRuns = (kbId: string, workflowId: string) =>
  get<KnowledgeContextEnvelope<KnowledgeWorkflowRun[]>>(
    `${workflowBase(kbId)}/${encodeURIComponent(workflowId)}/runs`,
  )

export const runKnowledgeWorkflow = (kbId: string, workflowId: string) =>
  post<KnowledgeContextEnvelope<KnowledgeWorkflowRun>>(
    `${workflowBase(kbId)}/${encodeURIComponent(workflowId)}/run`,
    {},
  )

export const getKnowledgeWorkflowRun = (kbId: string, runId: string) =>
  get<KnowledgeContextEnvelope<KnowledgeWorkflowRun>>(
    `${workflowBase(kbId)}/runs/${encodeURIComponent(runId)}`,
  )

export const resumeKnowledgeWorkflowRun = (kbId: string, runId: string) =>
  post<KnowledgeContextEnvelope<KnowledgeWorkflowRun>>(
    `${workflowBase(kbId)}/runs/${encodeURIComponent(runId)}/resume`,
    {},
  )

export const retryKnowledgeWorkflowRun = (kbId: string, runId: string) =>
  post<KnowledgeContextEnvelope<KnowledgeWorkflowRun>>(
    `${workflowBase(kbId)}/runs/${encodeURIComponent(runId)}/retry`,
    {},
  )

export const listKnowledgeWorkflowReports = (kbId: string) =>
  get<KnowledgeContextEnvelope<KnowledgeWorkflowReport[]>>(reportBase(kbId))

export const getKnowledgeWorkflowReport = (kbId: string, reportId: string) =>
  get<KnowledgeContextEnvelope<KnowledgeWorkflowReport>>(
    `${reportBase(kbId)}/${encodeURIComponent(reportId)}`,
  )

export const downloadKnowledgeWorkflowReport = (kbId: string, reportId: string, format: 'markdown' | 'json' = 'markdown') =>
  getDown(`${reportBase(kbId)}/${encodeURIComponent(reportId)}/download${format === 'json' ? '?format=json' : ''}`)
