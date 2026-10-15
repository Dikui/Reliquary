import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  getSystemInfo,
  type SystemInfo,
} from '@/api/system'
import { getSpaceRetrievalConfig } from '@/api/retrieval'

const CACHE_TTL_MS = 60_000

type EditorResourceKey =
  | 'spaceRetrievalConfig'
  | 'systemInfo'

export const useEditorResourcesStore = defineStore('editorResources', () => {
  const spaceRetrievalConfig = ref<Record<string, unknown> | null>(null)
  const systemInfo = ref<SystemInfo | null>(null)

  const loadedAt = ref<Partial<Record<EditorResourceKey, number>>>({})
  const inflight = new Map<EditorResourceKey, Promise<void>>()

  function isFresh(key: EditorResourceKey): boolean {
    const at = loadedAt.value[key]
    return !!at && Date.now() - at < CACHE_TTL_MS
  }

  async function runOnce(key: EditorResourceKey, force: boolean, loader: () => Promise<void>): Promise<void> {
    if (!force && isFresh(key)) return
    const existing = inflight.get(key)
    if (existing) return existing
    const p = loader().finally(() => inflight.delete(key))
    inflight.set(key, p)
    return p
  }

  async function ensureSpaceRetrievalConfig(force = false): Promise<void> {
    return runOnce('spaceRetrievalConfig', force, async () => {
      const retrievalRes: any = await getSpaceRetrievalConfig()
      spaceRetrievalConfig.value = retrievalRes?.data ?? null
      loadedAt.value.spaceRetrievalConfig = Date.now()
    })
  }

  async function ensureSystemInfo(force = false): Promise<void> {
    return runOnce('systemInfo', force, async () => {
      const response = await getSystemInfo()
      systemInfo.value = response?.data ?? null
      loadedAt.value.systemInfo = Date.now()
    })
  }

  function invalidate(...keys: EditorResourceKey[]) {
    if (keys.length === 0) {
      loadedAt.value = {}
      spaceRetrievalConfig.value = null
      systemInfo.value = null
      inflight.clear()
      return
    }
    keys.forEach((k) => {
      delete loadedAt.value[k]
      inflight.delete(k)
    })
  }

  return {
    spaceRetrievalConfig,
    systemInfo,
    ensureSpaceRetrievalConfig,
    ensureSystemInfo,
    invalidate,
  }
})
