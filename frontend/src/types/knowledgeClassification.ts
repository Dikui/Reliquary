import type { Entity } from '@/api/entities'

export const CONTENT_TYPES = ['event', 'opinion', 'research', 'tutorial', 'case', 'reference'] as const
export type ContentType = typeof CONTENT_TYPES[number]
export interface StructuredSummary { overview: string; points: string[]; key_terms: string[] }
export interface KnowledgeTopic { id: string; name: string; description?: string }
export interface ClassifiedKnowledge {
  source_kind?: 'article' | 'mine' | 'report'
  vault_path?: string
  content_type?: ContentType | string | null
  processing_mode?: 'full' | 'summary_only'
  structured_summary?: StructuredSummary | null
  description?: string
  source?: string
  metadata?: { intake?: { source_info?: { original_url?: string } } }
  entities?: Pick<Entity, 'id' | 'canonical_name' | 'entity_type'>[]
  topics?: KnowledgeTopic[]
}
