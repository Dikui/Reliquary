import { get } from '@/utils/request'
import i18n from '@/i18n'

const t = (key: string) => i18n.global.t(key)

// 用户信息接口
export interface UserInfo {
  id: string
  username: string
  email: string
  avatar?: string
  space_id: string
  created_at: string
  updated_at: string
}

/**
 * 将后端用户信息规范化为前端 UserInfo，统一登录与令牌恢复时的字段。
 * 新增用户字段只需修改此处，避免遗漏系统管理员等权限属性。
 * fallbackSpaceId 用于响应中用户对象缺少 space_id 时补充空间 ID。
 * 权限字段严格按 boolean true 读取。
 */
export function userInfoFromApi(
  u: any,
  fallbackSpaceId?: string | number | null,
): UserInfo {
  const rawSpaceId =
    u?.space_id !== undefined && u?.space_id !== null && u.space_id !== ''
      ? u.space_id
      : fallbackSpaceId ?? ''
  const tid = Number(rawSpaceId) > 0 ? rawSpaceId : ''
  return {
    id: u?.id || '',
    username: u?.username || '',
    email: u?.email || '',
    avatar: u?.avatar,
    space_id: String(tid) || '',
    created_at: u?.created_at || new Date().toISOString(),
    updated_at: u?.updated_at || new Date().toISOString(),
  }
}

// 空间信息接口
export interface SpaceInfo {
  id: string
  name: string
  description?: string
  status?: string
  business?: string
  owner_id: string
  storage_used?: number
  created_at: string
  updated_at: string
}

// 模型信息接口
export interface ModelInfo {
  id: string
  name: string
  type: string
  source: string
  description?: string
  is_default?: boolean
  created_at: string
  updated_at: string
}

export async function getCurrentUser(): Promise<{ success: boolean; data?: { user: UserInfo; space?: SpaceInfo | null; space_required?: boolean }; message?: string }> {
  try {
    const response = await get('/api/v1/auth/me')
    return response as unknown as { success: boolean; data?: { user: UserInfo; space?: SpaceInfo | null; space_required?: boolean }; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.auth.getUserFailed')
    }
  }
}
