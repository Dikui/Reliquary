import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getLibrary } from '@/api/library'
import { listModels, type ModelConfig } from '@/api/model'

const CACHE_TTL_MS = 60_000
type ResourceKey = 'library' | 'models'
export const useResourcesStore = defineStore('resources', () => {
  const library = ref<any>(null)
  const allModels = ref<ModelConfig[]>([])
  const loadedAt: Partial<Record<ResourceKey, number>> = {}
  const inflight = new Map<ResourceKey, Promise<any>>()
  const generations: Record<ResourceKey, number> = { library: 0, models: 0 }
  function isFresh(key: ResourceKey) { return !!loadedAt[key] && Date.now() - loadedAt[key]! < CACHE_TTL_MS }
  async function fetchLibrary(force = false): Promise<any> {
    if (!force && isFresh('library')) return library.value
    if (!force && inflight.has('library')) return inflight.get('library')
    const generation = ++generations.library
    const pending = (async () => {
      try {
        const response: any = await getLibrary()
        if (generation === generations.library) { library.value = response?.data ?? null; loadedAt.library = Date.now() }
        return generation === generations.library ? library.value : null
      } finally { if (generation === generations.library) inflight.delete('library') }
    })()
    inflight.set('library', pending)
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
  function invalidateLibraryDetail() { generations.library++; delete loadedAt.library; inflight.delete('library') }
  function invalidate(...keys: ResourceKey[]) {
    for (const key of keys.length ? keys : ['library', 'models'] as ResourceKey[]) {
      generations[key]++; delete loadedAt[key]; inflight.delete(key)
      if (key === 'library') library.value = null
      else allModels.value = []
    }
  }
  return { library, allModels, isFresh, fetchLibrary, ensureModels, replaceModels, invalidateLibraryDetail, invalidate }
})
