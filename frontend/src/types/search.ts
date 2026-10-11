import type { EntityType } from '@/api/entity-types'
import type { ContentType, EntryTopic, StructuredSummary } from './entryClassification'

export const SEARCH_SOURCES = ['article', 'mine', 'report'] as const
export type SearchSource = typeof SEARCH_SOURCES[number]
export interface SearchEntity { id: string; canonical_name: string; entity_type: EntityType; aliases?: string[] }
export interface SearchSnippet { chunk_id: string; content: string; chunk_index: number; score: number; match_type: string; image_path?: string }
export interface SearchEntry {
  entry_id: string
  title: string
  source: SearchSource
  published_at: string | null
  content_type: ContentType | string
  processing_mode: 'full' | 'summary_only'
  summary: StructuredSummary | null
  entities: SearchEntity[]
  topics: EntryTopic[]
  snippets: SearchSnippet[]
  score: number
  similar_reports: SearchEntry[]
  vault_path: string
  original_url: string
  starred?: boolean
  source_unverified?: boolean
}
export interface SearchFilters {
  content_types: ContentType[]
  entity_ids: string[]
  topic_ids: string[]
  sources: SearchSource[]
  published_from: string
  published_to: string
  collapse_similar: boolean
  limit: number
  starred?: boolean
}
export interface SearchRequest extends Partial<SearchFilters> { query: string }
export interface SearchData { results: SearchEntry[]; excluded_missing_published_at: number }
export interface SearchOptions { entities: SearchEntity[]; topics: EntryTopic[] }
