import { CONTENT_TYPES, type ContentType } from '../types/knowledgeClassification'
import { SEARCH_SOURCES, type KnowledgeSearchFilters, type KnowledgeSearchRequest, type SearchSource } from '../types/knowledgeSearch'

export function defaultSearchFilters(): KnowledgeSearchFilters {
  return { content_types: [], entity_ids: [], topic_ids: [], sources: [], published_from: '', published_to: '', collapse_similar: true, limit: 20 }
}
export function buildKnowledgeSearchRequest(query: string, filters: KnowledgeSearchFilters): KnowledgeSearchRequest {
  return {
    query: query.trim(),
    ...Object.fromEntries(['content_types', 'entity_ids', 'topic_ids', 'sources'].flatMap(key => {
      const value = filters[key as 'content_types' | 'entity_ids' | 'topic_ids' | 'sources']
      return value.length ? [[key, [...value]]] : []
    })),
    ...(filters.published_from ? { published_from: filters.published_from } : {}),
    ...(filters.published_to ? { published_to: filters.published_to } : {}),
    collapse_similar: filters.collapse_similar,
    limit: filters.limit,
  }
}
type URLValue = string | null | (string | null)[] | undefined
export function searchStateFromURL(query: Record<string, URLValue>): { query: string; filters: KnowledgeSearchFilters } {
  const first = (key: string) => typeof query[key] === 'string' ? query[key] as string : ''
  const list = (key: string) => (Array.isArray(query[key]) ? query[key] as (string | null)[] : first(key).split(',')).filter((value): value is string => !!value)
  const filters = defaultSearchFilters()
  filters.content_types = list('content_types').filter((value): value is ContentType => CONTENT_TYPES.includes(value as ContentType))
  filters.sources = list('sources').filter((value): value is SearchSource => SEARCH_SOURCES.includes(value as SearchSource))
  filters.entity_ids = list('entity_ids')
  filters.topic_ids = list('topic_ids')
  filters.published_from = first('published_from')
  filters.published_to = first('published_to')
  filters.collapse_similar = first('collapse_similar') !== 'false'
  const limit = Number(first('limit'))
  if (Number.isInteger(limit) && limit >= 1 && limit <= 100) filters.limit = limit
  return { query: first('q'), filters }
}
export function searchStateToURL(query: string, filters: KnowledgeSearchFilters): Record<string, string | string[]> {
  const request = buildKnowledgeSearchRequest(query, filters)
  const { query: text, ...rest } = request
  return { ...(text ? { q: text } : {}), ...Object.fromEntries(Object.entries(rest).map(([key, value]) => [key, Array.isArray(value) ? value : String(value)])) }
}

// Dependency messages and response bodies must never become search UI text.
export function knowledgeSearchErrorMessage(error: unknown, t: (key: string) => string): string {
  const code = knowledgeSearchErrorCode(error)
  return t(code === 2400 ? 'knowledgeSearch.unavailable' : 'knowledgeSearch.searchFailed')
}
export function knowledgeSearchErrorCode(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined
  const value = error as { error?: { code?: unknown }; code?: unknown }
  const code = value.error?.code ?? value.code
  return typeof code === 'number' && Number.isInteger(code) ? code : undefined
}
