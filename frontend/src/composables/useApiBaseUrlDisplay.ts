import { computed } from 'vue'
import { getApiBaseUrl } from '@/utils/api-base'

export function useApiBaseUrlDisplay() {
  const apiBaseUrlDisplay = computed(() => {
    const configured = getApiBaseUrl().trim().replace(/\/$/, '')
    const origin = typeof window !== 'undefined' && window.location.origin !== 'null' ? window.location.origin : ''
    return `${configured || origin}/api/v1`
  })

  return { apiBaseUrlDisplay }
}
