import { knowledgeSearchErrorMessage } from '@/utils/knowledgeSearch'
import { useI18n } from 'vue-i18n'
import { onUnmounted, ref, watch } from 'vue'
import { searchKnowledgeArticles } from '@/api/knowledge-search'
import type { KnowledgeSearchArticle } from '@/types/knowledgeSearch'

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
  source?: KnowledgeSearchArticle['source']
  similarCount?: number
}

export function articleFileGroups(articles: KnowledgeSearchArticle[]): CmdkFileGroup[] {
  return articles.map(article => ({
    knowledgeId: article.article_id, kbId: '', title: article.title, source: article.source,
    similarCount: article.similar_reports?.length || 0,
    chunks: (article.snippets?.length ? article.snippets : [{ chunk_id: article.article_id, chunk_index: 0, content: article.summary?.overview || '', match_type: 'article', score: article.score }]).map(snippet => ({
      id: snippet.chunk_id, chunk_index: snippet.chunk_index, knowledge_id: article.article_id,
      knowledge_base_id: '', knowledge_title: article.title, content: snippet.content,
      match_type: snippet.match_type, score: article.score,
    })),
  }))
}

/** Knowledge search with debouncing and protection against stale responses. */
export function useCmdkSearch(options: {
  /** How many knowledge chunks to keep after grouping; default no-cap. */
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
    const articles = await searchKnowledgeArticles({ query: q }).then(res => {
      if (!res?.success) throw res
      return res.data.results
    }).catch(error => {
      if (seq === requestSeq) searchError.value = knowledgeSearchErrorMessage(error, t)
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
