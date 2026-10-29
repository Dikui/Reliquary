import { searchErrorMessage } from '@/utils/search'
import { useI18n } from 'vue-i18n'
import { onUnmounted, ref, watch, type Ref } from 'vue'
import { useLibraryScope } from '@/composables/useLibraryScope'
import { searchEntries } from '@/api/search'
import { listEntities, type Entity } from '@/api/entities'
import type { SourceLocation, SearchEntry } from '@/types/search'

export interface CmdkChunk {
  locations?: SourceLocation[]
  id: string
  chunk_index: number
  entry_id: string
  library_id: string
  entry_title: string
  content: string
  matched_content?: string
  image_path?: string
  match_type: 'vector' | 'keyword' | string
  score: number
}

export interface CmdkFileGroup {
  entryId: string
  libraryId: string
  libraryName?: string
  title: string
  contentType?: string
  publishedAt?: string | null
  chunks: CmdkChunk[]
  source?: SearchEntry['source']
  cannot_be_fact?: boolean
  has_update?: boolean
  latest_entry_id?: string
  source_unverified?: boolean
  acquisition?: SearchEntry['acquisition']
  similarCount?: number
  imagePath?: string
}

export function articleFileGroups(articles: SearchEntry[]): CmdkFileGroup[] {
  return articles.map(article => ({
    entryId: article.entry_id, libraryId: article.library_id || '', libraryName: article.library_name, title: article.title, source: article.source,
    contentType: article.content_type, publishedAt: article.published_at,
    cannot_be_fact: Boolean(article.cannot_be_fact || article.source === 'report'),
    source_unverified: Boolean(article.source_unverified),
    has_update: Boolean(article.has_update), latest_entry_id: article.latest_entry_id,
    acquisition: article.source === 'article' ? article.acquisition : undefined,
    similarCount: article.similar_reports?.length || 0,
    imagePath: article.source === 'article' ? article.snippets?.find(snippet => snippet.image_path)?.image_path : undefined,
    chunks: (article.snippets || []).map(snippet => ({
      id: snippet.chunk_id, chunk_index: snippet.chunk_index, entry_id: article.entry_id,
      library_id: article.library_id || '', entry_title: article.title, content: snippet.content,
      match_type: snippet.match_type, score: article.score,
      image_path: 'image_path' in snippet ? snippet.image_path : undefined,
      locations: 'locations' in snippet ? snippet.locations : undefined,
    })),
  }))
}

/** Entry search with debouncing and protection against stale responses. */
export function useCmdkSearch(options: {
  /** How many entry chunks to keep after grouping; default no-cap. */
  chunkLimit?: number
  /** Debounce delay in ms. */
  debounceMs?: number
  libraries?: Ref<Array<{ id: string }>>
}) {
  const { t } = useI18n()
  const libraryScope = useLibraryScope()
  const searchError = ref('')
  const debounceMs = options.debounceMs ?? 350
  const query = ref('')
  const composing = ref(false)
  const loading = ref(false)
  const hasSearched = ref(false)

  const fileGroups = ref<CmdkFileGroup[]>([])
  const entities = ref<Entity[]>([])
  const entityError = ref('')
  const totalChunks = ref(0)

  // Monotonic id to drop stale responses.
  let requestSeq = 0
  let debounceTimer: ReturnType<typeof setTimeout> | null = null

  const clearResults = () => {
    requestSeq++
    if (debounceTimer) clearTimeout(debounceTimer)
    debounceTimer = null
    fileGroups.value = []
    entities.value = []
    entityError.value = ''
    loading.value = false
    totalChunks.value = 0
    hasSearched.value = false
    searchError.value = ''
  }

  const runSearch = async (q: string, seq: number) => {
    loading.value = true
    hasSearched.value = true

    searchError.value = ''
    entityError.value = ''
    const scope = [...libraryScope.value]
    const ids = scope.length ? scope : options.libraries?.value.map(library => library.id) || []
    const [materials, entityResults] = await Promise.all([
      searchEntries({ query: q, libraries: scope }).then(res => {
        if (!res?.success) throw res
        return { articles: res.data.results, error: '' }
      }).catch(error => ({ articles: [] as SearchEntry[], error: searchErrorMessage(error, t) })),
      Promise.allSettled((options.libraries ? ids : []).map(async library_id => {
        const response = await listEntities({ library_id, entity: q, limit: 5 })
        if (!response.success) throw response
        return response.data
      })),
    ])

    // Stale response guard.
    if (seq !== requestSeq) return

    fileGroups.value = articleFileGroups(materials.articles)
    searchError.value = materials.error
    entities.value = entityResults.flatMap(result => result.status === 'fulfilled' ? result.value : []).slice(0, 5)
    if (entityResults.some(result => result.status === 'rejected')) entityError.value = t('commandPalette.entitySearchFailed')
    totalChunks.value = materials.articles.reduce((count, article) => count + (article.snippets?.length || 0), 0)

    loading.value = false
  }

  watch([query, composing, libraryScope, () => options.libraries?.value], ([val, isComposing]) => {
    clearResults()
    const trimmed = (val as string).trim()
    if (!trimmed) return
    if (isComposing) return
    debounceTimer = setTimeout(() => {
      const seq = ++requestSeq
      runSearch(trimmed, seq)
    }, debounceMs)
  }, { flush: 'sync' })

  onUnmounted(() => { requestSeq++; if (debounceTimer) clearTimeout(debounceTimer) })

  return {
    libraryScope,
    searchError,
    entityError,
    entities,
    retry: () => {
      if (debounceTimer) clearTimeout(debounceTimer)
      if (!query.value.trim() || composing.value) return Promise.resolve()
      return runSearch(query.value.trim(), ++requestSeq)
    },
    query,
    composing,
    loading,
    hasSearched,
    fileGroups,
    totalChunks,
    clearResults,
  }
}
