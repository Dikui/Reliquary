import type { SearchEntity, SearchEntry, SourceLocation } from './search'
import type { Acquisition } from './ingest'

export interface GraphSearchRequest {
  libraries: string[]
  entity: { id: string } | { name: string }
  target?: { id: string } | { name: string }
  mode: 'neighbors' | 'path'
  hops: number
  relation_types: string[]
  limit: number
}
export interface GraphSource {
  entry_id: string
  title: string
  library_id: string
  library_name: string
  library_directory: string
  published_at: string | null
  source_unverified: boolean
  acquisition: Acquisition | ''
  evidence: Array<{
    locations: SourceLocation[]
    speaker?: { entity_id?: string; name: string; selection?: string }
  }>
}
export interface GraphSearchEdge {
  id: string
  subject_id: string
  object_id: string
  library_id: string
  relation_type: string
  version: number
  sources: GraphSource[]
  source_count: number
}
export interface GraphSearchResponse {
  status: 'ok' | 'needs_selection'
  libraries: string[]
  mode: 'neighbors' | 'path'
  hops: number
  expansion_limit: number
  candidates: Record<string, SearchEntity[]>
  entities: SearchEntity[]
  edges: GraphSearchEdge[]
  paths: Array<{ entity_ids: string[]; relation_ids: string[] }>
  documents: SearchEntry[]
  truncated: boolean
  truncations: Array<{ hop?: number; entity_id?: string; reason: string }>
  pending_sync: { global: number; involved: number }
}
