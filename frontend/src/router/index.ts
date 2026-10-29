import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import type { DeploymentCapabilityKey } from '@/config/deploymentCapabilities'
import { toast } from '@/components/ui/feedback'
import i18n from '@/i18n'
import { createApplicationRoutes } from './routes'
import { notifyRemovedFeatureRedirect } from './routeFallbacks'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: createApplicationRoutes(import.meta.env.DEV),
});

// This branch and its lazy import are removed from production bundles.
if (import.meta.env.DEV) {
  router.addRoute({
    path: '/platform/dev/ui',
    name: 'uiPreview',
    component: () => import('../views/dev/UiPreview.vue'),
  })
}

// 路由守卫：载入唯一用户与部署能力
router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore()

  // 没有登录：首次进入时从 /auth/me 取唯一用户和空间。
  if (!authStore.hasValidSpace) {
    await authStore.refreshFromAuthMe()
  }

  // 部署能力只描述“后端是否提供该功能”，不反映服务健康或是否已配置。
  // 探测失败时 Store 会 fail-open，真正的权限和可用性仍由后端接口校验。
  const deploymentCapabilities = useDeploymentCapabilitiesStore()
  await deploymentCapabilities.ensureLoaded()
  const requiredCapability = to.meta.requiredCapability as DeploymentCapabilityKey | undefined
  if (requiredCapability && !deploymentCapabilities.isSupported(requiredCapability)) {
    toast.warning(i18n.global.t('settings.capabilityUnavailable'))
    next('/platform/entries')
    return
  }

  next()
})

router.afterEach((to, _from, failure) => {
  if (!failure) {
    notifyRemovedFeatureRedirect(to, () => toast.info(i18n.global.t('navigation.removedFeature')))
  }
})

export default router
