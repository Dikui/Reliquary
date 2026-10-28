import { get, post, put, del } from '@/utils/request'
import type { EntryTopic } from '@/types/entryClassification'

export interface Topic extends EntryTopic { library_id: string; entry_count: number; created_at: string; updated_at: string }
export interface TopicBackfill {
  id: string
  topic_id: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
  total: number
  processed: number
  failed_entries: number
  failed_batches: number
  created_at: string
  updated_at: string
  batches?: { id: string; ordinal: number; entry_count: number; status: string; attempts: number; error?: string }[]
}
export interface TopicEnvelope<T> { success: boolean; data: T; message?: string }
export interface TopicInput { name: string; description: string }
const path = (id: string) => `/api/v1/topics/${encodeURIComponent(id)}`
export const listTopics = (libraryID: string) => get<TopicEnvelope<Topic[]>>(`/api/v1/topics?library_id=${encodeURIComponent(libraryID)}`)
export const createTopic = (input: TopicInput & { library_id: string }) => post<TopicEnvelope<Topic>>('/api/v1/topics', input)
export const updateTopic = (id: string, input: TopicInput) => put<TopicEnvelope<Topic>>(path(id), input)
export const deleteTopic = (id: string) => del<TopicEnvelope<null>>(path(id))
export const getTopicBackfill = (id: string) => get<TopicEnvelope<TopicBackfill | null>>(`${path(id)}/backfill`)
export const retryTopicBackfill = (id: string) => post<TopicEnvelope<TopicBackfill>>(`${path(id)}/backfill/retry`, {})
