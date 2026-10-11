import { searchErrorMessage } from '@/utils/search'
import { useI18n } from 'vue-i18n'
import { onUnmounted, ref, watch } from 'vue'
import { searchEntries } from '@/api/search'
import type { SearchEntry } from '@/types/search'

export interface CmdkChunk {
  id: string
  chunk_index: number
  entry_id: string
  knowledge_base_id: string
  entry_title: string
  content: string
  matched_content?: string
  image_path?: string
  match_type: 'vector' | 'keyword' | string
  score: number
}

export interface CmdkFileGroup {
  entryId: string
  kbId: string
  title: string
  chunks: CmdkChunk[]
  source?: SearchEntry['source']
  source_unverified?: boolean
  similarCount?: number
  imagePath?: string
}

export function articleFileGroups(articles: SearchEntry[]): CmdkFileGroup[] {
  return articles.map(article => ({
    entryId: article.entry_id, kbId: '', title: article.title, source: article.source,
    source_unverified: Boolean(article.source_unverified),
    similarCount: article.similar_reports?.length || 0,
    imagePath: article.source === 'article' ? article.snippets?.find(snippet => snippet.image_path)?.image_path : undefined,
    chunks: (article.snippets?.length ? article.snippets : [{ chunk_id: article.entry_id, chunk_index: 0, content: article.summary?.overview || '', match_type: 'article', score: article.score }]).map(snippet => ({
      id: snippet.chunk_id, chunk_index: snippet.chunk_index, entry_id: article.entry_id,
      knowledge_base_id: '', entry_title: article.title, content: snippet.content,
      match_type: snippet.match_type, score: article.score,
      image_path: 'image_path' in snippet ? snippet.image_path : undefined,
    })),
  }))
}

/** Entry search with debouncing and protection against stale responses. */
export function useCmdkSearch(options: {
  /** How many entry chunks to keep after grouping; default no-cap. */
  chunkLimit?: number
  /** Debounce delay in ms. */
  debounceMs?: number
}) {
  const { t } = useI18n()
  const searchError = ref('')
  const debounceMs = options.debounceMs ?? 350
  const query = ref('')
  const composing = ref(false)
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
    searchError.value = ''
  }

  const runSearch = async (q: string, seq: number) => {
    loading.value = true
    hasSearched.value = true

    searchError.value = ''
    const articles = await searchEntries({ query: q }).then(res => {
      if (!res?.success) throw res
      return res.data.results
    }).catch(error => {
      if (seq === requestSeq) searchError.value = searchErrorMessage(error, t)
      return []
    })

    // Stale response guard.
    if (seq !== requestSeq) return

    fileGroups.value = articleFileGroups(articles)
    totalChunks.value = articles.reduce((count, article) => count + article.snippets.length, 0)

    loading.value = false
  }

  watch([query, composing], ([val, isComposing]) => {
    requestSeq++
    if (debounceTimer) clearTimeout(debounceTimer)
    loading.value = false
    const trimmed = (val as string).trim()
    if (!trimmed) {
      requestSeq++ // cancel in-flight
      loading.value = false
      clearResults()
      return
    }
    if (isComposing) return
    debounceTimer = setTimeout(() => {
      const seq = ++requestSeq
      runSearch(trimmed, seq)
    }, debounceMs)
  })

  onUnmounted(() => { requestSeq++; if (debounceTimer) clearTimeout(debounceTimer) })

  return {
    searchError,
    retry: () => runSearch(query.value.trim(), ++requestSeq),
    query,
    composing,
    loading,
    hasSearched,
    fileGroups,
    totalChunks,
    clearResults,
  }
}
