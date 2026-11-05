import type { SearchFilters, SearchRequest } from '../types/search'

export function defaultSearchFilters(): SearchFilters {
  return { content_types: [], entity_ids: [], topic_ids: [], published_from: '', published_to: '', collapse_similar: true, limit: 20 }
}
export function buildSearchRequest(query: string, filters: SearchFilters): SearchRequest {
  return {
    query: query.trim(),
    ...Object.fromEntries(['content_types', 'entity_ids', 'topic_ids'].flatMap(key => {
      const value = filters[key as 'content_types' | 'entity_ids' | 'topic_ids']
      return value.length ? [[key, [...value]]] : []
    })),
    ...(filters.published_from ? { published_from: filters.published_from } : {}),
    ...(filters.published_to ? { published_to: filters.published_to } : {}),
    ...(typeof filters.starred === 'boolean' ? { starred: filters.starred } : {}),
    ...(filters.include_descendants ? { include_descendants: true } : {}),
    collapse_similar: filters.collapse_similar,
    limit: filters.limit,
  }
}
type URLValue = string | null | (string | null)[] | undefined
export function searchStateFromURL(query: Record<string, URLValue>): { query: string; filters: SearchFilters } {
  const first = (key: string) => typeof query[key] === 'string' ? query[key] as string : ''
  const list = (key: string) => (Array.isArray(query[key]) ? query[key] as (string | null)[] : first(key).split(',')).filter((value): value is string => !!value)
  const filters = defaultSearchFilters()
  filters.content_types = list('content_types').filter(value => value.trim() && [...value].length <= 100)
  filters.entity_ids = list('entity_ids')
  filters.topic_ids = list('topic_ids')
  if (first('include_descendants') === 'true') filters.include_descendants = true
  filters.published_from = first('published_from')
  filters.published_to = first('published_to')
  if (first('starred') === 'true' || first('starred') === 'false') filters.starred = first('starred') === 'true'
  filters.collapse_similar = first('collapse_similar') !== 'false'
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
  return t(code === 2400 ? 'search.unavailable' : 'search.searchFailed')
}
export function searchErrorCode(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined
  const value = error as { error?: { code?: unknown }; code?: unknown }
  const code = value.error?.code ?? value.code
  return typeof code === 'number' && Number.isInteger(code) ? code : undefined
}
