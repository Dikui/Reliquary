import { del, get, post, put } from '@/utils/request'
import i18n from '@/i18n'

const t = (key: string) => i18n.global.t(key)

// 空间信息接口
export interface TenantInfo {
  id: number
  name: string
  description?: string
  status?: string
  business?: string
  storage_used?: number
  created_at: string
  updated_at: string
}

export interface TenantAPIKey {
  status?: 'active' | 'migration_invalidated' | 'expired' | 'revoked'
  id: number
  name: string
  api_key: string
  last_used_at?: string
  expires_at?: string
  created_at: string
}

export interface CreatedTenantAPIKey extends TenantAPIKey {
  token?: string
}

export interface CreateTenantAPIKeyPayload {
  name: string
  expires_at_unix?: number
}

export interface UpdateTenantAPIKeyPayload {
  name: string
  expires_at_unix?: number
}

export async function getTenantInfo(): Promise<{ success: boolean; data?: TenantInfo; message?: string }> {
  return get('/api/v1/space')
}

export async function listTenantAPIKeys(
): Promise<{ success: boolean; data?: TenantAPIKey[]; message?: string }> {
  try {
    const response = await get(`/api/v1/space/api-keys`)
    return response as unknown as { success: boolean; data?: TenantAPIKey[]; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('integrations.api.loadApiKeysFailed'),
    }
  }
}

export async function createTenantAPIKey(
  payload: CreateTenantAPIKeyPayload,
): Promise<{ success: boolean; data?: CreatedTenantAPIKey; message?: string }> {
  try {
    const response = await post(`/api/v1/space/api-keys`, payload)
    return response as unknown as { success: boolean; data?: CreatedTenantAPIKey; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('integrations.api.createApiKeyFailed'),
    }
  }
}

/** 更新 API Key 的名称和有效期。 */
export async function updateTenantAPIKey(
  keyId: number,
  payload: UpdateTenantAPIKeyPayload,
): Promise<{ success: boolean; data?: TenantAPIKey; message?: string }> {
  try {
    const response = await put(`/api/v1/space/api-keys/${keyId}`, payload)
    return response as unknown as { success: boolean; data?: TenantAPIKey; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('integrations.api.updateApiKeyFailed'),
    }
  }
}

export async function deleteTenantAPIKey(
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
export async function updateTenant(
  payload: { name?: string; description?: string },
): Promise<{ success: boolean; data?: TenantInfo; message?: string }> {
  try {
    const response = await put('/api/v1/space', payload)
    return response as unknown as { success: boolean; data?: TenantInfo; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.tenant.updateFailed'),
    }
  }
}
