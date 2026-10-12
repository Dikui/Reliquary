import { onScopeDispose, ref } from 'vue'
import { defineStore } from 'pinia'
import { listPendingEntry } from '@/api/entry-review'
import { listIncompleteArticles } from '@/api/ingest'

export const usePendingCountsStore = defineStore('pending-counts', () => {
  const total = ref<number | null>(null), loading = ref(false)
  let generation = 0, disposed = false

  async function refresh() {
    if (disposed) return
    const current = ++generation
    total.value = null
    loading.value = true
    try {
      const responses = await Promise.all([
        listPendingEntry({ kind: 'intake', page: 1, page_size: 1 }),
        listPendingEntry({ kind: 'processing', page: 1, page_size: 1 }),
        listIncompleteArticles({ page: 1, page_size: 1, include_attempted: true }),
      ])
      if (responses.some(response => !response.success || !Number.isSafeInteger(response.total) || response.total < 0)) return
      const sum = responses.reduce((value, response) => value + response.total, 0)
      if (current === generation && !disposed && Number.isSafeInteger(sum)) total.value = sum
    } catch {
      // An unknown queue size must not be presented as zero or a partial sum.
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
