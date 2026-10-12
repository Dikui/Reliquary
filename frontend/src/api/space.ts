import { del, get, post, put } from '@/utils/request'
import i18n from '@/i18n'

const t = (key: string) => i18n.global.t(key)

// 空间信息接口
export interface SpaceInfo {
  space_id: number
  id: number
  name: string
  description?: string
  status?: string
  business?: string
  storage_used?: number
  created_at: string
  updated_at: string
}

export interface APIKey {
  status?: 'active' | 'migration_invalidated' | 'expired' | 'revoked'
  id: number
  name: string
  api_key: string
  last_used_at?: string
  expires_at?: string
  created_at: string
}

export interface CreatedAPIKey extends APIKey {
  token?: string
}

export interface CreateAPIKeyPayload {
  name: string
  expires_at_unix?: number
}

export interface UpdateAPIKeyPayload {
  name: string
  expires_at_unix?: number
}

export async function getSpaceInfo(): Promise<{ success: boolean; data?: SpaceInfo; message?: string }> {
  return get('/api/v1/space')
}

export async function listAPIKeys(
): Promise<{ success: boolean; data?: APIKey[]; message?: string }> {
  try {
    const response = await get(`/api/v1/space/api-keys`)
    return response as unknown as { success: boolean; data?: APIKey[]; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('integrations.api.loadApiKeysFailed'),
    }
  }
}

export async function createAPIKey(
  payload: CreateAPIKeyPayload,
): Promise<{ success: boolean; data?: CreatedAPIKey; message?: string }> {
  try {
    const response = await post(`/api/v1/space/api-keys`, payload)
    return response as unknown as { success: boolean; data?: CreatedAPIKey; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('integrations.api.createApiKeyFailed'),
    }
  }
}

/** 更新 API Key 的名称和有效期。 */
export async function updateAPIKey(
  keyId: number,
  payload: UpdateAPIKeyPayload,
): Promise<{ success: boolean; data?: APIKey; message?: string }> {
  try {
    const response = await put(`/api/v1/space/api-keys/${keyId}`, payload)
    return response as unknown as { success: boolean; data?: APIKey; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('integrations.api.updateApiKeyFailed'),
    }
  }
}

export async function deleteAPIKey(
  keyId: number,
): Promise<{ success: boolean; message?: string }> {
  try {
    const response = await del(`/api/v1/space/api-keys/${keyId}`)
    return response as unknown as { success: boolean; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('integrations.api.deleteApiKeyFailed'),
    }
  }
}

/**
 * 更新空间信息（目前暴露名称、描述两个字段的编辑入口）。
 * 后端 `PUT /space` 用指针字段区分"未传"和"显式空串"，未传的列不会
 * 被改动；这里也按需选择性传 `name` / `description`，互不影响。
 */
export async function updateSpace(
  payload: { name?: string; description?: string },
): Promise<{ success: boolean; data?: SpaceInfo; message?: string }> {
  try {
    const response = await put('/api/v1/space', payload)
    return response as unknown as { success: boolean; data?: SpaceInfo; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.space.updateFailed'),
    }
  }
}
