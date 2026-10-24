import { put } from '@/utils/request'
import type { ClassifiedEntry } from '@/types/entryClassification'

export interface EntryJudgment extends ClassifiedEntry { id: string; parse_status?: string; error_message?: string }
export interface JudgmentEnvelope { success: boolean; data: EntryJudgment; message?: string }
const path = (id: string) => `/api/v1/entries/${encodeURIComponent(id)}`
export const setEntryStar = (id: string, starred: boolean) => put<JudgmentEnvelope>(`${path(id)}/star`, { starred })
