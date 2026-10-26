import { createRouter, createWebHistory } from 'vue-router'
import { useSpaceStore } from '@/stores/space'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import type { DeploymentCapabilityKey } from '@/config/deploymentCapabilities'
import { MessagePlugin } from 'tdesign-vue-next'
import i18n from '@/i18n'
import { createApplicationRoutes } from './routes'
import { notifyRemovedFeatureRedirect } from './routeFallbacks'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: createApplicationRoutes(import.meta.env.DEV),
});

// 路由守卫：载入工作区与部署能力
router.beforeEach(async (to, from, next) => {
  const spaceStore = useSpaceStore()

  // 首次进入时载入个人工作区。
  if (!spaceStore.hasValidSpace) {
    await spaceStore.refresh()
  }

  // 部署能力只描述“后端是否提供该功能”，不反映服务健康或是否已配置。
  // 探测失败时 Store 会 fail-open，真正的权限和可用性仍由后端接口校验。
  const deploymentCapabilities = useDeploymentCapabilitiesStore()
  await deploymentCapabilities.ensureLoaded()
  const requiredCapability = to.meta.requiredCapability as DeploymentCapabilityKey | undefined
  if (requiredCapability && !deploymentCapabilities.isSupported(requiredCapability)) {
    MessagePlugin.warning(i18n.global.t('settings.capabilityUnavailable'))
    next('/platform/entries')
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
