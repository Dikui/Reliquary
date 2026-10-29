import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { useAuthStore } from './auth'

const RECENT_KEY_PREFIX = 'reliquary_cmdk_recent'
const RECENT_LIMIT = 4

export interface CmdkRecentTarget {
  title: string
  path: string
  query?: Record<string, string>
}

function validTarget(value: unknown): value is CmdkRecentTarget {
  if (!value || typeof value !== 'object') return false
  const target = value as CmdkRecentTarget
  return typeof target.title === 'string' && typeof target.path === 'string'
    && /^\/platform\/(entries(?:\/[^/?#]+)?|entities|search|follows|settings)$/.test(target.path)
    && (!target.query || (typeof target.query === 'object' && !Array.isArray(target.query)
      && Object.values(target.query).every(item => typeof item === 'string')))
}

// recentKey scopes "recent searches" to the active (user, space) pair.
// Separate recent searches for users sharing a browser. The "anon" key
// keeps the palette functional before login finishes hydrating.
function recentKey(userId: string | null | undefined, spaceId: number | string | null | undefined): string {
  const u = userId ? String(userId) : 'anon'
  const t = spaceId !== null && spaceId !== undefined && spaceId !== '' ? String(spaceId) : 'none'
  return `${RECENT_KEY_PREFIX}:${u}:${t}`
}

/**
 * Pinia store for the global command palette (⌘K / Ctrl+K).
 * The palette itself is rendered once in platform/index.vue.
 * Any code (router redirect, deep link, programmatic trigger) can open it
 * by calling openPalette(q).
 */
export const useCommandPaletteStore = defineStore('commandPalette', () => {
  const open = ref(false)
  const initialQuery = ref('')
  const recentQueries = ref<string[]>([])
  const recentTargets = ref<CmdkRecentTarget[]>([])

  // Lazily resolve the auth store inside actions / watchers — at store
  // setup time pinia may not have finished registering peer stores yet.
  const currentRecentKey = (): string => {
    const auth = useAuthStore()
    return recentKey(auth.user?.id, auth.effectiveSpaceId)
  }

  const loadRecent = () => {
    try {
      const raw = localStorage.getItem(currentRecentKey())
      const queries: unknown = raw ? JSON.parse(raw) : []
      recentQueries.value = Array.isArray(queries) ? queries.filter((item): item is string => typeof item === 'string').slice(0, RECENT_LIMIT) : []
      const targets: unknown = JSON.parse(localStorage.getItem(`${currentRecentKey()}:targets`) || '[]')
      recentTargets.value = Array.isArray(targets) ? targets.filter(validTarget).slice(0, RECENT_LIMIT) : []
    } catch {
      recentQueries.value = []
      recentTargets.value = []
    }
  }

  const openPalette = (query = '') => {
    initialQuery.value = query
    open.value = true
  }

  const closePalette = () => {
    open.value = false
    initialQuery.value = ''
  }

  const pushRecent = (q: string) => {
    const trimmed = q.trim()
    if (!trimmed) return
    recentQueries.value = [
      trimmed,
      ...recentQueries.value.filter(x => x !== trimmed),
    ].slice(0, RECENT_LIMIT)
    try {
      localStorage.setItem(currentRecentKey(), JSON.stringify(recentQueries.value))
    } catch {
      /* ignore quota errors */
    }
  }

  const clearRecent = () => {
    recentQueries.value = []
    recentTargets.value = []
    try {
      localStorage.removeItem(currentRecentKey())
      localStorage.removeItem(`${currentRecentKey()}:targets`)
    } catch {
      /* ignore */
    }
  }

  const pushTarget = (target: CmdkRecentTarget) => {
    if (!validTarget(target)) return
    const key = JSON.stringify([target.path, target.query])
    recentTargets.value = [target, ...recentTargets.value.filter(item => JSON.stringify([item.path, item.query]) !== key)].slice(0, RECENT_LIMIT)
    try { localStorage.setItem(`${currentRecentKey()}:targets`, JSON.stringify(recentTargets.value)) } catch { /* Storage may be full. */ }
  }

  // Reload recent queries when login or logout changes the active user.
  watch(
    () => {
      const auth = useAuthStore()
      return [auth.user?.id, auth.effectiveSpaceId] as const
    },
    () => loadRecent(),
    { immediate: true },
  )

  return {
    open,
    initialQuery,
    recentQueries,
    recentTargets,
    openPalette,
    closePalette,
    pushRecent,
    pushTarget,
    clearRecent,
    loadRecent,
  }
})
