import type { LocationQuery, LocationQueryRaw } from 'vue-router'
import { normalizeEntryFileType } from '../../utils/entrySourceLabels'
import { SEARCH_SOURCES, type SearchSource } from '../../types/search'

export interface DocumentListFilters {
  domainIds?: string
  entityIds?: string
  keyword: string
  fileType: string
  starred: '' | 'true' | 'false'
  parseStatus: string
  source: string
  sourceKind: SearchSource | ''
  startDate: string
  publishedFrom: string
  publishedTo: string
  endDate: string
}

const queryFields = {
  domainIds: 'domain_ids', entityIds: 'entity_ids', keyword: 'keyword', fileType: 'file_type', starred: 'starred',
  parseStatus: 'parse_status', source: 'source', sourceKind: 'source_kind',
  startDate: 'start_date', endDate: 'end_date', publishedFrom: 'published_from', publishedTo: 'published_to',
} as const
const fileTypes = ['pdf', 'docx', 'doc', 'pptx', 'ppt', 'epub', 'txt', 'md', 'markdown', 'url', 'manual', 'mp3', 'wav', 'm4a', 'flac', 'ogg']
const parseStatuses = ['pending', 'processing', 'completed', 'failed', 'cancelled', 'finalizing', 'draft', 'rejected']
const sources = ['web', 'rss', 'api', 'browser_extension']

const queryString = (value: unknown): string => typeof value === 'string' ? value : ''
const choice = (value: unknown, choices: readonly string[]): string => {
  const text = queryString(value)
  return choices.includes(text) ? text : ''
}
const date = (value: unknown): string => {
  const text = queryString(value)
  if (!/^\d{4}-\d{2}-\d{2}$/.test(text)) return ''
  const parsed = new Date(`${text}T00:00:00Z`)
  return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === text ? text : ''
}

export function documentListFiltersFromQuery(query: LocationQuery): DocumentListFilters {
  const domainIds = [...new Set(queryString(query.domain_ids).split(',').map(value => value.trim()).filter(Boolean))].slice(0, 100).join(',')
  const entityIds = [...new Set(queryString(query.entity_ids).split(',').map(value => value.trim()).filter(Boolean))].slice(0, 100).join(',')
  return {
    ...(domainIds ? { domainIds } : {}),
    ...(entityIds ? { entityIds } : {}),
    keyword: queryString(query.keyword).trim(),
    fileType: normalizeEntryFileType(choice(query.file_type, fileTypes)),
    starred: choice(query.starred, ['true', 'false']) as DocumentListFilters['starred'],
    parseStatus: choice(query.parse_status, parseStatuses),
    source: choice(query.source, sources),
    sourceKind: choice(query.source_kind, SEARCH_SOURCES) as DocumentListFilters['sourceKind'],
    startDate: date(query.start_date),
    endDate: date(query.end_date),
    publishedFrom: date(query.published_from), publishedTo: date(query.published_to),
  }
}

export function documentListFiltersToQuery(query: LocationQuery, filters: DocumentListFilters): LocationQueryRaw {
  const result: LocationQueryRaw = { ...query }
  for (const [field, key] of Object.entries(queryFields)) {
    const value = (filters[field as keyof DocumentListFilters] || '').trim()
    if (value) result[key] = value
    else delete result[key]
  }
  return result
}

export function documentListFilterQueriesEqual(query: LocationQuery, canonical: LocationQueryRaw): boolean {
  return Object.values(queryFields).every(key => query[key] === canonical[key])
}

export function documentListFiltersEqual(left: DocumentListFilters, right: DocumentListFilters): boolean {
  return Object.keys(queryFields).every(field => (left[field as keyof DocumentListFilters] || '') === (right[field as keyof DocumentListFilters] || ''))
}
