export const LIBRARY_SCOPE_KEY = 'reliquary.ikb18.libraryScope'
export function readLibraryScope(): string[] {
  try { const value = JSON.parse(localStorage.getItem(LIBRARY_SCOPE_KEY) || '[]'); return Array.isArray(value) ? [...new Set(value.filter(id => typeof id === 'string' && id))] : [] } catch { return [] }
}
export function saveLibraryScope(ids: string[]) { try { localStorage.setItem(LIBRARY_SCOPE_KEY, JSON.stringify(ids)) } catch { /* Storage can be unavailable. */ } }
export function scopedLibraryIDs(ids: string[], catalog: Array<{ id: string }>): string[] {
  return ids.length ? catalog.filter(item => ids.includes(item.id)).map(item => item.id) : catalog.map(item => item.id)
}
