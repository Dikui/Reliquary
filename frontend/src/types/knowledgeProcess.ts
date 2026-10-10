/** Matches backend types.KnowledgeProcessOverrides (snake_case JSON). */

export interface ParserEngineRule {
  file_types: string[]
  engine: string
}

export interface ChunkingConfigOverride {
  chunk_size?: number
  chunk_overlap?: number
  separators?: string[]
  parser_engine_rules?: ParserEngineRule[]
  enable_parent_child?: boolean
  parent_chunk_size?: number
  child_chunk_size?: number
  strategy?: string
  token_limit?: number
  languages?: string[]
  table_metadata_instructions?: string
}

export interface KnowledgeProcessOverrides {
  parser_engine_rules?: ParserEngineRule[]
  chunking_config?: ChunkingConfigOverride
  parser_engine_overrides?: Record<string, string>
}
