import type { LibraryCatalogItem } from '../api/library'
export const LIBRARY_TEMPLATES = ['external_timely', 'external_durable', 'own', 'generated'] as const
export function validDirectory(value: string) { return value.length <= 100 && /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(value) }
export function validLibraryName(value: string) { return !!value.trim() && Array.from(value).length <= 255 }
export function validTypes(rows: Array<{ name: string }>) {
  const names = rows.map(row => row.name.trim())
  return names.every(name => !!name && Array.from(name).length <= 100) && new Set(names).size === names.length
}
export function librarySettingsPayload(draft: LibraryCatalogItem) {
  return {
    name: draft.name.trim(), description: draft.description,
    ...(draft.model_tiers ? { model_tiers: { ...draft.model_tiers } } : {}),
    ...(draft.chunking_config ? { chunking_config: { ...draft.chunking_config } } : {}),
    feed_enabled: draft.feed_enabled,
    content_types: draft.content_types.map((row, position) => ({ ...row, name: row.name.trim(), position })),
    relation_types: draft.relation_types.map((row, position) => ({ ...row, name: row.name.trim(), position })),
  }
}
export function moveType(rows: unknown[], index: number, delta: number) {
  const target = index + delta
  if (target < 0 || target >= rows.length) return
  const [row] = rows.splice(index, 1); rows.splice(target, 0, row)
}

// Only the exact historical system default is adapted for display. Saved
// configuration and user-authored descriptions remain untouched until Save.
export function catalogDescription(library: LibraryCatalogItem, currentMediaDescription: string) {
  return library.system_role === 'media' && /^外部个人的观点：专家、分析师、KOL 的评论与\u6587\u7ae0。$/.test(library.description)
    ? currentMediaDescription : library.description
}
