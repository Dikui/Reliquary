import { get, post } from '@/utils/request'
import type { SearchData, SearchOptions, SearchRequest } from '@/types/search'

interface SearchEnvelope<T> { success: boolean; data: T; message?: string }
// Until the UI offers library selection, every web search explicitly selects
// the current library catalog. Do not cache: newly created libraries participate.
export async function searchEntries(request: SearchRequest) {
  if (request.libraries === undefined) {
    const catalog = await get<SearchEnvelope<Array<{ id: string }>>>('/api/v1/libraries')
    if (!catalog.success) throw catalog
    request = { ...request, libraries: catalog.data.map(library => library.id) }
  }
  return post<SearchEnvelope<SearchData>>('/api/v1/search', request)
}
export const getSearchOptions = () => get<SearchEnvelope<SearchOptions>>('/api/v1/search/options')
