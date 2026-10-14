import type { SearchEntry } from './search'

export type FollowTargetType = 'entity' | 'topic'
export interface FollowTarget {
  target_type: FollowTargetType
  target_id: string
  name: string
  created_at: string
  recent_article_count: number
}
export interface FollowLibraryState { library_id: string; library_name: string; display_name: string; new_count: number; last_viewed_at: string | null }
export interface FollowState { libraries?: FollowLibraryState[]; new_count: number; last_viewed_at: string | null }
export interface FollowFeedRequest {
  libraries?: string[]
  cursor?: string
  since?: string
  target_ids?: string[]
  limit?: number
  collapse_similar?: boolean
  include_seen?: boolean
}
export interface FollowFeedArticle extends SearchEntry {
  entered_at: string | null
  collected_at: string
  is_new: boolean
  matched_follows: FollowTarget[]
  similar_reports: FollowFeedArticle[]
}
export interface FollowFeedData extends FollowState { results: FollowFeedArticle[]; next_cursor?: string; viewed_at: string; total: number }
