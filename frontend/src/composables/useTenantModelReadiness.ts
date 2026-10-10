import { computed, onMounted, ref, watch } from 'vue'
import { useUIStore } from '@/stores/ui'
import { useResourcesStore } from '@/stores/resources'
import {
  evaluateTenantModelReadiness,
  type TenantModelReadiness,
} from '@/utils/tenantModelReadiness'

export function useTenantModelReadiness() {
  const uiStore = useUIStore()
  const resources = useResourcesStore()
  const readiness = ref<TenantModelReadiness | null>(null)
  const loaded = ref(false)
  const loading = ref(false)

  const refresh = async (force = false) => {
    loading.value = true
    try {
      await resources.ensureModels(force)
      readiness.value = evaluateTenantModelReadiness(resources.allModels)
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

  const isReadyForDocumentKb = computed(
    () => readiness.value?.isReadyForDocumentKb ?? false,
  )


  const hasGeneration = computed(() => readiness.value?.hasGeneration ?? false)

  return {
    readiness,
    loaded,
    loading,
    refresh,
    isReadyForDocumentKb,
    hasGeneration,
  }
}
