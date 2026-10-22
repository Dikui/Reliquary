import type { RouteRecordRaw } from 'vue-router'
import { normalizeSettingsSection } from '../config/settingsRoute'
import { removedPlatformRoutes, removedRootRoutes } from './routeFallbacks'

const libraryPage = () => import('../views/library/Library.vue')
const entryLocation = (path: string, to: { query: Record<string, any>; hash: string }) => ({ path, query: { ...to.query }, hash: to.hash })
const notFound = () => import('../views/NotFound.vue')

// Settings is owned by the platform layout. Its route supplies only the background.
export function createApplicationRoutes(development = false): RouteRecordRaw[] {
  return [
    {
      path: "/",
      redirect: "/platform/entries",
    },
    {
      // 登录已移除，旧书签回到资料页。
      path: "/login",
      redirect: "/platform/entries",
    },
    {
      path: "/platform",
      name: "Platform",
      redirect: "/platform/entries",
      component: () => import("../views/platform/index.vue"),
      meta: { requiresInit: true, requiresAuth: true },
      children: [
        ...removedPlatformRoutes,
        {
          path: "space",
          redirect: "/platform/settings"
        },
        {
          path: "settings",
          name: "settings",
          component: libraryPage,
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "libraries",
          redirect: "/platform/entries",
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "libraries/:legacyLibraryId/:pathMatch(.*)*",
          redirect: to => entryLocation('/platform/entries', to),
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "entries",
          name: "entries",
          component: libraryPage,
          meta: { requiresInit: true, requiresAuth: true }
        },
        { path: 'content-checks', name: 'contentChecks', component: () => import('../views/insights/ContentChecksPage.vue'), meta: { requiresInit: true, requiresAuth: true } },
        { path: 'diagnostics', name: 'diagnostics', component: () => import('../views/insights/IngestFailuresPage.vue'), meta: { requiresInit: true, requiresAuth: true } },
        { path: 'evaluation', name: 'evaluation', component: () => import('../views/insights/EvaluationPage.vue'), meta: { requiresInit: true, requiresAuth: true } },
        { path: 'model-calls', name: 'modelCalls', component: () => import('../views/insights/ModelCallsPage.vue'), meta: { requiresInit: true, requiresAuth: true } },
        {
          path: "topics",
          name: "topics",
          component: () => import('../views/library/TopicManagement.vue'),
          meta: { requiresInit: true, requiresAuth: true },
        },
        {
          path: "follows",
          name: "follows",
          component: () => import('../views/library/FollowFeed.vue'),
          meta: { requiresInit: true, requiresAuth: true },
        },
        {
          path: "operation-logs",
          name: "operationLogs",
          redirect: to => ({ path: '/platform/settings', query: { ...to.query, section: 'operation-logs' } }),
          meta: { requiresInit: true, requiresAuth: true },
        },
        {
          path: "search",
          name: "search",
          component: () => import('../views/library/SearchView.vue'),
          meta: { requiresInit: true, requiresAuth: true },
        },
        {
          path: "integrations",
          redirect: (to) => {
            const tab = typeof to.query.tab === 'string' ? to.query.tab : undefined
            const incoming = typeof to.query.section === 'string' ? to.query.section : 'integrations'
            const rest = { ...to.query }
            delete rest.tab
            return {
              path: '/platform/settings',
              query: {
                ...rest,
                section: normalizeSettingsSection(incoming, tab),
              },
            }
          },
          meta: { requiresInit: true, requiresAuth: true }
        },
        // Compatibility redirects for /platform/system/* URLs. System
        // administration surfaces live as dedicated sections inside the
        // standard Settings modal; keep stable URLs for bookmarks and
        // external links.
        {
          path: "system",
          redirect: { path: "/platform/settings", query: { section: "system-global" } },
          meta: { requiresInit: true, requiresAuth: true },
        },
        {
          path: "system/settings",
          name: "systemSettings",
          redirect: { path: "/platform/settings", query: { section: "system-global" } },
          meta: { requiresInit: true, requiresAuth: true },
        },
        {
          path: "system/queues",
          name: "systemQueues",
          redirect: { path: "/platform/settings", query: { section: "runtime-queues" } },
          meta: { requiresInit: true, requiresAuth: true },
        },
        {
          path: ':pathMatch(.*)*',
          name: 'platformNotFound',
          component: notFound,
          meta: { requiresInit: true, requiresAuth: true },
        },
      ],
    },
    ...removedRootRoutes,
    {
      path: '/:pathMatch(.*)*',
      name: 'notFound',
      component: notFound,
      meta: { requiresAuth: false, requiresInit: false },
    },
  ]
}
