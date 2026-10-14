import type { Entity } from '@/api/entities'
import type { Acquisition, MissingMaterial } from './ingest'

export const CONTENT_TYPES = ['event', 'opinion', 'research', 'tutorial', 'case', 'reference', 'other'] as const
export type ContentType = typeof CONTENT_TYPES[number]
export interface StructuredSummary { overview: string; points: string[]; key_terms: string[] }
export interface EntryTopic { id: string; name: string; description?: string }
export interface ClassifiedEntry {
 library_content_types?: { name: string; enabled: boolean; extract_graph: boolean; position: number }[]
 image_descriptions?: { url: string; description?: string; skip_reason?: string; ordinal: number; locations: { paragraph: number; start: number; end: number }[] }[]
  acquisition?: Acquisition | ''
  incomplete?: boolean
  external_completion_attempted?: boolean
  missing_materials?: MissingMaterial[]
  related_article_id?: string
  starred?: boolean
  source_unverified?: boolean
  source_kind?: 'article' | 'mine' | 'report'
  vault_path?: string
  content_type?: ContentType | string | null
  structured_summary?: StructuredSummary | null
  description?: string
  source?: string
  metadata?: { intake?: { decision?: string; reason?: string; source?: string; original_path?: string; source_info?: { author?: string; publisher?: string; original_url?: string; original_url_source?: string; published_at?: string } } }
  entities?: Pick<Entity, 'id' | 'canonical_name' | 'entity_type'>[]
  topics?: EntryTopic[]
}
