import { get, post } from '@/utils/request'
import type { SearchData, SearchOptions, SearchRequest } from '@/types/search'

import { readLibraryScope, scopedLibraryIDs } from '@/utils/libraryScope'

interface SearchEnvelope<T> { success: boolean; data: T; message?: string }
export async function searchEntries(request: SearchRequest) {
  if (request.libraries === undefined || request.libraries.length === 0) {
    const catalog = await get<SearchEnvelope<Array<{ id: string }>>>('/api/v1/libraries')
    if (!catalog.success) throw catalog
    request = { ...request, libraries: scopedLibraryIDs(request.libraries ?? readLibraryScope(), catalog.data) }
  }
  return post<SearchEnvelope<SearchData>>('/api/v1/search', request)
}
export const getSearchOptions = (libraries: string[] = readLibraryScope()) => get<SearchEnvelope<SearchOptions>>(`/api/v1/search/options${libraries.length ? `?libraries=${encodeURIComponent(libraries.join(','))}` : ''}`)
