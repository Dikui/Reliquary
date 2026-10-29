import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { useAuthStore } from './auth'

const RECENT_KEY_PREFIX = 'reliquary_cmdk_recent'
const RECENT_LIMIT = 4

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

  // Lazily resolve the auth store inside actions / watchers — at store
  // setup time pinia may not have finished registering peer stores yet.
  const currentRecentKey = (): string => {
    const auth = useAuthStore()
    return recentKey(auth.user?.id, auth.effectiveSpaceId)
  }

  const loadRecent = () => {
    try {
      const raw = localStorage.getItem(currentRecentKey())
      recentQueries.value = raw ? JSON.parse(raw) : []
    } catch {
      recentQueries.value = []
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
    try {
      localStorage.removeItem(currentRecentKey())
    } catch {
      /* ignore */
    }
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
    openPalette,
    closePalette,
    pushRecent,
    clearRecent,
    loadRecent,
  }
})
