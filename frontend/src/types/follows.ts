import type { KnowledgeSearchArticle } from './knowledgeSearch'

export type FollowTargetType = 'entity' | 'topic'
export interface FollowTarget {
  target_type: FollowTargetType
  target_id: string
  name: string
  created_at: string
  recent_article_count: number
}
export interface FollowState { new_count: number; last_viewed_at: string | null }
export interface FollowFeedRequest {
  cursor?: string
  since?: string
  target_ids?: string[]
  limit?: number
  collapse_similar?: boolean
  include_seen?: boolean
}
export interface FollowFeedArticle extends KnowledgeSearchArticle {
  entered_at: string | null
  collected_at: string
  is_new: boolean
  matched_follows: FollowTarget[]
  similar_reports: FollowFeedArticle[]
}
export interface FollowFeedData extends FollowState { results: FollowFeedArticle[]; next_cursor?: string; viewed_at: string; total: number }
