import { get, post, put, del } from '@/utils/request'
import type { FollowFeedData, FollowFeedRequest, FollowTarget, FollowTargetType } from '@/types/follows'

interface FollowEnvelope<T> { success: boolean; data: T; message?: string }
const path = (type: FollowTargetType, id: string) => `/api/v1/follows/${type}/${encodeURIComponent(id)}`
export const listFollows = () => get<FollowEnvelope<FollowTarget[]>>('/api/v1/follows')
export const followTarget = (type: FollowTargetType, id: string) => put<FollowEnvelope<FollowTarget>>(path(type, id), {})
export const unfollowTarget = (type: FollowTargetType, id: string) => del<FollowEnvelope<null>>(path(type, id))
export const getFollowFeed = (request: FollowFeedRequest) => post<FollowEnvelope<FollowFeedData>>('/api/v1/follows/feed', request)
