/** Matches backend types.EntryProcessOverrides (snake_case JSON). */

export interface ChunkingConfigOverride {
  chunk_size?: number
  chunk_overlap?: number
  separators?: string[]
  enable_parent_child?: boolean
  parent_chunk_size?: number
  child_chunk_size?: number
  strategy?: string
  token_limit?: number
  languages?: string[]
}

export interface EntryProcessOverrides {
  chunking_config?: ChunkingConfigOverride
}
