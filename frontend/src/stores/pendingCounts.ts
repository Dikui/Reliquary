import { onScopeDispose, ref } from 'vue'
import { defineStore } from 'pinia'
import { listPendingEntry } from '@/api/entry-review'

export const usePendingCountsStore = defineStore('pending-counts', () => {
  const total = ref<number | null>(null), loading = ref(false)
  let generation = 0, disposed = false

  async function refresh() {
    if (disposed) return
    const current = ++generation
    total.value = null
    loading.value = true
    try {
      const response = await listPendingEntry({ kind: 'intake', page: 1, page_size: 1 })
      if (!response.success || !Number.isSafeInteger(response.total) || response.total < 0) return
      if (current === generation && !disposed) total.value = response.total
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
