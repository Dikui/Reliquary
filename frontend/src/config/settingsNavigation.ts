import { watch } from 'vue'
import type { Router } from 'vue-router'
import { normalizeSettingsSection } from './settingsRoute'

interface SettingsRequests {
  showSettingsModal: boolean
  settingsInitialSection: string | null
  settingsInitialSubSection: string | null
  closeSettings(): void
}

/** Bridge existing settings requests to the page so all callers use router guards. */
export function connectSettingsNavigation(router: Router, ui: SettingsRequests, target: EventTarget = window) {
  let request = 0
  const navigate = async (section = 'general', subsection?: string | null) => {
    const current = ++request
    try {
      await router.push({ path: '/platform/settings', query: {
        section: normalizeSettingsSection(section),
        ...(subsection ? { subsection } : {}),
      } })
    } finally {
      if (current === request) ui.closeSettings()
    }
  }
  const stop = watch(() => [ui.showSettingsModal, ui.settingsInitialSection, ui.settingsInitialSubSection] as const,
    ([open, section, subsection]) => { if (open) void navigate(section || 'general', subsection) }, { immediate: true })
  const onNavigate = (event: Event) => {
    const detail = (event as CustomEvent<{ section?: string; subsection?: string }>).detail
    if (typeof detail?.section === 'string') void navigate(detail.section, detail.subsection)
  }
  target.addEventListener('settings-nav', onNavigate)
  return () => { request++; stop(); target.removeEventListener('settings-nav', onNavigate) }
}
