import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { getSpaceInfo, type SpaceInfo } from '@/api/space'
import { safeGetItem, safeSetItem } from '@/composables/preferenceStorage'

export const useSpaceStore = defineStore('space', () => {
  // Keep the existing storage key so upgrades preserve the workspace cache.
  const storageKey = 'reliquary_tenant'
  const space = ref<SpaceInfo | null>(null)
  try {
    space.value = JSON.parse(safeGetItem(storageKey) || 'null')
  } catch { /* Reload invalid cached data from the server. */ }

  const hasValidSpace = computed(() => Number(space.value?.id) > 0)

  function setSpace(value: SpaceInfo) {
    space.value = value
    safeSetItem(storageKey, JSON.stringify(value))
  }

  async function refresh(): Promise<boolean> {
    try {
      const response = await getSpaceInfo()
      if (!response.success || !response.data || !(Number(response.data.id) > 0)) return false
      setSpace(response.data)
      return true
    } catch {
      return false
    }
  }

  return { space, hasValidSpace, setSpace, refresh }
})
