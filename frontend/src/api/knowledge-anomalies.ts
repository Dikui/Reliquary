import { get, post } from '@/utils/request'

export type KnowledgeAnomalyKind = 'suspected_error' | 'outdated' | 'source_conflict' | 'manual_conflict' | 'tag_deletion'
export type KnowledgeAnomalyProposal = 'none' | 'set_tags' | 'delete_tag'

export interface KnowledgeAnomaly {
  id: string
  knowledge_ids: string[]
  kind: KnowledgeAnomalyKind
  description: string
  proposal_type: KnowledgeAnomalyProposal
  proposed_tag_ids: string[]
  proposed_delete_tag_id: string
  status: 'pending' | 'applying' | 'applied' | 'resolved' | 'dismiss'
  reported_by: string
  decided_by: string
  decision_note: string
  result: string
  created_at: string
  decided_at?: string
}

export type KnowledgeAnomalyInput = Pick<KnowledgeAnomaly, 'knowledge_ids' | 'kind' | 'description'> & {
  proposal_type?: KnowledgeAnomalyProposal
  proposed_tag_ids?: string[]
  proposed_delete_tag_id?: string
}

const base = (kbId: string) => `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/anomalies`

export const listKnowledgeAnomalies = (kbId: string) =>
  get<{ success: boolean; data: KnowledgeAnomaly[] }>(base(kbId))

export const createKnowledgeAnomaly = (kbId: string, input: KnowledgeAnomalyInput) =>
  post<{ success: boolean; data: KnowledgeAnomaly }>(base(kbId), input)

export const decideKnowledgeAnomaly = (kbId: string, id: string, action: 'apply' | 'resolved' | 'dismiss', note = '') =>
  post<{ success: boolean; data: KnowledgeAnomaly }>(`${base(kbId)}/${encodeURIComponent(id)}/decision`, { action, note })
