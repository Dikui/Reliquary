import { get, post } from '@/utils/request'

export const KNOWLEDGE_ANOMALY_KINDS = ['suspected_error', 'outdated', 'source_conflict', 'manual_conflict', 'tag_deletion', 'source_adapter_failed'] as const
export type KnowledgeAnomalyKind = typeof KNOWLEDGE_ANOMALY_KINDS[number]
export type KnowledgeAnomalyReportKind = Exclude<KnowledgeAnomalyKind, 'source_adapter_failed'>
export type KnowledgeAnomalyProposal = 'none' | 'change_tags' | 'delete_tag'

export interface KnowledgeAnomaly {
  id: string
  knowledge_ids: string[]
  kind: KnowledgeAnomalyKind
  description: string
  site?: string
  source_url?: string
  adapter?: string
  reason?: string
  proposal_type: KnowledgeAnomalyProposal
  proposed_tag_ids: string[]
  proposed_remove_tag_ids: string[]
  proposed_delete_tag_id: string
  applied_changes: Record<string, { added: string[]; removed: string[] }>
  status: 'pending' | 'applying' | 'applied' | 'resolved' | 'dismiss'
  reported_by: string
  decided_by: string
  decision_note: string
  result: string
  created_at: string
  decided_at?: string
}

export type KnowledgeAnomalyInput = Pick<KnowledgeAnomaly, 'knowledge_ids' | 'description'> & {
  kind: KnowledgeAnomalyReportKind
  proposal_type?: KnowledgeAnomalyProposal
  proposed_tag_ids?: string[]
  proposed_remove_tag_ids?: string[]
  proposed_delete_tag_id?: string
}

const base = (kbId: string) => `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/anomalies`

export const listKnowledgeAnomalies = (kbId: string, offset = 0) =>
  get<{ success: boolean; data: KnowledgeAnomaly[]; has_more: boolean }>(`${base(kbId)}?offset=${offset}`)

export const createKnowledgeAnomaly = (kbId: string, input: KnowledgeAnomalyInput) =>
  post<{ success: boolean; data: KnowledgeAnomaly }>(base(kbId), input)

export const decideKnowledgeAnomaly = (kbId: string, id: string, action: 'apply' | 'resolved' | 'dismiss', note = '') =>
  post<{ success: boolean; data: KnowledgeAnomaly }>(`${base(kbId)}/${encodeURIComponent(id)}/decision`, { action, note })
