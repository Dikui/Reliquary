import { ref, watch } from 'vue'
import { knowledgeSemanticSearch } from '@/api/knowledge-base'

export interface CmdkChunk {
  id: string
  chunk_index: number
  knowledge_id: string
  knowledge_base_id: string
  knowledge_title: string
  content: string
  matched_content?: string
  match_type: 'vector' | 'keyword' | string
  score: number
}

export interface CmdkFileGroup {
  knowledgeId: string
  kbId: string
  title: string
  chunks: CmdkChunk[]
}

/** Knowledge search with debouncing and protection against stale responses. */
export function useCmdkSearch(options: {
  /** How many knowledge chunks to keep after grouping; default no-cap. */
  chunkLimit?: number
  /** Debounce delay in ms. */
  debounceMs?: number
}) {
  const debounceMs = options.debounceMs ?? 350
  const query = ref('')
  const loading = ref(false)
  const hasSearched = ref(false)

  const fileGroups = ref<CmdkFileGroup[]>([])
  const totalChunks = ref(0)

  // Monotonic id to drop stale responses.
  let requestSeq = 0
  let debounceTimer: ReturnType<typeof setTimeout> | null = null

  const clearResults = () => {
    fileGroups.value = []
    totalChunks.value = 0
    hasSearched.value = false
  }

  const runSearch = async (q: string, seq: number) => {
    loading.value = true
    hasSearched.value = true

    const chunks = await knowledgeSemanticSearch({ query: q }).then((res: any) => res?.success && res.data ? res.data : []).catch(error => { console.error('[cmdk] knowledge search failed', error); return [] })

    // Stale response guard.
    if (seq !== requestSeq) return

    // Group chunks by knowledge_id.
    const fmap = new Map<string, CmdkFileGroup>()
    for (const item of chunks as any[]) {
      const kid = item.knowledge_id || 'unknown'
      if (!fmap.has(kid)) {
        fmap.set(kid, {
          knowledgeId: kid,
          kbId: item.knowledge_base_id || '',
          title: item.knowledge_title || item.knowledge_filename || kid,
          chunks: [],
        })
      }
      fmap.get(kid)!.chunks.push({
        id: item.id,
        chunk_index: item.chunk_index,
        knowledge_id: kid,
        knowledge_base_id: item.knowledge_base_id || '',
        knowledge_title: item.knowledge_title || '',
        content: item.content || '',
        matched_content: item.matched_content,
        match_type: item.match_type || 'vector',
        score: item.score || 0,
      })
    }
    fileGroups.value = Array.from(fmap.values())
    totalChunks.value = (chunks as any[]).length

    loading.value = false
  }

  watch(query, val => {
    if (debounceTimer) clearTimeout(debounceTimer)
    const trimmed = (val as string).trim()
    if (!trimmed) {
      requestSeq++ // cancel in-flight
      loading.value = false
      clearResults()
      return
    }
    debounceTimer = setTimeout(() => {
      const seq = ++requestSeq
      runSearch(trimmed, seq)
    }, debounceMs)
  })

  return {
    query,
    loading,
    hasSearched,
    fileGroups,
    totalChunks,
    clearResults,
  }
}
