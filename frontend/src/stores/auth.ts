import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { UserInfo, TenantInfo, KnowledgeBaseInfo } from '@/api/auth'
import { userInfoFromApi } from '@/api/auth'
import i18n from '@/i18n'
import { reloadFontFromStorage } from '@/composables/useFont'
import { reloadThemeFromStorage } from '@/composables/useTheme'
import { resetMigrationLatch } from '@/composables/preferenceStorage'
import { useResourcesStore } from '@/stores/resources'
import { useEditorResourcesStore } from '@/stores/editorResources'

/** 登出时丢弃 Pinia 内的空间级资源缓存，避免 SPA 重登复用上一账号数据。 */
function clearSessionResourceCaches() {
  useResourcesStore().invalidate()
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

  // Refresh the current user and workspace.
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
          storage_used: tenantSnapshot.storage_used,
          created_at: tenantSnapshot.created_at || new Date().toISOString(),
          updated_at: tenantSnapshot.updated_at || new Date().toISOString(),
        })
      } else {
        setTenant(null)
      }

      return true
    } catch {
      return false
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
    clearSessionResourceCaches()

    // 清空localStorage
    localStorage.removeItem('reliquary_user')
    localStorage.removeItem('reliquary_tenant')
    localStorage.removeItem('reliquary_token')
    localStorage.removeItem('reliquary_refresh_token')
    localStorage.removeItem('reliquary_knowledge_bases')
    localStorage.removeItem('reliquary_current_kb')
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

    // 计算属性
    isLoggedIn,
    hasValidTenant,
    currentTenantId,
    currentTenantName,
    currentUserId,
    effectiveTenantId,

    // 方法
    setUser,
    setTenant,
    setToken,
    setRefreshToken,
    setKnowledgeBases,
    setCurrentKnowledgeBase,
    refreshFromAuthMe,
    logout,
    initFromStorage
  }
})
