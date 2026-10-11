import { put } from '@/utils/request'
import type { ClassifiedEntry, ContentType } from '@/types/entryClassification'

export interface ArticleJudgment extends ClassifiedEntry { id: string; parse_status?: string; error_message?: string }
export interface JudgmentEnvelope { success: boolean; data: ArticleJudgment; message?: string }
const path = (id: string) => `/api/v1/entries/${encodeURIComponent(id)}`
export const setEntryStar = (id: string, starred: boolean) => put<JudgmentEnvelope>(`${path(id)}/star`, { starred })
export const correctEntryContentType = (id: string, content_type: ContentType, confirm_remove_body = false) => put<JudgmentEnvelope>(`${path(id)}/content-type`, { content_type, confirm_remove_body }, { timeout: 300000 })
export const correctEntryTopics = (id: string, topic_ids: string[]) => put<JudgmentEnvelope>(`${path(id)}/topics`, { topic_ids })
