import type { Domain } from '@/api/domains'
import type { Entity } from '@/api/entities'
import type { Acquisition } from './ingest'

export interface StructuredSummary { overview: string }
export interface ClassifiedEntry {
  summary_status?: string
  error_message?: string
  has_update?: boolean
  latest_entry_id?: string
  library_id?: string
  library_name?: string
 image_descriptions?: { url: string; description?: string; skip_reason?: string; ordinal: number; locations: { paragraph: number; start: number; end: number }[] }[]
  acquisition?: Acquisition | ''
  related_entry_id?: string
  starred?: boolean
  cannot_be_fact?: boolean
  source_unverified?: boolean
  source_kind?: 'article' | 'mine' | 'report'
  vault_path?: string
  structured_summary?: StructuredSummary | null
  description?: string
  source?: string
  metadata?: { intake?: { decision?: string; reason?: string; source?: string; original_path?: string; source_info?: { author?: string; publisher?: string; original_url?: string; original_url_source?: string; published_at?: string } } }
  domains?: Domain[]
  domains_manual?: boolean
  entities_manual?: boolean
  entities?: Pick<Entity, 'id' | 'canonical_name' | 'entity_type'>[]
}
