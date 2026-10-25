import { get, post } from '@/utils/request'

export interface Story {
  id: string
  title: string
  first_fact_at: string
  manual_corrected: boolean
  merged_into_id: string
  last_fact_at: string
}

export interface FactSource {
  entry_id: string
  library_id: string
  title: string
  original_url: string
  role: 'source' | 'mention'
  quote: string
  manual: boolean
}

export interface StoryFact {
  id: string
  manual_corrected: boolean
  merged_into_id: string
  story_id: string
  title: string
  subject: string
  action: string
  object: string
  conditions: string
  subject_entity_id: string | null
  object_entity_id: string | null
  // Calendar date (YYYY-MM-DD) or RFC3339 instant; null means unknown.
  occurred_at: string | null
  created_at: string
  sources: FactSource[]
}

export interface StoryDetail { story: Story; facts: StoryFact[]; mentions: FactSource[] }
export interface StoryPage { items: Story[]; total: number }
interface StoryEnvelope<T> { success: boolean; data: T }

export const listStories = (limit = 20, offset = 0) =>
  get<StoryEnvelope<StoryPage>>(`/api/v1/stories?limit=${limit}&offset=${offset}`)
export const getStory = (id: string) =>
  get<StoryEnvelope<StoryDetail>>(`/api/v1/stories/${encodeURIComponent(id)}`)

export type CorrectionAction = 'move' | 'split' | 'mergeFacts' | 'mergeStories'
export const getFact = (id: string) =>
  get<StoryEnvelope<StoryFact>>(`/api/v1/facts/${encodeURIComponent(id)}`)
export const moveFact = (id: string, storyID: string) =>
  post<StoryEnvelope<StoryFact>>(`/api/v1/facts/${encodeURIComponent(id)}/move`, { story_id: storyID })
export const splitFact = (id: string, title: string) =>
  post<StoryEnvelope<StoryFact>>(`/api/v1/facts/${encodeURIComponent(id)}/split`, { title })
export const mergeFacts = (id: string, keepID: string) =>
  post<StoryEnvelope<StoryFact>>(`/api/v1/facts/${encodeURIComponent(id)}/merge`, { keep_fact_id: keepID })
export const mergeStories = (id: string, keepID: string) =>
  post<StoryEnvelope<StoryDetail>>(`/api/v1/stories/${encodeURIComponent(id)}/merge`, { keep_story_id: keepID })
