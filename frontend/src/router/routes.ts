import type { RouteRecordRaw } from 'vue-router'
import { normalizeSettingsSection } from '../config/settingsRoute'
import { removedPlatformRoutes, removedRootRoutes } from './routeFallbacks'

const knowledgePage = () => import('../views/knowledge/KnowledgeBase.vue')
const notFound = () => import('../views/NotFound.vue')

// Settings is owned by the platform layout. Its route supplies only the background.
export function createApplicationRoutes(development = false): RouteRecordRaw[] {
  return [
    {
      path: "/",
      redirect: "/platform/knowledge",
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
      redirect: "/platform/knowledge",
      component: () => import("../views/platform/index.vue"),
      meta: { requiresInit: true, requiresAuth: true },
      children: [
        ...removedPlatformRoutes,
        {
          path: "tenant",
          redirect: "/platform/settings"
        },
        {
          path: "settings",
          name: "settings",
          component: knowledgePage,
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "knowledge-bases",
          redirect: "/platform/knowledge",
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "knowledge-bases/:legacyKnowledgeBaseId/:pathMatch(.*)*",
          redirect: to => ({ path: '/platform/knowledge', query: to.query }),
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "knowledge",
          name: "knowledge",
          component: knowledgePage,
          meta: { requiresInit: true, requiresAuth: true }
        },
        {
          path: "knowledge-search",
          name: "knowledgeSearch",
          component: () => import('../views/knowledge/KnowledgeSearch.vue'),
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
