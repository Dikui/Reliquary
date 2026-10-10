import type { EntityType } from '@/api/entity-types'
import type { ContentType, KnowledgeTopic, StructuredSummary } from './knowledgeClassification'

export const SEARCH_SOURCES = ['article', 'mine', 'report'] as const
export type SearchSource = typeof SEARCH_SOURCES[number]
export interface KnowledgeSearchEntity { id: string; canonical_name: string; entity_type: EntityType; aliases?: string[] }
export interface KnowledgeSearchSnippet { chunk_id: string; content: string; chunk_index: number; score: number; match_type: string }
export interface KnowledgeSearchArticle {
  article_id: string
  title: string
  source: SearchSource
  published_at: string | null
  content_type: ContentType | string
  processing_mode: 'full' | 'summary_only'
  summary: StructuredSummary | null
  entities: KnowledgeSearchEntity[]
  topics: KnowledgeTopic[]
  snippets: KnowledgeSearchSnippet[]
  score: number
  similar_reports: KnowledgeSearchArticle[]
  vault_path: string
  original_url: string
}
export interface KnowledgeSearchFilters {
  content_types: ContentType[]
  entity_ids: string[]
  topic_ids: string[]
  sources: SearchSource[]
  published_from: string
  published_to: string
  collapse_similar: boolean
  limit: number
}
export interface KnowledgeSearchRequest extends Partial<KnowledgeSearchFilters> { query: string }
export interface KnowledgeSearchData { results: KnowledgeSearchArticle[]; excluded_missing_published_at: number }
export interface KnowledgeSearchOptions { entities: KnowledgeSearchEntity[]; topics: KnowledgeTopic[] }
