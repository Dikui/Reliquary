import { post } from '@/utils/request'
import type { GraphSearchRequest, GraphSearchResponse } from '@/types/graph'

export const searchGraph = (request: GraphSearchRequest, signal?: AbortSignal) =>
  post<{ success: boolean; data: GraphSearchResponse; message?: string }>('/api/v1/graph/search', request, { signal })
