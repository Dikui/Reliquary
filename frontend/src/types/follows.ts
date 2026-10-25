import type { Story, StoryFact } from '@/api/stories'

export type FollowTargetType = 'entity' | 'story'
export interface FollowTarget {
  target_type: FollowTargetType
  target_id: string
  name: string
  created_at: string
}
export interface FollowLibrary { library_id: string; library_name: string; display_name: string }
export interface FollowFeedRequest {
  libraries?: string[]
  cursor?: string
  since?: string
  target_ids?: string[]
  limit?: number
}
export interface FollowFeedCard {
  story: Story
  trigger_fact: StoryFact
  recent_facts: StoryFact[]
  matched_follows: FollowTarget[]
}
export interface FollowFeedData { libraries: FollowLibrary[]; results: FollowFeedCard[]; next_cursor?: string; total: number }
