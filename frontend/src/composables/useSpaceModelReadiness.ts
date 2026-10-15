import { computed, onMounted, ref, watch } from 'vue'
import { useUIStore } from '@/stores/ui'
import { useResourcesStore } from '@/stores/resources'
import {
  evaluateSpaceModelReadiness,
  type SpaceModelReadiness,
} from '@/utils/spaceModelReadiness'

export function useSpaceModelReadiness() {
  const uiStore = useUIStore()
  const resources = useResourcesStore()
  const readiness = ref<SpaceModelReadiness | null>(null)
  const loaded = ref(false)
  const loading = ref(false)

  const refresh = async (force = false) => {
    loading.value = true
    try {
      await resources.ensureModels(force)
      readiness.value = evaluateSpaceModelReadiness(resources.allModels)
    } finally {
      loading.value = false
      loaded.value = true
    }
  }

  onMounted(() => {
    refresh()
  })

  watch(
    () => uiStore.showSettingsModal,
    (open, wasOpen) => {
      if (wasOpen && !open) {
        refresh(true)
      }
    },
  )

  const isReadyForDocumentLibrary = computed(
    () => readiness.value?.isReadyForDocumentLibrary ?? false,
  )


  const hasGeneration = computed(() => readiness.value?.hasGeneration ?? false)

  return {
    readiness,
    loaded,
    loading,
    refresh,
    isReadyForDocumentLibrary,
    hasGeneration,
  }
}
