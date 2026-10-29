import type { LibraryCatalogItem } from '../api/library'
export function validDirectory(value: string) { return value.length <= 100 && /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(value) }
export function validLibraryName(value: string) { return !!value.trim() && Array.from(value).length <= 255 }
export function validTypes(rows: Array<{ name: string }>) {
  const names = rows.map(row => row.name.trim())
  return names.every(name => !!name && Array.from(name).length <= 100) && new Set(names).size === names.length
}
export function librarySettingsPayload(draft: LibraryCatalogItem) {
  const chunking = draft.chunking_config ? { ...draft.chunking_config, token_limit: Number(draft.chunking_config.token_limit || 0) } : undefined
  if (chunking) fillParentChunkDefaults(chunking)
  return {
    name: draft.name.trim(), description: draft.description, color: draft.color,
    ...(draft.model_tiers ? { model_tiers: { ...draft.model_tiers } } : {}),
    ...(chunking ? { chunking_config: chunking } : {}),
    feed_enabled: draft.feed_enabled,
    content_types: draft.content_types.map((row, position) => ({ ...row, name: row.name.trim(), position })),
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

export const LIBRARY_NUMBER_RANGES = {
  chunk_size: { min: 100, max: 4000 },
  chunk_overlap: { min: 0, max: 500 },
  parent_chunk_size: { min: 512, max: 8192 },
  child_chunk_size: { min: 64, max: 2048 },
  token_limit: { min: 0, max: 8192 },
} as const
export type LibraryNumberField = keyof typeof LIBRARY_NUMBER_RANGES
export type LibraryNumberError = { reason: 'required' | 'range'; min: number; max: number }

export function fillParentChunkDefaults(config: Pick<LibraryCatalogItem['chunking_config'], 'parent_chunk_size' | 'child_chunk_size'>) {
  config.parent_chunk_size ||= 4096
  config.child_chunk_size ||= 384
}

export function libraryNumberErrors(config: LibraryCatalogItem['chunking_config']) {
  const errors: Partial<Record<LibraryNumberField, LibraryNumberError>> = {}
  for (const field of Object.keys(LIBRARY_NUMBER_RANGES) as LibraryNumberField[]) {
    if (!config.enable_parent_child && (field === 'parent_chunk_size' || field === 'child_chunk_size')) continue
    const value: unknown = config[field]
    const range = LIBRARY_NUMBER_RANGES[field]
    if (value === '' || value == null) {
      if (field !== 'token_limit') errors[field] = { reason: 'required', ...range }
    } else if (typeof value !== 'number' || !Number.isInteger(value) || value < range.min || value > range.max) {
      errors[field] = { reason: 'range', ...range }
    }
  }
  return errors
}
