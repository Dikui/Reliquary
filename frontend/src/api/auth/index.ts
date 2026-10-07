import { post, get, put } from '@/utils/request'
import i18n from '@/i18n'

const t = (key: string) => i18n.global.t(key)

// 用户登录接口
export interface LoginRequest {
  email: string
  password: string
}

export interface LoginResponse {
  success: boolean
  message?: string
  user?: {
    id: string
    username: string
    email: string
    avatar?: string
    tenant_id: number
    is_system_admin?: boolean
    is_active: boolean
    created_at: string
    updated_at: string
  }
  tenant?: {
    id: number
    name: string
    description: string
    status: string
    business: string
    storage_quota: number
    storage_used: number
    created_at: string
    updated_at: string
  } | null
  // Current team returned by password login or invitation registration.
  active_tenant?: {
    id: number
    name: string
    description?: string
    status?: string
    business?: string
    storage_quota?: number
    storage_used?: number
    created_at?: string
    updated_at?: string
  } | null
  memberships?: MembershipInfo[]
  token?: string
  refresh_token?: string
}

// 用户注册接口
export interface RegisterRequest {
  username: string
  email: string
  password: string
}

export interface RegisterResponse {
  success: boolean
  message?: string
  data?: {
    user: {
      id: string
      username: string
      email: string
    }
    tenant: {
      id: string
      name: string
    }
  }
}

// 用户信息接口
export interface UserInfo {
  id: string
  username: string
  email: string
  avatar?: string
  tenant_id: string
  is_system_admin?: boolean
  created_at: string
  updated_at: string
}

/**
 * 将后端用户信息规范化为前端 UserInfo，统一登录与令牌恢复时的字段。
 * 新增用户字段只需修改此处，避免遗漏系统管理员等权限属性。
 * fallbackTenantId 用于响应中用户对象缺少 tenant_id 时补充团队 ID。
 * 权限字段严格按 boolean true 读取。
 */
export function userInfoFromApi(
  u: any,
  fallbackTenantId?: string | number | null,
): UserInfo {
  const rawTenantId =
    u?.tenant_id !== undefined && u?.tenant_id !== null && u.tenant_id !== ''
      ? u.tenant_id
      : fallbackTenantId ?? ''
  const tid = Number(rawTenantId) > 0 ? rawTenantId : ''
  return {
    id: u?.id || '',
    username: u?.username || '',
    email: u?.email || '',
    avatar: u?.avatar,
    tenant_id: String(tid) || '',
    is_system_admin: u?.is_system_admin === true,
    created_at: u?.created_at || new Date().toISOString(),
    updated_at: u?.updated_at || new Date().toISOString(),
  }
}

// 空间信息接口
export interface TenantInfo {
  id: string
  name: string
  description?: string
  status?: string
  business?: string
  owner_id: string
  storage_quota?: number
  storage_used?: number
  created_at: string
  updated_at: string
  knowledge_bases?: KnowledgeBaseInfo[]
}

// 知识库信息接口
export interface KnowledgeBaseInfo {
  id: string
  name: string
  description: string
  tenant_id: string
  // creator_id is the user id of whoever originally created the KB.
  // KBs created before that migration backfilled the column.
  creator_id?: string
  // creator_name 由后端 list 接口批量回填（username 优先，退化到 email），
  // 仅用于列表卡片来源徽章；缺失代表无法解析（已删除 / 老数据）。
  creator_name?: string
  created_at: string
  updated_at: string
  document_count?: number
  chunk_count?: number
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

/**
 * 用户登录
 */
export async function login(data: LoginRequest): Promise<LoginResponse> {
  try {
    const response = await post('/api/v1/auth/login', data)
    return response as unknown as LoginResponse
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.auth.loginFailed')
    }
  }
}

/**
 * 获取认证配置（仅返回前端渲染需要的公开字段，例如注册模式）。
 *
 * 后端通过 `auth.registration_mode` 控制是否允许自助注册：
 *   - "self_serve"  保留现有自助注册入口（默认）
 *   - "invite_only" 关闭注册，要求管理员邀请
 *
 * 失败时回落到 self_serve，避免接口异常导致注册入口直接消失。
 */
export interface AuthConfigResponse {
  success: boolean
  registration_mode: 'self_serve' | 'invite_only' | string
  complex_password_enabled: boolean
}

export async function getAuthConfig(): Promise<AuthConfigResponse> {
  try {
    const response = await get('/api/v1/auth/config')
    return response as unknown as AuthConfigResponse
  } catch {
    return { success: false, registration_mode: 'self_serve', complex_password_enabled: false }
  }
}

/**
 * 用户注册
 */
export async function register(data: RegisterRequest): Promise<RegisterResponse> {
  try {
    const response = await post('/api/v1/auth/register', data)
    return response as unknown as RegisterResponse
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.auth.registerFailed')
    }
  }
}

/** Team membership returned by /auth/me to refresh the current role. */
export interface MembershipInfo {
  tenant_id: number
  tenant_name?: string
  role: string
}

/**
 * 获取当前用户信息
 */
export interface AuthCapabilities {
  auto_accept_invitation: boolean
}

export async function getCurrentUser(): Promise<{ success: boolean; data?: { user: UserInfo; tenant?: TenantInfo | null; memberships?: MembershipInfo[]; tenant_required?: boolean; capabilities?: AuthCapabilities }; message?: string }> {
  try {
    const response = await get('/api/v1/auth/me')
    return response as unknown as { success: boolean; data?: { user: UserInfo; tenant?: TenantInfo | null; memberships?: MembershipInfo[]; tenant_required?: boolean; capabilities?: AuthCapabilities }; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.auth.getUserFailed')
    }
  }
}

/**
 * 获取当前空间信息
 */
export async function getCurrentTenant(): Promise<{ success: boolean; data?: TenantInfo; message?: string }> {
  try {
    const response = await get('/api/v1/auth/tenant')
    return response as unknown as { success: boolean; data?: TenantInfo; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.auth.getTenantFailed')
    }
  }
}

/**
 * 刷新Token
 */
export async function refreshToken(refreshToken: string): Promise<{ success: boolean; data?: { token: string; refreshToken: string }; message?: string }> {
  try {
    const response: any = await post('/api/v1/auth/refresh', { refreshToken })
    if (response && response.success) {
      if (response.access_token || response.refresh_token) {
        return {
          success: true,
          data: {
            token: response.access_token,
            refreshToken: response.refresh_token,
          }
        }
      }
    }

    // 其他情况直接返回原始消息
    return {
      success: false,
      message: response?.message || t('error.auth.refreshTokenFailed')
    }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.auth.refreshTokenFailed')
    }
  }
}

/**
 * 用户登出
 */
export async function logout(): Promise<{ success: boolean; message?: string }> {
  try {
    await post('/api/v1/auth/logout', {})
    return {
      success: true
    }
  } catch (error: any) {
    return {
      success: false,
      message: error.message || t('error.auth.logoutFailed')
    }
  }
}

export interface ChangePasswordRequest {
  old_password: string
  new_password: string
}

/** Map change-password API failures to localized UI strings. */
export function resolveChangePasswordError(error: any): string {
  const details =
    typeof error?.error?.details === 'string'
      ? error.error.details
      : typeof error?.details === 'string'
        ? error.details
        : ''
  switch (details) {
    case 'invalid_old_password':
      return t('userProfile.changePassword.failed')
    case 'password_policy':
      return t('userProfile.changePassword.policyFailed')
    case 'same_password':
      return t('userProfile.changePassword.sameAsCurrent')
    default:
      return error?.message || t('userProfile.changePassword.failed')
  }
}

/**
 * Self-service password rotation. On success the backend revokes every
 * outstanding session for the caller, so the client should clear local
 * auth state and send the user back to /login.
 */
export async function changePassword(
  data: ChangePasswordRequest,
): Promise<{ success: boolean; message?: string }> {
  try {
    const response = await post('/api/v1/auth/change-password', data)
    return response as unknown as { success: boolean; message?: string }
  } catch (error: any) {
    return {
      success: false,
      message: resolveChangePasswordError(error),
    }
  }
}

/**
 * 验证Token有效性
 */
export async function validateToken(): Promise<{ success: boolean; valid?: boolean; message?: string }> {
  try {
    const response = await get('/api/v1/auth/validate')
    return response as unknown as { success: boolean; valid?: boolean; message?: string }
  } catch (error: any) {
    return {
      success: false,
      valid: false,
      message: error.message || t('error.auth.validateTokenFailed')
    }
  }
}

// ---- share-link registration --------------------------------------------

// InviteLookup is the public projection of a share-link row used by
// /register?token=xxx — enough to render the registration page header
// ("X invited you to Y") without leaking sensitive inviter fields.
export interface InviteLookup {
  tenant_id: number
  tenant_name?: string
  role: string
  expires_at: string
}

export interface InviteLookupResponse {
  success: boolean
  data?: InviteLookup
  message?: string
}

export interface RegisterByInviteRequest {
  token: string
  email: string
  username: string
  password: string
}

/**
 * Resolve a share-link token (no auth) into the context the
 * registration page needs (tenant name, role, expiry). Returns 410
 * when the link is invalid / revoked / expired.
 *
 * Uses POST + body (rather than GET + path) so the plaintext token
 * never appears in access logs, browser history, or tracing spans.
 */
export async function getInvitationByToken(token: string): Promise<InviteLookupResponse> {
  try {
    const response = await post(`/api/v1/auth/invitations/lookup`, { token })
    return response as unknown as InviteLookupResponse
  } catch (error: any) {
    return { success: false, message: error.message || '' }
  }
}

/**
 * Complete registration via a share-link token. The invitee supplies
 * their own email — the token is the authorisation, not an identity
 * lock.
 */
export async function registerByInvite(data: RegisterByInviteRequest): Promise<LoginResponse> {
  try {
    const response = await post('/api/v1/auth/register-by-invite', data)
    return response as unknown as LoginResponse
  } catch (error: any) {
    return { success: false, message: error.message || t('error.auth.registerFailed') }
  }
}
