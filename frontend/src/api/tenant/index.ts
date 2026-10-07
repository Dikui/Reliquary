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
  storage_quota?: number
  storage_used?: number
  created_at: string
  updated_at: string
}

export type APIPrincipalMode = 'tenant' | 'direct_header' | 'signed_token'

export interface APIPrincipalConfig {
  mode: APIPrincipalMode
  direct_header_name: string
  signed_token_header_name: string
  require_direct_header: boolean
  // The server never returns the plaintext secret; only its presence.
  has_hmac_secret: boolean
}

export interface UpdateAPIPrincipalConfigPayload {
  mode: APIPrincipalMode
  direct_header_name?: string
  signed_token_header_name?: string
  require_direct_header?: boolean
  hmac_secret?: string
}

export interface CreateAPIPrincipalTestTokenPayload {
  external_user_id: string
  expires_in_seconds?: number
}

export interface APIPrincipalTestToken {
  token: string
  header_name: string
  expires_in_seconds: number
  expires_at_unix: number
  external_user_id: string
}

// Bounded per-key grants for non-full-access API keys.
//  - 'retrieve': read/search knowledge-base data within scope
//  - 'chat': run the conversation flow (sessions + agent listing + self identity)
//  - 'read_agents': list/read agents without chat or authoring
//  - 'manage_kbs': manage the KB lifecycle (create/copy/duplicate/update/delete + config)
//  - 'manage_agents': create/update/delete/copy agents
//  - 'message_history': search/read tenant chat-history metadata
//  - 'manage_models': manage tenant model definitions, checks, and credentials
//  - 'manage_mcp_services': manage MCP services, credentials, tool policies, and OAuth state
//  - 'manage_datasources': manage data-source connectors and sync jobs
//  - 'manage_channels': manage embed and IM channels
//  - 'manage_vector_stores': manage vector stores and parser/storage checks
//  - 'manage_web_search': manage web-search providers
//  - 'run_evaluations': run/read evaluation jobs
//  - 'manage_members': manage tenant members and invitations
//  - 'manage_tenant_settings': read/update tenant integration settings (API principal mode, headers, tenant KV)
export type TenantAPIKeyCapability =
  | 'retrieve'
  | 'chat'
  | 'read_agents'
  | 'ingest'
  | 'manage_kbs'
  | 'manage_agents'
  | 'message_history'
  | 'manage_models'
  | 'manage_mcp_services'
  | 'manage_datasources'
  | 'manage_channels'
  | 'manage_vector_stores'
  | 'manage_storage_backends'
  | 'manage_web_search'
  | 'run_evaluations'
  | 'manage_members'
  | 'manage_tenant_settings'







export interface TenantAPIKey {
  id: number
  scope_type?: 'tenant'
  name: string
  api_key: string
  full_access: boolean
  knowledge_base_ids: string[] | null
  capabilities?: TenantAPIKeyCapability[]
  last_used_at?: string
  expires_at?: string
  created_at: string
}

export interface CreatedTenantAPIKey extends TenantAPIKey {
  token?: string
}

export interface CreateTenantAPIKeyPayload {
  name: string
  full_access?: boolean
  knowledge_base_ids?: string[]
  capabilities?: TenantAPIKeyCapability[]
  expires_at_unix?: number
}

export interface UpdateTenantAPIKeyPayload {
  name: string
  full_access: boolean
  knowledge_base_ids: string[]
  capabilities: TenantAPIKeyCapability[]
  expires_at_unix?: number
}

export async function getAPIPrincipalConfig(
  tenantId: number,
): Promise<{ success: boolean; data?: APIPrincipalConfig; message?: string }> {
  try {
    const response = await get(`/api/v1/tenants/${tenantId}/api-principal-config`)
    return response as unknown as { success: boolean; data?: APIPrincipalConfig; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.tenant.getApiPrincipalConfigFailed'),
    }
  }
}

export async function updateAPIPrincipalConfig(
  tenantId: number,
  payload: UpdateAPIPrincipalConfigPayload,
): Promise<{ success: boolean; data?: APIPrincipalConfig; message?: string }> {
  try {
    const response = await put(`/api/v1/tenants/${tenantId}/api-principal-config`, payload)
    return response as unknown as { success: boolean; data?: APIPrincipalConfig; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.tenant.updateApiPrincipalConfigFailed'),
    }
  }
}

export async function createAPIPrincipalTestToken(
  tenantId: number,
  payload: CreateAPIPrincipalTestTokenPayload,
): Promise<{ success: boolean; data?: APIPrincipalTestToken; message?: string }> {
  try {
    const response = await post(`/api/v1/tenants/${tenantId}/api-principal-test-token`, payload)
    return response as unknown as { success: boolean; data?: APIPrincipalTestToken; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.tenant.createApiPrincipalTestTokenFailed'),
    }
  }
}

export async function listTenantAPIKeys(
  tenantId: number,
): Promise<{ success: boolean; data?: TenantAPIKey[]; message?: string }> {
  try {
    const response = await get(`/api/v1/tenants/${tenantId}/api-keys`)
    return response as unknown as { success: boolean; data?: TenantAPIKey[]; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.tenant.listApiKeysFailed'),
    }
  }
}

export async function createTenantAPIKey(
  tenantId: number,
  payload: CreateTenantAPIKeyPayload,
): Promise<{ success: boolean; data?: CreatedTenantAPIKey; message?: string }> {
  try {
    const response = await post(`/api/v1/tenants/${tenantId}/api-keys`, payload)
    return response as unknown as { success: boolean; data?: CreatedTenantAPIKey; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.tenant.createApiKeyFailed'),
    }
  }
}

/** 更新已创建租户 API Key 的授权范围和其他可配置属性。 */
export async function updateTenantAPIKey(
  tenantId: number,
  keyId: number,
  payload: UpdateTenantAPIKeyPayload,
): Promise<{ success: boolean; data?: TenantAPIKey; message?: string }> {
  try {
    const response = await put(`/api/v1/tenants/${tenantId}/api-keys/${keyId}`, payload)
    return response as unknown as { success: boolean; data?: TenantAPIKey; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('integrations.api.updateApiKeyScopeFailed'),
    }
  }
}

export async function deleteTenantAPIKey(
  tenantId: number,
  keyId: number,
): Promise<{ success: boolean; message?: string }> {
  try {
    const response = await del(`/api/v1/tenants/${tenantId}/api-keys/${keyId}`)
    return response as unknown as { success: boolean; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.tenant.deleteApiKeyFailed'),
    }
  }
}

/**
 * 更新空间信息（目前暴露名称、描述两个字段的编辑入口）。
 * 后端 `PUT /tenants/:id` 用指针字段区分"未传"和"显式空串"，未传的列不会
 * 被改动；这里也按需选择性传 `name` / `description`，互不影响。
 * 权限：owner（与 router.go 中的 g.Owner() 守卫保持一致）。
 */
export async function updateTenant(
  tenantId: number,
  payload: { name?: string; description?: string },
): Promise<{ success: boolean; data?: TenantInfo; message?: string }> {
  try {
    const response = await put(`/api/v1/tenants/${tenantId}`, payload)
    return response as unknown as { success: boolean; data?: TenantInfo; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.tenant.updateFailed'),
    }
  }
}
