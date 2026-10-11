import { defineStore } from 'pinia'
import { safeSetItem } from '@/composables/preferenceStorage'
import { loadAndReconcileSettings, SETTINGS_STORAGE_KEY } from '@/stores/settingsStorage'
interface OllamaConfig { baseUrl: string; enabled: boolean }
const defaultSettings = { ollamaConfig: { baseUrl: 'http://localhost:11434', enabled: true } as OllamaConfig }
export const useSettingsStore = defineStore('settings', {
  state: () => ({ settings: loadAndReconcileSettings(defaultSettings) }),
  actions: {
    updateOllamaConfig(config: Partial<OllamaConfig>) {
      this.settings.ollamaConfig = { ...this.settings.ollamaConfig, ...config }
      safeSetItem(SETTINGS_STORAGE_KEY, JSON.stringify(this.settings))
    },
  },
})
