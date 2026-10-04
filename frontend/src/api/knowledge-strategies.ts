import { get, post } from '@/utils/request'

export interface KnowledgeStrategyRules {
  filter: string
  tags: string
  collection: string
  research: string
}

export interface KnowledgeStrategySample {
  knowledge_id: string
  old_filter: string
  new_filter: string
  old_tags: string
  new_tags: string
  old_collection: string
  new_collection: string
  old_research: string
  new_research: string
}

export interface KnowledgeStrategyVersion {
  id: string
  status: 'candidate' | 'active' | 'archived'
  rules: KnowledgeStrategyRules
  previous_rules: KnowledgeStrategyRules
  reason: string
  sample_results: KnowledgeStrategySample[]
  created_at: string
  created_by: string
  activated_at?: string
}

const base = (kbId: string) => `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/strategies`

export const listKnowledgeStrategies = (kbId: string) =>
  get<{ success: boolean; data: KnowledgeStrategyVersion[] }>(base(kbId))

export const proposeKnowledgeStrategy = (kbId: string, instruction: string, sampleIds: string[]) =>
  post<{ success: boolean; data: KnowledgeStrategyVersion }>(base(kbId), { instruction, sample_ids: sampleIds })

export const activateKnowledgeStrategy = (kbId: string, id: string) =>
  post<{ success: boolean; data: KnowledgeStrategyVersion }>(`${base(kbId)}/${encodeURIComponent(id)}/activate`, {})

export type KnowledgeStrategyEvaluationLabel = 'select' | 'reject' | 'either'
export type KnowledgeStrategyEvaluationSplit = 'development' | 'holdout'

export interface KnowledgeStrategyEvaluationSample {
  knowledge_id: string
  label: KnowledgeStrategyEvaluationLabel
  split: KnowledgeStrategyEvaluationSplit
}

export interface KnowledgeStrategyConfusion {
  tp: number
  fp: number
  fn: number
  tn: number
  precision: number | null
  recall: number | null
}

export interface KnowledgeStrategyEvaluationCase {
  knowledge_id: string
  label: KnowledgeStrategyEvaluationLabel
  split: KnowledgeStrategyEvaluationSplit
  old_decision: 'approved' | 'rejected'
  new_decision: 'approved' | 'rejected'
  old_reason: string
  new_reason: string
  old_correct: boolean | null
  new_correct: boolean | null
}

export interface KnowledgeStrategyEvaluationResult {
  version_id: string
  development: { old: KnowledgeStrategyConfusion; new: KnowledgeStrategyConfusion; samples: number }
  holdout: { old: KnowledgeStrategyConfusion; new: KnowledgeStrategyConfusion; samples: number }
  cases: KnowledgeStrategyEvaluationCase[]
}

export const evaluateKnowledgeStrategy = (kbId: string, versionId: string, samples: KnowledgeStrategyEvaluationSample[]) =>
  post<{ success: boolean; data: KnowledgeStrategyEvaluationResult }>(
    `${base(kbId)}/${encodeURIComponent(versionId)}/evaluate`, { samples })

export interface KnowledgeReprocessResult {
  processed: number
  updated: number
  flagged: number
  conflicts: number
  failed: number
  errors: string[]
}

export const reprocessKnowledge = (kbId: string, scope: { message_ids?: string[]; tag_id?: string }) =>
  post<{ success: boolean; data: KnowledgeReprocessResult }>(`${base(kbId)}/reprocess`, scope)
