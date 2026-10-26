import { ref } from 'vue'
import {
  loadPreference,
  savePreference,
} from './preferenceStorage'

export type ThemeMode = 'light' | 'dark' | 'system'

const THEME_KEY = 'theme'

function loadTheme(): ThemeMode {
  const v = loadPreference(THEME_KEY)
  if (v === 'light' || v === 'dark' || v === 'system') return v
  return 'light'
}

// Shared reactive state across all consumers
const currentTheme = ref<ThemeMode>(loadTheme())

let lastEffective: 'light' | 'dark' | null = null

function getSystemTheme(): 'light' | 'dark' {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function applyTheme(mode: ThemeMode) {
  const effective = mode === 'system' ? getSystemTheme() : mode
  if (lastEffective === effective) return
  lastEffective = effective
  document.documentElement.setAttribute('theme-mode', effective)
}

export function useTheme() {
  function setTheme(mode: ThemeMode): boolean {
    if (mode !== 'light' && mode !== 'dark' && mode !== 'system') return false
    currentTheme.value = mode
    savePreference(THEME_KEY, mode)
    applyTheme(mode)
    return true
  }

  return { currentTheme, setTheme }
}

/** Call once in main.ts to initialise theme and listen for OS changes. */
export function initTheme() {
  currentTheme.value = loadTheme()
  applyTheme(currentTheme.value)

  // React to OS theme changes when user chose "system"
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if (currentTheme.value === 'system') {
      applyTheme('system')
    }
  })
}
