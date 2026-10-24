import type { LibraryCatalogItem } from '../api/library'
export const LIBRARY_TEMPLATES = ['external_timely', 'external_durable', 'own', 'generated'] as const
export function validDirectory(value: string) { return value.length <= 100 && /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(value) }
export function validLibraryName(value: string) { return !!value.trim() && Array.from(value).length <= 255 }
export function librarySettingsPayload(draft: LibraryCatalogItem) {
  return {
    name: draft.name.trim(), description: draft.description,
    ...(draft.model_tiers ? { model_tiers: { ...draft.model_tiers } } : {}),
    ...(draft.chunking_config ? { chunking_config: { ...draft.chunking_config } } : {}),
    feed_enabled: draft.feed_enabled,
  }
}

// Only the exact historical system default is adapted for display. Saved
// configuration and user-authored descriptions remain untouched until Save.
export function catalogDescription(library: LibraryCatalogItem, currentMediaDescription: string) {
  return library.system_role === 'media' && /^外部个人的观点：专家、分析师、KOL 的评论与\u6587\u7ae0。$/.test(library.description)
    ? currentMediaDescription : library.description
}
