import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  getParserEngines,
  getSystemInfo,
  type ParserEngineInfo,
  type SystemInfo,
} from '@/api/system'
import { getTenantRetrievalConfig } from '@/api/retrieval'

const CACHE_TTL_MS = 60_000

type EditorResourceKey =
  | 'tenantRetrievalConfig'
  | 'parserEngines'
  | 'systemInfo'

export const useEditorResourcesStore = defineStore('editorResources', () => {
  const tenantRetrievalConfig = ref<Record<string, unknown> | null>(null)
  const parserEngines = ref<ParserEngineInfo[]>([])
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

  async function ensureTenantRetrievalConfig(force = false): Promise<void> {
    return runOnce('tenantRetrievalConfig', force, async () => {
      const retrievalRes: any = await getTenantRetrievalConfig()
      tenantRetrievalConfig.value = retrievalRes?.data ?? null
      loadedAt.value.tenantRetrievalConfig = Date.now()
    })
  }

  async function ensureParserEngines(force = false): Promise<void> {
    return runOnce('parserEngines', force, async () => {
      const resp = await getParserEngines()
      parserEngines.value = resp?.data && Array.isArray(resp.data) ? resp.data : []
      loadedAt.value.parserEngines = Date.now()
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
      tenantRetrievalConfig.value = null
      parserEngines.value = []
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
    tenantRetrievalConfig,
    parserEngines,
    systemInfo,
    ensureTenantRetrievalConfig,
    ensureParserEngines,
    ensureSystemInfo,
    invalidate,
  }
})
