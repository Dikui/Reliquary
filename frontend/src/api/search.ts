import { get, post } from '@/utils/request'
import type { SearchData, SearchOptions, SearchRequest } from '@/types/search'

interface SearchEnvelope<T> { success: boolean; data: T; message?: string }
export const searchEntries = (request: SearchRequest) => post<SearchEnvelope<SearchData>>('/api/v1/search', request)
export const getSearchOptions = () => get<SearchEnvelope<SearchOptions>>('/api/v1/search/options')
