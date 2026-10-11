import type { Entity } from '@/api/entities'

export const CONTENT_TYPES = ['event', 'opinion', 'research', 'tutorial', 'case', 'reference'] as const
export type ContentType = typeof CONTENT_TYPES[number]
export interface StructuredSummary { overview: string; points: string[]; key_terms: string[] }
export interface EntryTopic { id: string; name: string; description?: string }
export interface ClassifiedEntry {
  starred?: boolean
  source_unverified?: boolean
  content_type_uncertain?: boolean
  processing_review_status?: string
  processing_review_content_type?: string
  processing_review_reason?: string
  source_kind?: 'article' | 'mine' | 'report'
  vault_path?: string
  content_type?: ContentType | string | null
  processing_mode?: 'full' | 'summary_only'
  structured_summary?: StructuredSummary | null
  description?: string
  source?: string
  metadata?: { intake?: { decision?: string; reason?: string; source?: string; original_path?: string; source_info?: { author?: string; publisher?: string; original_url?: string; original_url_source?: string; published_at?: string } } }
  entities?: Pick<Entity, 'id' | 'canonical_name' | 'entity_type'>[]
  topics?: EntryTopic[]
}
