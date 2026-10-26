/** Browser-local preferences for the personal workspace. */
export function safeGetItem(key: string): string | null {
  try { return localStorage.getItem(key) } catch { return null }
}

export function safeSetItem(key: string, value: string): boolean {
  try {
    localStorage.setItem(key, value)
    return true
  } catch (err) {
    console.warn(`[Reliquary] failed to persist preference "${key}":`, err)
    return false
  }
}

export function safeRemoveItem(key: string): void {
  try { localStorage.removeItem(key) } catch { /* Storage may be disabled. */ }
}

export function loadPreference(suffix: string): string | null {
  return safeGetItem(`Reliquary_${suffix}`)
}

export function savePreference(suffix: string, value: string): void {
  safeSetItem(`Reliquary_${suffix}`, value)
}

// Adopt the former account's preferences before removing its cached profile.
function migrateLocalPreferences() {
  let userId = 'anon'
  let spaceId = 'none'
  try {
    const user = JSON.parse(safeGetItem('reliquary_user') || 'null')
    userId = String(user?.id || 'anon')
    spaceId = String(user?.space_id || user?.tenant_id || 'none')
    const space = JSON.parse(safeGetItem('reliquary_tenant') || 'null')
    spaceId = String(space?.id || spaceId)
  } catch { /* Keep any valid owner identity if the workspace cache is corrupt. */ }

  const sources: Record<string, string[]> = {
    theme: [`Reliquary_${userId}_theme`, 'Reliquary_anon_theme'],
    font_sans: [`Reliquary_${userId}_font_sans`, 'Reliquary_anon_font_sans'],
    font_mono: [`Reliquary_${userId}_font_mono`, 'Reliquary_anon_font_mono'],
    font_size: [`Reliquary_${userId}_font_size`, 'Reliquary_anon_font_size'],
    recent_queries: [`reliquary_cmdk_recent:${userId}:${spaceId}`],
  }
  let saved = true
  for (const [suffix, keys] of Object.entries(sources)) {
    if (loadPreference(suffix) === null) {
      const value = keys.map(safeGetItem).find(value => value !== null)
      if (value !== undefined && !safeSetItem(`Reliquary_${suffix}`, value)) {
        saved = false
        continue
      }
    }
    keys.forEach(safeRemoveItem)
  }
  if (saved) safeRemoveItem('reliquary_user')
  safeRemoveItem('reliquary_token')
  safeRemoveItem('reliquary_refresh_token')
}

migrateLocalPreferences()
