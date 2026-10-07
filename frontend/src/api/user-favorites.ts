// API client for per-user starred resources stored in the database.
//
// The backend authoritatively scopes favorites to the active (user, tenant)
// pair from the auth context, so these helpers do not pass user_id or tenant_id.

import { get, post, del } from '@/utils/request'

export type FavoriteResourceType = 'kb' | 'agent'
type FavoriteTargetType = FavoriteResourceType | 'tag'

export interface FavoriteEntry {
  user_id: string
  tenant_id: number
  resource_type: FavoriteTargetType
  resource_id: string
  /** ISO timestamp from the server (created_at column). */
  created_at: string
}

export function listFavorites(type: FavoriteTargetType) {
  return get<{ success: boolean; data: FavoriteEntry[] }>(
    `/api/v1/user/favorites?type=${encodeURIComponent(type)}`
  )
}

export function addFavorite(type: FavoriteTargetType, id: string) {
  return post('/api/v1/user/favorites', { type, id })
}

export function removeFavorite(type: FavoriteTargetType, id: string) {
  return del(`/api/v1/user/favorites/${encodeURIComponent(type)}/${encodeURIComponent(id)}`)
}
