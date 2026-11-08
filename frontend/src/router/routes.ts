import type { RouteRecordRaw } from 'vue-router'
import { normalizeSettingsSection } from '../config/settingsRoute'
import { removedPlatformRoutes, removedRootRoutes } from './routeFallbacks'

const libraryPage = () => import('../views/library/LibraryPage.vue')
const libraryList = () => import('../views/library/LibraryList.vue')
const entryLocation = (path: string, to: { query: Record<string, any>; hash: string }) => ({ path, query: { ...to.query }, hash: to.hash })
const notFound = () => import('../views/NotFound.vue')

// Settings is an independent page inside the platform shell.
export function createApplicationRoutes(development = false): RouteRecordRaw[] {
  return [
    {
      path: "/",
      redirect: "/platform/libraries",
    },
    {
      // 登录已移除，旧书签回到资料页。
      path: "/login",
      redirect: "/platform/libraries",
    },
    {
      path: "/platform",
      name: "Platform",
      redirect: "/platform/libraries",
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
          component: () => import("../views/settings/Settings.vue"),
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "libraries",
          name: "libraries",
          component: libraryList,
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "libraries/:legacyLibraryId/:pathMatch(.*)*",
          redirect: to => ({ ...entryLocation('/platform/entries', to), query: { ...to.query, libraries: String(to.params.legacyLibraryId) } }),
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "entries",
          name: "entries",
          beforeEnter: to => {
            if (to.query.tab === 'reviews' || (to.query.tab === 'entities' && to.query.section === 'merges')) {
              const { tab, section, ...query } = to.query
              return { path: '/platform/pending', query: { ...query, tab: tab === 'reviews' ? 'reviews' : 'merges' }, hash: to.hash, replace: true }
            }
            if (to.query.tab === 'entities') {
              const { tab, ...query } = to.query
              return { path: '/platform/entities', query, hash: to.hash, replace: true }
            }
            if (typeof to.query.entry_id !== 'string' || !to.query.entry_id) return
            const { entry_id: id, ...query } = to.query
            return { name: 'entryDetail', params: { id }, query, hash: to.hash, replace: true }
          },
          component: libraryPage,
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: 'pending',
          name: 'pending',
          component: () => import('../views/library/PendingPage.vue'),
          meta: { requiresInit: true, requiresAuth: true },
        },
        {
          path: 'entities',
          name: 'entities',
          component: () => import('../views/library/EntitiesPage.vue'),
          meta: { requiresInit: true, requiresAuth: true },
        },
        {
          path: 'entries/:id',
          name: 'entryDetail',
          component: () => import('../views/library/EntryDetail.vue'),
          meta: { requiresInit: true, requiresAuth: true },
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
        // standard Settings page; keep stable URLs for bookmarks and
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
