import { onScopeDispose, ref } from 'vue'
import { defineStore } from 'pinia'
import { listPendingEntry } from '@/api/entry-review'
import { listLibraries } from '@/api/library'
import { listEntityMergeSuggestions } from '@/api/entities'

export const usePendingCountsStore = defineStore('pending-counts', () => {
  const total = ref<number | null>(null), loading = ref(false)
  let generation = 0, disposed = false

  async function countMergeSuggestions(current: number) {
    const catalog = await listLibraries()
    if (!catalog.success || !Array.isArray(catalog.data) || catalog.data.some(library => !library.id)) {
      throw new Error('Library catalogue unavailable')
    }
    const ids = new Set<string>()
    await Promise.all(catalog.data.map(async library => {
      for (let offset = 0; current === generation && !disposed; offset += 100) {
        const response = await listEntityMergeSuggestions({ library_id: library.id, status: 'pending', limit: 100, offset })
        if (!response.success || !Array.isArray(response.data) || response.data.some(item => !item.id)) {
          throw new Error('Merge suggestions unavailable')
        }
        for (const item of response.data) ids.add(item.id)
        if (response.data.length < 100) break
      }
    }))
    return ids.size
  }

  async function refresh() {
    if (disposed) return
    const current = ++generation
    total.value = null
    loading.value = true
    try {
      const [response, merges] = await Promise.all([
        listPendingEntry({ kind: 'intake', page: 1, page_size: 1 }),
        countMergeSuggestions(current),
      ])
      if (!response.success || !Number.isSafeInteger(response.total) || response.total < 0) return
      const combined = response.total + merges
      if (current === generation && !disposed && Number.isSafeInteger(combined)) total.value = combined
    } catch {
      // An unknown queue size must not be presented as zero.
      if (current === generation && !disposed) total.value = null
    } finally {
      if (current === generation && !disposed) loading.value = false
    }
  }

  onScopeDispose(() => {
    disposed = true
    generation++
    total.value = null
    loading.value = false
  })
  return { total, loading, refresh }
})
