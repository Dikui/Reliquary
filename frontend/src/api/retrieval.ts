import { get, put } from '@/utils/request'

// RetrievalConfig represents the global retrieval/search configuration for a space.
// Shared by the full search page and command palette.
export interface RetrievalConfig {
  embedding_top_k: number
  vector_threshold: number
  keyword_threshold: number
  rerank_top_k: number
  rerank_threshold: number
  similar_reports_threshold: number
  similar_reports_window_days: number
  rrf_k?: number
  rrf_vector_weight?: number
  rrf_keyword_weight?: number
  rrf_image_weight?: number
}

// Get space retrieval config via KV API
export function getSpaceRetrievalConfig() {
  return get('/api/v1/space/kv/retrieval-config')
}

// Update space retrieval config via KV API
export function updateSpaceRetrievalConfig(config: RetrievalConfig) {
  return put('/api/v1/space/kv/retrieval-config', config)
}
