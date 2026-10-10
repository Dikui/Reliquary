import { get, post, del } from '@/utils/request'

export type FavoriteResourceType = 'kb'
type FavoriteTargetType = FavoriteResourceType | 'tag'

export interface FavoriteEntry {
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
