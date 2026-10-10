import { ref, watch, onMounted, computed } from 'vue'
import { knowledgeSemanticSearch } from '@/api/knowledge-base'
import { useResourcesStore } from '@/stores/resources'

export interface CmdkKb {
  id: string
  name: string
  type?: string
}

export interface CmdkChunk {
  id: string
  chunk_index: number
  knowledge_id: string
  knowledge_base_id: string
  knowledge_title: string
  kb_name: string
  content: string
  matched_content?: string
  match_type: 'vector' | 'keyword' | string
  score: number
}

export interface CmdkFileGroup {
  knowledgeId: string
  kbId: string
  title: string
  kbName: string
  chunks: CmdkChunk[]
}

/** Knowledge search with debouncing and protection against stale responses. */
export function useCmdkSearch(options: {
  /** When set, scopes knowledge search to these KBs. Re-evaluated on each search. */
  lockedKbIds?: () => string[]
  /** How many knowledge chunks to keep after grouping; default no-cap. */
  chunkLimit?: number
  /** Debounce delay in ms. */
  debounceMs?: number
}) {
  const debounceMs = options.debounceMs ?? 350
  const query = ref('')
  const loading = ref(false)
  const hasSearched = ref(false)

  const knowledgeBases = ref<CmdkKb[]>([])
  const kbsLoaded = ref(false)
  let kbsLoadingPromise: Promise<void> | null = null

  const fileGroups = ref<CmdkFileGroup[]>([])
  const totalChunks = ref(0)



  const ensureKbs = async (): Promise<void> => {
    if (kbsLoaded.value) return
    if (kbsLoadingPromise) return kbsLoadingPromise
    kbsLoadingPromise = (async () => {
      try {
        const resources = useResourcesStore()
        await resources.ensureKnowledgeBases()
        const own: CmdkKb[] = resources.rawKnowledgeBases.map((kb: any) => ({
          id: String(kb.id),
          name: kb.name || '',
          type: kb.type,
        }))
        knowledgeBases.value = own
        kbsLoaded.value = true
      } catch (e) {
        console.error('[cmdk] failed to load knowledge bases', e)
      } finally {
        kbsLoadingPromise = null
      }
    })()
    return kbsLoadingPromise
  }

  // Derived: KB name lookup
  const getKbName = (kbId: string): string => {
    if (!kbId) return ''
    const kb = knowledgeBases.value.find(k => k.id === kbId)
    return kb?.name || ''
  }

  // KB name hits (client-side, works even before a remote search returns).
  const kbMatches = computed<CmdkKb[]>(() => {
    const q = query.value.trim().toLowerCase()
    if (!q) return []
    return knowledgeBases.value
      .filter(k => k.name.toLowerCase().includes(q))
      .slice(0, 4)
  })

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

    // Determine knowledge_base_ids for chunk search. A non-empty lockedKbIds
    // narrows the scope.
    const locked = options.lockedKbIds?.() || []
    const scoped = locked.length > 0
    let kbIds: string[]
    if (scoped) {
      kbIds = locked
    } else {
      await ensureKbs()
      kbIds = knowledgeBases.value.map(k => k.id)
    }

    const knowledgePromise = kbIds.length > 0
      ? knowledgeSemanticSearch({ query: q, knowledge_base_ids: kbIds })
          .then((res: any) => (res?.success && res.data ? res.data : []))
          .catch((e) => {
            console.error('[cmdk] knowledge search failed', e)
            return []
          })
      : Promise.resolve([])

    const chunks = await knowledgePromise

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
          kbName: getKbName(item.knowledge_base_id),
          chunks: [],
        })
      }
      fmap.get(kid)!.chunks.push({
        id: item.id,
        chunk_index: item.chunk_index,
        knowledge_id: kid,
        knowledge_base_id: item.knowledge_base_id || '',
        knowledge_title: item.knowledge_title || '',
        kb_name: getKbName(item.knowledge_base_id),
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

  // Watch query changes (debounced). lockedKbIds is tracked as a derived
  // string so the search re-runs when the KB scope toggles.
  const lockedSignature = () =>
    (options.lockedKbIds?.() || []).slice().sort().join(',')

  watch([query, lockedSignature], ([val]) => {
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

  onMounted(() => {
    ensureKbs()
  })

  return {
    query,
    loading,
    hasSearched,
    knowledgeBases,
    fileGroups,
    kbMatches,
    totalChunks,
    clearResults,
    ensureKbs,
  }
}
