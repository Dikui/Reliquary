import { get, post } from '@/utils/request'
import type { KnowledgeSearchData, KnowledgeSearchOptions, KnowledgeSearchRequest } from '@/types/knowledgeSearch'

interface SearchEnvelope<T> { success: boolean; data: T; message?: string }
export const searchKnowledgeArticles = (request: KnowledgeSearchRequest) => post<SearchEnvelope<KnowledgeSearchData>>('/api/v1/knowledge-search', request)
export const getKnowledgeSearchOptions = () => get<SearchEnvelope<KnowledgeSearchOptions>>('/api/v1/knowledge-search/options')
