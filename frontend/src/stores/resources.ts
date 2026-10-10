import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import { listKnowledgeBases, getKnowledgeBaseById } from '@/api/knowledge-base'
import { listModels, type ModelConfig } from '@/api/model'

/** 空间级资源缓存 TTL */
const CACHE_TTL_MS = 60_000

type ResourceKey = 'knowledgeBases' | 'models'

function isKbModelReady(kb: any): boolean {
  if (!kb.summary_model_id || kb.summary_model_id === '') return false
  const strategy = kb.indexing_strategy
  const needsEmbedding = !strategy || strategy.vector_enabled || strategy.keyword_enabled
  if (needsEmbedding && (!kb.embedding_model_id || kb.embedding_model_id === '')) return false
  return true
}

export const useResourcesStore = defineStore('resources', () => {
  const rawKnowledgeBases = ref<any[]>([])
  const allModels = ref<ModelConfig[]>([])

  const loadedAt = ref<Partial<Record<ResourceKey, number>>>({})
  const inflight = new Map<ResourceKey, Promise<void>>()
  // 列表请求复用缓存并去重并发调用。
  let kbAllInflight: Promise<any[]> | null = null
  // 代际计数：force 与非 force 并发时句柄会被后来者覆盖，旧请求结束时凭此判断
  // 自己是否仍是最新的那次，避免误清正在飞行的句柄。
  let kbAllGen = 0
  // 模型列表同样用代际挡住过期的 inflight：设置页保存后会 replaceModels，
  // 不能让保存前发出的 ensureModels 把旧列表写回来。
  let modelsGen = 0

  const kbDetailCache = new Map<string, { at: number; data: any }>()
  const kbDetailInflight = new Map<string, Promise<any | null>>()

  const validKnowledgeBases = computed(() => rawKnowledgeBases.value.filter(isKbModelReady))

  function isFresh(key: ResourceKey): boolean {
    const at = loadedAt.value[key] ?? 0
    return at > 0 && Date.now() - at < CACHE_TTL_MS
  }

  async function runOnce(key: ResourceKey, force: boolean, loader: () => Promise<void>): Promise<void> {
    if (!force && isFresh(key)) return
    const existing = inflight.get(key)
    if (existing) return existing
    const p = loader().finally(() => {
      inflight.delete(key)
    })
    inflight.set(key, p)
    return p
  }

  /**
   * 知识库列表缓存。
   */
  async function fetchKnowledgeBasesForList(
    force = false,
  ): Promise<any[]> {

    if (!force && isFresh('knowledgeBases')) {
      return rawKnowledgeBases.value
    }
    if (!force && kbAllInflight) return kbAllInflight

    const gen = ++kbAllGen
    kbAllInflight = (async () => {
      try {
        const res: any = await listKnowledgeBases()
        const data = res?.data && Array.isArray(res.data) ? res.data : []
        if (kbAllGen !== gen) return []
        rawKnowledgeBases.value = data
        loadedAt.value.knowledgeBases = Date.now()
        return data
      } finally {
        if (kbAllGen === gen) kbAllInflight = null
      }
    })()
    return kbAllInflight
  }

  async function ensureKnowledgeBases(force = false): Promise<void> {
    await fetchKnowledgeBasesForList(force)
  }

  async function ensureModels(force = false): Promise<void> {
    return runOnce('models', force, async () => {
      const gen = ++modelsGen
      const models = await listModels()
      if (gen !== modelsGen) return
      allModels.value = Array.isArray(models) ? models : []
      loadedAt.value.models = Date.now()
    })
  }

  /** 用刚拉到的列表覆盖缓存（模型增删改之后调用，避免 60s TTL 把旧窗口大小继续展示）。 */
  function replaceModels(models: ModelConfig[]) {
    modelsGen++
    inflight.delete('models')
    allModels.value = Array.isArray(models) ? models : []
    loadedAt.value.models = Date.now()
  }

  /** 单个知识库详情（侧栏 + 详情页共用，去重并发请求） */
  async function fetchKnowledgeBaseById(kbId: string, force = false): Promise<any | null> {
    if (!kbId) return null
    const cached = kbDetailCache.get(kbId)
    if (!force && cached && Date.now() - cached.at < CACHE_TTL_MS) {
      return cached.data
    }
    const existing = kbDetailInflight.get(kbId)
    if (existing) return existing

    const p = (async () => {
      try {
        const res: any = await getKnowledgeBaseById(kbId)
        const data = res?.data ?? null
        if (data) {
          kbDetailCache.set(kbId, { at: Date.now(), data })
        }
        return data
      } catch {
        return null
      } finally {
        kbDetailInflight.delete(kbId)
      }
    })()
    kbDetailInflight.set(kbId, p)
    return p
  }

  function invalidateKnowledgeBaseDetail(kbId?: string) {
    if (kbId) {
      kbDetailCache.delete(kbId)
      kbDetailInflight.delete(kbId)
    } else {
      kbDetailCache.clear()
      kbDetailInflight.clear()
    }
  }

  function invalidate(...keys: ResourceKey[]) {
    if (keys.length === 0) {
      loadedAt.value = {}
      rawKnowledgeBases.value = []
      allModels.value = []
      // 同时丢弃所有 inflight 句柄，否则失效后仍在飞行的请求会把旧数据写回缓存。
      inflight.clear()
      kbAllGen++
      kbAllInflight = null
      modelsGen++
      invalidateKnowledgeBaseDetail()
      return
    }
    keys.forEach((k) => {
      delete loadedAt.value[k]
      inflight.delete(k)
    })
    if (keys.includes('knowledgeBases')) {
      kbAllGen++
      kbAllInflight = null
      invalidateKnowledgeBaseDetail()
    }
    if (keys.includes('models')) {
      modelsGen++
    }
  }

  return {
    rawKnowledgeBases,
    validKnowledgeBases,
    allModels,
    isFresh,
    fetchKnowledgeBasesForList,
    ensureKnowledgeBases,
    ensureModels,
    replaceModels,
    fetchKnowledgeBaseById,
    invalidateKnowledgeBaseDetail,
    invalidate,
  }
})
