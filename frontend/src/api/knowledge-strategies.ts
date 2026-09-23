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
