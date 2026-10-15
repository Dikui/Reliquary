import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { UserInfo, SpaceInfo } from '@/api/auth'
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
  const space = ref<SpaceInfo | null>(null)
  const token = ref<string>('')
  const refreshToken = ref<string>('')
  // 计算属性
  const isLoggedIn = computed(() => {
    return !!token.value && !!user.value
  })

  const hasValidSpace = computed(() => {
    return !!space.value && !!space.value.id
  })

  const currentSpaceId = computed(() => {
    return space.value?.id || ''
  })
  const currentSpaceName = computed(() => space.value?.name || '')


  const currentUserId = computed(() => {
    return user.value?.id || ''
  })

  const effectiveSpaceId = computed(() => space.value?.id ? Number(space.value.id) : null)


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

  const setSpace = (spaceData: SpaceInfo | null) => {
    space.value = spaceData
    if (spaceData) {
      localStorage.setItem('reliquary_tenant', JSON.stringify(spaceData))
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

  // Refresh the current user and workspace.
  const refreshFromAuthMe = async (): Promise<boolean> => {
    try {
      const { getCurrentUser } = await import('@/api/auth')
      const response = await getCurrentUser()
      const u = response.data?.user
      if (!response.success || !u) return false

      setUser(userInfoFromApi(u, response.data?.space?.id))

      const spaceSnapshot = response.data?.space
      if (spaceSnapshot) {
        setSpace({
          id: String(spaceSnapshot.id) || '',
          name: spaceSnapshot.name || '',
          owner_id: spaceSnapshot.owner_id || u.id || '',
          description: spaceSnapshot.description,
          status: spaceSnapshot.status,
          business: spaceSnapshot.business,
          storage_used: spaceSnapshot.storage_used,
          created_at: spaceSnapshot.created_at || new Date().toISOString(),
          updated_at: spaceSnapshot.updated_at || new Date().toISOString(),
        })
      } else {
        setSpace(null)
      }

      return true
    } catch {
      return false
    }
  }

  const logout = () => {
    // 清空状态
    user.value = null
    space.value = null
    token.value = ''
    refreshToken.value = ''
    clearSessionResourceCaches()

    // 清空localStorage
    localStorage.removeItem('reliquary_user')
    localStorage.removeItem('reliquary_tenant')
    localStorage.removeItem('reliquary_token')
    localStorage.removeItem('reliquary_refresh_token')
    localStorage.removeItem('reliquary_libraries')
    localStorage.removeItem('reliquary_current_library')
    reloadUserPreferences()
  }

  const initFromStorage = () => {
    // 从localStorage恢复状态
    const storedUser = localStorage.getItem('reliquary_user')
    // The existing browser storage key survives upgrades. HTTP uses current names.
    const storedSpace = localStorage.getItem('reliquary_tenant')
    const storedToken = localStorage.getItem('reliquary_token')
    const storedRefreshToken = localStorage.getItem('reliquary_refresh_token')

    if (storedUser) {
      try {
        const cached = JSON.parse(storedUser)
        if (cached.space_id === undefined && cached['tenant_id'] !== undefined) {
          cached.space_id = cached['tenant_id']
          delete cached['tenant_id']
        }
        user.value = userInfoFromApi(cached)
      } catch (e) {
        console.error(i18n.global.t('authStore.errors.parseUserFailed'), e)
      }
    }

    if (storedSpace) {
      try {
        space.value = JSON.parse(storedSpace)
      } catch (e) {
        console.error(i18n.global.t('authStore.errors.parseSpaceFailed'), e)
      }
    }

    if (storedToken) {
      token.value = storedToken
    }

    if (storedRefreshToken) {
      refreshToken.value = storedRefreshToken
    }




  }

  // 初始化时从localStorage恢复状态
  initFromStorage()

  return {
    // 状态
    user,
    space,
    token,
    refreshToken,

    // 计算属性
    isLoggedIn,
    hasValidSpace,
    currentSpaceId,
    currentSpaceName,
    currentUserId,
    effectiveSpaceId,

    // 方法
    setUser,
    setSpace,
    setToken,
    setRefreshToken,
    refreshFromAuthMe,
    logout,
    initFromStorage
  }
})
