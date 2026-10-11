import { put } from '@/utils/request'
import type { ClassifiedKnowledge, ContentType } from '@/types/knowledgeClassification'

export interface ArticleJudgment extends ClassifiedKnowledge { id: string; parse_status?: string; error_message?: string }
export interface JudgmentEnvelope { success: boolean; data: ArticleJudgment; message?: string }
const path = (id: string) => `/api/v1/knowledge/${encodeURIComponent(id)}`
export const setKnowledgeStar = (id: string, starred: boolean) => put<JudgmentEnvelope>(`${path(id)}/star`, { starred })
export const correctKnowledgeContentType = (id: string, content_type: ContentType, confirm_remove_body = false) => put<JudgmentEnvelope>(`${path(id)}/content-type`, { content_type, confirm_remove_body }, { timeout: 300000 })
export const correctKnowledgeTopics = (id: string, topic_ids: string[]) => put<JudgmentEnvelope>(`${path(id)}/topics`, { topic_ids })
