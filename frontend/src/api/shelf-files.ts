import { get, post } from '@/utils/request'
import { listEntities, type Entity } from './entities'
import type { LibraryPatch } from './library'
import type { ShelfCondition, ShelfDocumentVersion, ShelfResponse } from './shelf'
import type { ShelfReviewChangeset, ShelfReviewOperation } from './shelf-review'

export interface ShelfFileRow { entity: Entity; document: ShelfDocumentVersion; owner?: Entity; requirement?: Entity }
export interface ShelfFileQuery { q?: string; group?: string; owner_kind?: string; parse_status?: string; include_history?: boolean; offset?: number }
export interface ShelfDocumentInput {
  name?: string; unassigned?: boolean; owner_id: string; requirement_id?: string; certificate_number?: string
  attachment_id?: string; cloud_url?: string; version_label: string; stage: 'draft' | 'final'; origin?: string; file_category?: string; conditions: ShelfCondition[]
}
const base = (library: string) => `/api/v1/libraries/${encodeURIComponent(library)}/shelf`
export const listShelfFiles = (library: string, input: ShelfFileQuery) => get<ShelfResponse<{ items: ShelfFileRow[]; total: number; truncated: boolean }>>(`${base(library)}/documents?${new URLSearchParams(Object.entries({ limit: 50, ...input }).filter(([, value]) => value !== undefined && value !== '').map(([key, value]) => [key, String(value)]))}`)
export const listShelfVersions = (id: string) => get<ShelfResponse<ShelfDocumentVersion[]>>(`/api/v1/entities/${encodeURIComponent(id)}/document-versions`)
export const shelfVersionDownload = (file: string, version: string) => `/api/v1/entities/${encodeURIComponent(file)}/document-versions/${encodeURIComponent(version)}/download`
export const writeShelf = (library: string, operations: ShelfReviewOperation[]) => post<ShelfResponse<ShelfReviewChangeset>>(`${base(library)}/changesets`, {
  source: { type: 'web' }, confirmed: true, operations, decisions: operations.map(value => ({ item_id: value.id, action: 'apply' })),
})
export const configureShelf = (library: string, updatedAt: string, patch: LibraryPatch) => writeShelf(library, [{ id: 'settings', type: 'configure_shelf', library_updated_at: updatedAt, library: patch }])

// Choices must include every page, so a valid configured identity is never hidden.
export async function shelfEntities(library: string, kind?: string) {
  const result: Entity[] = []
  for (let offset = 0; ; offset += 100) {
    const response = await listEntities({ library_id: library, kind, limit: 100, offset })
    if (!response.success) throw new Error(response.error)
    result.push(...response.data)
    if (response.data.length < 100) return result
  }
}
export function shelfWriteError(cause: unknown, fallback: string) {
  const error = cause as { message?: string; error?: { details?: ShelfReviewChangeset } }
  return [fallback, error?.message, ...error?.error?.details?.items?.flatMap(item => item.conflicts.map(value => value.message).filter(Boolean)) || []].filter(Boolean).join(' · ')
}
