import type { RouteLocationNormalizedLoaded, RouteRecordRaw } from 'vue-router'

const removedRoute = (path: string): RouteRecordRaw => ({
  path,
  redirect: { name: 'knowledgeBaseList', params: {}, query: {}, hash: '' },
  meta: { removedFeature: true },
})

export const removedRootRoutes: RouteRecordRaw[] = [
  '/join',
  '/onboarding/workspace/:pathMatch(.*)*',
  '/embed/:pathMatch(.*)*',
  '/embed.html',
].map(removedRoute)

export const removedPlatformRoutes: RouteRecordRaw[] = [
  'organizations/:pathMatch(.*)*',
  'shared-spaces/:pathMatch(.*)*',
].map(removedRoute)

// Redirect metadata lasts for one navigation and never survives a refresh.
const notifiedRedirects = new WeakSet<object>()

export function notifyRemovedFeatureRedirect(
  to: Pick<RouteLocationNormalizedLoaded, 'redirectedFrom'>,
  notify: () => unknown,
): void {
  const source = to.redirectedFrom
  if (!source?.meta.removedFeature || notifiedRedirects.has(source)) return
  notifiedRedirects.add(source)
  notify()
}
