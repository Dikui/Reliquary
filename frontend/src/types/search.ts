import type { StructuredSummary } from './entryClassification'
import type { Acquisition } from './ingest'

export const SEARCH_SOURCES = ['article', 'mine', 'report'] as const
export type SearchSource = typeof SEARCH_SOURCES[number]
export interface SearchEntity { id: string; canonical_name: string; aliases?: string[] }
export interface SourceLocation { heading_path: string[]; paragraph: number; start: number; end: number; body_hash: string; mapping: string }
export interface SearchSnippet { chunk_type?: string; locations?: SourceLocation[]; chunk_id: string; content: string; chunk_index: number; score: number; match_type: string; image_path?: string }
export interface SearchEntry {
  library_id?: string
  library_name?: string
  library_directory?: string
  source_ref?: string
  identifier?: string
  has_update?: boolean
  latest_entry_id?: string
  entry_id: string
  title: string
  source: SearchSource
  published_at: string | null
  summary: StructuredSummary | null
  entities: SearchEntity[]
  snippets: SearchSnippet[]
  score: number
  vault_path: string
  original_url: string
  starred?: boolean
  cannot_be_fact?: boolean
  source_unverified?: boolean
  acquisition?: Acquisition | ''
}
export interface SearchFilters {
  published_from: string
  published_to: string
  limit: number
  starred?: boolean
}
export interface SearchRequest extends Partial<SearchFilters> { query: string; libraries?: string[] }
export interface SearchData { vector_unavailable?: boolean; rerank_status?: string; results: SearchEntry[]; excluded_missing_published_at: number }
export interface SearchOptions { entities: SearchEntity[] }
