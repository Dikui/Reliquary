import type { RouteRecordRaw } from 'vue-router'
import { normalizeSettingsSection } from '../config/settingsRoute'
import { removedPlatformRoutes, removedRootRoutes } from './routeFallbacks'

const libraryPage = () => import('../views/knowledge-base/KnowledgeBase.vue')
const legacyEntryLocation = (path: string, to: { query: Record<string, any>; hash: string }) => {
  const query = { ...to.query }
  if (query.entry_id == null && query.knowledge_id != null) query.entry_id = query.knowledge_id
  delete query.knowledge_id
  return { path, query, hash: to.hash }
}
const notFound = () => import('../views/NotFound.vue')

// Settings is owned by the platform layout. Its route supplies only the background.
export function createApplicationRoutes(development = false): RouteRecordRaw[] {
  return [
    {
      path: "/",
      redirect: "/platform/entries",
    },
    {
      path: "/login",
      name: "login",
      component: () => import("../views/auth/Login.vue"),
      meta: { requiresAuth: false, requiresInit: false }
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
          path: 'knowledge',
          redirect: to => legacyEntryLocation('/platform/entries', to),
        },
        {
          path: 'knowledge-search',
          redirect: to => legacyEntryLocation('/platform/search', to),
        },
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
          path: "knowledge-bases",
          redirect: "/platform/entries",
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "knowledge-bases/:legacyKnowledgeBaseId/:pathMatch(.*)*",
          redirect: to => legacyEntryLocation('/platform/entries', to),
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "entries",
          name: "entries",
          component: libraryPage,
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "topics",
          name: "topics",
          component: () => import('../views/knowledge-base/TopicManagement.vue'),
          meta: { requiresInit: true, requiresAuth: true },
        },
        {
          path: "follows",
          name: "follows",
          component: () => import('../views/knowledge-base/FollowFeed.vue'),
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
          component: () => import('../views/knowledge-base/SearchView.vue'),
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
