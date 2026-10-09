import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import { getCurrentUser, userInfoFromApi } from '@/api/auth'
import type { DeploymentCapabilityKey } from '@/config/deploymentCapabilities'
import { MessagePlugin } from 'tdesign-vue-next'
import i18n from '@/i18n'
import { createApplicationRoutes } from './routes'
import { notifyRemovedFeatureRedirect } from './routeFallbacks'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: createApplicationRoutes(import.meta.env.DEV),
});

async function hydrateSessionFromToken(authStore: ReturnType<typeof useAuthStore>) {
  const token = localStorage.getItem('reliquary_token')
  if (!token) return false

  if (!authStore.token) {
    authStore.setToken(token)
  }

  const storedRefreshToken = localStorage.getItem('reliquary_refresh_token')
  if (storedRefreshToken && !authStore.refreshToken) {
    authStore.setRefreshToken(storedRefreshToken)
  }

  try {
    const response = await getCurrentUser()
    const user = response.data?.user
    if (!response.success || !user) {
      return false
    }

    authStore.setUser(userInfoFromApi(user, response.data?.tenant?.id))

    const tenant = response.data?.tenant
    if (tenant) {
      authStore.setTenant({
        id: String(tenant.id) || '',
        name: tenant.name || '',
        owner_id: tenant.owner_id || user.id || '',
        description: tenant.description,
        status: tenant.status,
        business: tenant.business,
        storage_used: tenant.storage_used,
        created_at: tenant.created_at || new Date().toISOString(),
        updated_at: tenant.updated_at || new Date().toISOString(),
      })
    } else {
      authStore.setTenant(null)
    }

    return true
  } catch {
    return false
  }
}

// 路由守卫：检查认证状态和系统初始化状态
router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore()

  // 如果访问的是登录页面或初始化页面，直接放行
  if (to.meta.requiresAuth === false || to.meta.requiresInit === false) {
    // 如果已登录用户访问登录页面，重定向到知识库列表页面
    if (to.path === '/login' && authStore.isLoggedIn) {
      next('/platform/knowledge-bases')
      return
    }
    next()
    return
  }

  // 检查用户认证状态
  if (to.meta.requiresAuth !== false) {
    if (!authStore.isLoggedIn) {
      const restored = await hydrateSessionFromToken(authStore)
      if (!restored) {
        next('/login')
        return
      }
    }
  }

  if (to.meta.requiresTenant !== false && !authStore.hasValidTenant) {
    next('/login')
    return
  }

  // 部署能力只描述“后端是否提供该功能”，不反映服务健康或是否已配置。
  // 探测失败时 Store 会 fail-open，真正的权限和可用性仍由后端接口校验。
  const deploymentCapabilities = useDeploymentCapabilitiesStore()
  await deploymentCapabilities.ensureLoaded()
  const requiredCapability = to.meta.requiredCapability as DeploymentCapabilityKey | undefined
  if (requiredCapability && !deploymentCapabilities.isSupported(requiredCapability)) {
    MessagePlugin.warning(i18n.global.t('settings.capabilityUnavailable'))
    next('/platform/knowledge-bases')
    return
  }

  next()
})

router.afterEach((to, _from, failure) => {
  if (!failure) {
    notifyRemovedFeatureRedirect(to, () => MessagePlugin.info(i18n.global.t('navigation.removedFeature')))
  }
})

export default router
