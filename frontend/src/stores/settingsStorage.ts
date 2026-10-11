import { safeRemoveItem } from '@/composables/preferenceStorage'
export const SETTINGS_STORAGE_KEY = 'Reliquary_settings'
export function cloneSettings<T>(settings: T): T { return JSON.parse(JSON.stringify(settings)) }
export function isStoredSettingsRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}
export function loadAndReconcileSettings<T extends Record<string, unknown>>(defaults: T): T {
  try {
    const raw = localStorage.getItem(SETTINGS_STORAGE_KEY)
    if (!raw) return cloneSettings(defaults)
    const parsed: unknown = JSON.parse(raw)
    if (!isStoredSettingsRecord(parsed)) throw new Error('stored value is not a settings object')
    return Object.fromEntries(Object.entries(defaults).map(([key, fallback]) => [key, parsed[key] ?? fallback])) as T
  } catch (cause) {
    console.error('[settings] Failed to read local settings, resetting to defaults:', cause)
    safeRemoveItem(SETTINGS_STORAGE_KEY)
    return cloneSettings(defaults)
  }
}
