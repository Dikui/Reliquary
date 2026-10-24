import type { SearchFilters, SearchRequest } from '../types/search'

export function defaultSearchFilters(): SearchFilters {
  return { published_from: '', published_to: '', limit: 20 }
}
export function buildSearchRequest(query: string, filters: SearchFilters): SearchRequest {
  return {
    query: query.trim(),
    ...(filters.published_from ? { published_from: filters.published_from } : {}),
    ...(filters.published_to ? { published_to: filters.published_to } : {}),
    ...(typeof filters.starred === 'boolean' ? { starred: filters.starred } : {}),
    limit: filters.limit,
  }
}
type URLValue = string | null | (string | null)[] | undefined
export function searchStateFromURL(query: Record<string, URLValue>): { query: string; filters: SearchFilters } {
  const first = (key: string) => typeof query[key] === 'string' ? query[key] as string : ''
  const filters = defaultSearchFilters()
  filters.published_from = first('published_from')
  filters.published_to = first('published_to')
  if (first('starred') === 'true' || first('starred') === 'false') filters.starred = first('starred') === 'true'
  const limit = Number(first('limit'))
  if (Number.isInteger(limit) && limit >= 1 && limit <= 100) filters.limit = limit
  return { query: first('q'), filters }
}
export function searchStateToURL(query: string, filters: SearchFilters): Record<string, string | string[]> {
  const request = buildSearchRequest(query, filters)
  const { query: text, ...rest } = request
  return { ...(text ? { q: text } : {}), ...Object.fromEntries(Object.entries(rest).map(([key, value]) => [key, Array.isArray(value) ? value : String(value)])) }
}

// Dependency messages and response bodies must never become search UI text.
export function searchErrorMessage(error: unknown, t: (key: string) => string): string {
  const code = searchErrorCode(error)
  return t(code === 2400 ? 'knowledgeSearch.unavailable' : 'knowledgeSearch.searchFailed')
}
export function searchErrorCode(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined
  const value = error as { error?: { code?: unknown }; code?: unknown }
  const code = value.error?.code ?? value.code
  return typeof code === 'number' && Number.isInteger(code) ? code : undefined
}
