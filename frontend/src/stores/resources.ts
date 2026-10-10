import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getKnowledgeBase } from '@/api/knowledge-base'
import { listModels, type ModelConfig } from '@/api/model'

const CACHE_TTL_MS = 60_000
type ResourceKey = 'knowledgeBase' | 'models'
export const useResourcesStore = defineStore('resources', () => {
  const knowledgeBase = ref<any>(null)
  const allModels = ref<ModelConfig[]>([])
  const loadedAt: Partial<Record<ResourceKey, number>> = {}
  const inflight = new Map<ResourceKey, Promise<any>>()
  const generations: Record<ResourceKey, number> = { knowledgeBase: 0, models: 0 }
  function isFresh(key: ResourceKey) { return !!loadedAt[key] && Date.now() - loadedAt[key]! < CACHE_TTL_MS }
  async function fetchKnowledgeBase(force = false): Promise<any> {
    if (!force && isFresh('knowledgeBase')) return knowledgeBase.value
    if (!force && inflight.has('knowledgeBase')) return inflight.get('knowledgeBase')
    const generation = ++generations.knowledgeBase
    const pending = (async () => {
      try {
        const response: any = await getKnowledgeBase()
        if (generation === generations.knowledgeBase) { knowledgeBase.value = response?.data ?? null; loadedAt.knowledgeBase = Date.now() }
        return generation === generations.knowledgeBase ? knowledgeBase.value : null
      } finally { if (generation === generations.knowledgeBase) inflight.delete('knowledgeBase') }
    })()
    inflight.set('knowledgeBase', pending)
    return pending
  }
  async function ensureModels(force = false): Promise<void> {
    if (!force && isFresh('models')) return
    if (!force && inflight.has('models')) return inflight.get('models')
    const generation = ++generations.models
    const pending = (async () => {
      try { const data = await listModels(); if (generation === generations.models) { allModels.value = Array.isArray(data) ? data : []; loadedAt.models = Date.now() } }
      finally { if (generation === generations.models) inflight.delete('models') }
    })()
    inflight.set('models', pending)
    return pending
  }
  function replaceModels(models: ModelConfig[]) { generations.models++; inflight.delete('models'); allModels.value = models; loadedAt.models = Date.now() }
  function invalidateKnowledgeBaseDetail() { generations.knowledgeBase++; delete loadedAt.knowledgeBase; inflight.delete('knowledgeBase') }
  function invalidate(...keys: ResourceKey[]) {
    for (const key of keys.length ? keys : ['knowledgeBase', 'models'] as ResourceKey[]) {
      generations[key]++; delete loadedAt[key]; inflight.delete(key)
      if (key === 'knowledgeBase') knowledgeBase.value = null
      else allModels.value = []
    }
  }
  return { knowledgeBase, allModels, isFresh, fetchKnowledgeBase, ensureModels, replaceModels, invalidateKnowledgeBaseDetail, invalidate }
})
