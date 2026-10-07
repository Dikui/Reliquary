import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { UserInfo, TenantInfo, KnowledgeBaseInfo } from '@/api/auth'
import { userInfoFromApi } from '@/api/auth'
import i18n from '@/i18n'
import { reloadFontFromStorage } from '@/composables/useFont'
import { reloadThemeFromStorage } from '@/composables/useTheme'
import { resetMigrationLatch } from '@/composables/preferenceStorage'
import { BUILTIN_QUICK_ANSWER_ID } from '@/api/agent'
import { useChatResourcesStore } from '@/stores/chatResources'
import { useEditorResourcesStore } from '@/stores/editorResources'

/** 登出时丢弃 Pinia 内的空间级资源缓存，避免 SPA 重登复用上一账号数据。 */
function clearSessionResourceCaches() {
  useChatResourcesStore().invalidate()
  useEditorResourcesStore().invalidate()
}

// Per-user UI preferences are namespaced by user id in localStorage.
// Reload them whenever the active user changes.
function reloadUserPreferences() {
  // Reset the latch so migration runs once for the new active user.
  resetMigrationLatch()
  reloadFontFromStorage()
  reloadThemeFromStorage()
}

export const useAuthStore = defineStore('auth', () => {
  // 状态
  const user = ref<UserInfo | null>(null)
  const tenant = ref<TenantInfo | null>(null)
  const token = ref<string>('')
  const refreshToken = ref<string>('')
  const knowledgeBases = ref<KnowledgeBaseInfo[]>([])
  const currentKnowledgeBase = ref<KnowledgeBaseInfo | null>(null)
  // Memberships carry the team role returned by the authentication API.
  const memberships = ref<Array<{ tenant_id: number; tenant_name?: string; role: string }>>([])
  // pendingInvitationCount is the number of pending tenant invitations
  // addressed to the current user. Renders as a badge next to the
  // avatar; updated by fetchPendingInvitationCount, which runs after
  // login and on a polling interval. Polling
  // (vs SSE) is fine — the count is checked rarely and a 1-2 minute
  // staleness window is acceptable for an inbox indicator.
  const pendingInvitationCount = ref<number>(0)

  const autoAcceptInvitation = ref(false)

  // 计算属性
  const isLoggedIn = computed(() => {
    return !!token.value && !!user.value
  })

  const hasValidTenant = computed(() => {
    return !!tenant.value && !!tenant.value.id
  })

  const currentTenantId = computed(() => {
    return tenant.value?.id || ''
  })
const currentTenantName = computed(() => tenant.value?.name || '')


  const currentUserId = computed(() => {
    return user.value?.id || ''
  })

  // isSystemAdmin reflects the platform-wide system-administrator flag
  // (User.IsSystemAdmin on the server). It is independent of per-tenant
  // Owner/Admin/Contributor/Viewer roles — a system admin can manage
  // global settings, built-in models, and other system admins regardless
  // of which tenant they're currently scoped into.
  //
  // SECURITY: same caveat as currentTenantRole — this value is hydrated
  // from localStorage on reload and is therefore tamper-prone client-side.
  // Use it ONLY to gate UI visibility (menu entries, route guards). All
  // real authorisation lives in the server-side RequireSystemAdmin
  // middleware (see internal/middleware/rbac.go). A user who flips this
  // bit in DevTools will get a 403 the moment they hit a guarded endpoint.
  const isSystemAdmin = computed(() => {
    return user.value?.is_system_admin === true
  })
const currentTenantRole = computed(() => memberships.value.find(m => String(m.tenant_id) === String(tenant.value?.id))?.role || '')


  // hasRole answers "is the current tenant role at least <min>?", used by
  // role-aware UI gating across KB / Agent / settings views. The numeric
  // ordering (viewer < contributor < admin < owner) mirrors the server-side
  // matrix in middleware/rbac.go so a v-if here lines up with the 403 the
  // backend would return.
  //
  // SECURITY: shares the same caveat as currentTenantRole — derived from
  // localStorage, MUST be treated as UI-rendering-only. Never rely on
  // hasRole() for security decisions; the server is the source of truth.
  const ROLE_LEVEL: Record<string, number> = {
    viewer: 10,
    contributor: 20,
    admin: 30,
    owner: 40,
  }
  const hasRole = (min: 'viewer' | 'contributor' | 'admin' | 'owner'): boolean => {
    return (ROLE_LEVEL[currentTenantRole.value] ?? 0) >= ROLE_LEVEL[min]
  }
const effectiveTenantId = computed(() => tenant.value?.id ? Number(tenant.value.id) : null)


  // 操作方法
  const setUser = (userData: UserInfo) => {
    const previousId = user.value?.id
    user.value = userData
    // 保存到localStorage
    localStorage.setItem('reliquary_user', JSON.stringify(userData))
    if (previousId !== userData.id) {
      reloadUserPreferences()
    }
  }

  const setTenant = (tenantData: TenantInfo | null) => {
    tenant.value = tenantData
    if (tenantData) {
      localStorage.setItem('reliquary_tenant', JSON.stringify(tenantData))
    } else {
      localStorage.removeItem('reliquary_tenant')
    }
  }

  const setToken = (tokenValue: string) => {
    token.value = tokenValue
    localStorage.setItem('reliquary_token', tokenValue)
  }

  const setRefreshToken = (refreshTokenValue: string) => {
    refreshToken.value = refreshTokenValue
    localStorage.setItem('reliquary_refresh_token', refreshTokenValue)
  }

  const setKnowledgeBases = (kbList: KnowledgeBaseInfo[]) => {
    // 确保输入是数组
    knowledgeBases.value = Array.isArray(kbList) ? kbList : []
    localStorage.setItem('reliquary_knowledge_bases', JSON.stringify(knowledgeBases.value))
  }

  const setCurrentKnowledgeBase = (kb: KnowledgeBaseInfo | null) => {
    currentKnowledgeBase.value = kb
    if (kb) {
      localStorage.setItem('reliquary_current_kb', JSON.stringify(kb))
    } else {
      localStorage.removeItem('reliquary_current_kb')
    }
  }

  const setMemberships = (
    list: Array<{ tenant_id: number; tenant_name?: string; role: string }>
  ) => {
    memberships.value = Array.isArray(list) ? list : []
    localStorage.setItem('reliquary_memberships', JSON.stringify(memberships.value))
  }

  // setPendingInvitationCount is the explicit setter used by the
  // polling composable below. Keeping the mutation behind a setter
  // matches the pattern of every other piece of auth state and lets
  // future devtools / strict-mode checks intercept the write.
  const setPendingInvitationCount = (n: number) => {
    pendingInvitationCount.value = Number.isFinite(n) && n >= 0 ? Math.floor(n) : 0
  }

  const setAutoAcceptInvitation = (enabled: boolean) => {
    autoAcceptInvitation.value = enabled
  }

  // fetchPendingInvitationCount hits the dedicated /me/invitations/
  // pending-count endpoint and updates the store. Errors are
  // swallowed — the badge degrades to its last-known value instead
  // of nagging the user with a toast. The caller can choose to fire
  // and forget (login flow) or await (manual refresh button).
  const fetchPendingInvitationCount = async () => {
    // Import lazily to avoid circular module ordering between auth
    // store, axios interceptor, and i18n bootstrapping. The store is
    // imported by very early modules in main.ts; a top-level api
    // import would tighten that graph unnecessarily.
    try {
      const { getMyPendingInvitationCount } = await import('@/api/tenant/invitations')
      const resp = await getMyPendingInvitationCount()
      if (resp.success && resp.data) {
        setPendingInvitationCount(resp.data.pending_count)
      }
    } catch {
      // best-effort; keep last known value
    }
  }

  // Refresh the current user, team, and role with GET /api/v1/auth/me.
  // Login only populated memberships once; SPA navigations skip
  // hydrateSessionFromToken when isLoggedIn is already true — so inviting
  // flows or revokes would leave the sidebar switcher stale until reload.
  const refreshFromAuthMe = async (): Promise<boolean> => {
    try {
      const { getCurrentUser } = await import('@/api/auth')
      const response = await getCurrentUser()
      const u = response.data?.user
      if (!response.success || !u) return false

      setUser(userInfoFromApi(u, response.data?.tenant?.id))

      const tenantSnapshot = response.data?.tenant
      if (tenantSnapshot) {
        setTenant({
          id: String(tenantSnapshot.id) || '',
          name: tenantSnapshot.name || '',
          owner_id: tenantSnapshot.owner_id || u.id || '',
          description: tenantSnapshot.description,
          status: tenantSnapshot.status,
          business: tenantSnapshot.business,
          storage_quota: tenantSnapshot.storage_quota,
          storage_used: tenantSnapshot.storage_used,
          created_at: tenantSnapshot.created_at || new Date().toISOString(),
          updated_at: tenantSnapshot.updated_at || new Date().toISOString(),
        })
      } else {
        setTenant(null)
      }

      const list = response.data?.memberships
      if (Array.isArray(list)) {
        setMemberships(list)
      }

      setAutoAcceptInvitation(response.data?.capabilities?.auto_accept_invitation === true)

      return true
    } catch {
      return false
    }
  }

  // 用 token 加入空间并刷新成员关系、切到新空间。token 无效时返回 ok:false（不抛异常），
  // 提示与跳转交给调用方（store 不碰 router）。
  const acceptInvitationByTokenAndRefresh = async (
    token: string,
  ): Promise<{ ok: boolean; tenantId?: number; tenantName?: string }> => {
    try {
      const { acceptInvitationByToken } = await import('@/api/tenant/invitations')
      const resp = await acceptInvitationByToken(token)
      if (!resp.success || !resp.data?.membership) {
        return { ok: false }
      }
      const tenantId = resp.data.membership.tenant_id
      const tenantName = resp.data.tenant_name
      // 刷新成员关系，并切到刚加入的空间。
      await refreshFromAuthMe()
      return { ok: true, tenantId, tenantName }
    } catch {
      return { ok: false }
    }
  }

  const logout = () => {
    // 清空状态
    user.value = null
    tenant.value = null
    token.value = ''
    refreshToken.value = ''
    knowledgeBases.value = []
    currentKnowledgeBase.value = null
    memberships.value = []
    pendingInvitationCount.value = 0
    autoAcceptInvitation.value = false
    clearSessionResourceCaches()

    // 清空localStorage
    localStorage.removeItem('reliquary_user')
    localStorage.removeItem('reliquary_tenant')
    localStorage.removeItem('reliquary_token')
    localStorage.removeItem('reliquary_refresh_token')
    localStorage.removeItem('reliquary_knowledge_bases')
    localStorage.removeItem('reliquary_current_kb')
    localStorage.removeItem('reliquary_memberships')
    reloadUserPreferences()
  }

  const initFromStorage = () => {
    // 从localStorage恢复状态
    const storedUser = localStorage.getItem('reliquary_user')
    const storedTenant = localStorage.getItem('reliquary_tenant')
    const storedToken = localStorage.getItem('reliquary_token')
    const storedRefreshToken = localStorage.getItem('reliquary_refresh_token')
    const storedKnowledgeBases = localStorage.getItem('reliquary_knowledge_bases')
    const storedCurrentKb = localStorage.getItem('reliquary_current_kb')

    if (storedUser) {
      try {
        // 走 userInfoFromApi 把老 localStorage（可能缺新字段，如
        // is_system_admin）规范化一遍，避免「我新加了字段、但老登录态
        // 没经过登录响应处理过、字段就永远是 undefined」的死角。
        // 这是「漏拷 4 处」之外的第 5 个隐藏入口，专门给页面刷新走的。
        user.value = userInfoFromApi(JSON.parse(storedUser))
      } catch (e) {
        console.error(i18n.global.t('authStore.errors.parseUserFailed'), e)
      }
    }

    if (storedTenant) {
      try {
        tenant.value = JSON.parse(storedTenant)
      } catch (e) {
        console.error(i18n.global.t('authStore.errors.parseTenantFailed'), e)
      }
    }

    if (storedToken) {
      token.value = storedToken
    }

    if (storedRefreshToken) {
      refreshToken.value = storedRefreshToken
    }

    if (storedKnowledgeBases) {
      try {
        const parsed = JSON.parse(storedKnowledgeBases)
        knowledgeBases.value = Array.isArray(parsed) ? parsed : []
      } catch (e) {
        console.error(i18n.global.t('authStore.errors.parseKnowledgeBasesFailed'), e)
        knowledgeBases.value = []
      }
    }

    if (storedCurrentKb) {
      try {
        currentKnowledgeBase.value = JSON.parse(storedCurrentKb)
      } catch (e) {
        console.error(i18n.global.t('authStore.errors.parseCurrentKnowledgeBaseFailed'), e)
      }
    }

    const storedMemberships = localStorage.getItem('reliquary_memberships')
    if (storedMemberships) {
      try {
        const parsed = JSON.parse(storedMemberships)
        memberships.value = Array.isArray(parsed) ? parsed : []
      } catch (e) {
        console.error('Failed to parse memberships', e)
        memberships.value = []
      }
    }

  }

  // 初始化时从localStorage恢复状态
  initFromStorage()

  return {
    // 状态
    user,
    tenant,
    token,
    refreshToken,
    knowledgeBases,
    currentKnowledgeBase,
    memberships,
    pendingInvitationCount,
    autoAcceptInvitation,

    // 计算属性
    isLoggedIn,
    hasValidTenant,
    currentTenantId,
    currentTenantName,
    currentUserId,
    isSystemAdmin,
    currentTenantRole,
    hasRole,
    effectiveTenantId,

    // 方法
    setUser,
    setTenant,
    setToken,
    setRefreshToken,
    setKnowledgeBases,
    setCurrentKnowledgeBase,
    setMemberships,
    setPendingInvitationCount,
    setAutoAcceptInvitation,
    fetchPendingInvitationCount,
    refreshFromAuthMe,
    acceptInvitationByTokenAndRefresh,
    logout,
    initFromStorage
  }
})
