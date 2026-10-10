import { get, post, put, del, postUpload, getDown } from "../../utils/request";
import type { ModelTierConfig } from '@/types/modelTiers';
import type { KnowledgeProcessOverrides } from '@/types/knowledgeProcess';
import type { KnowledgeSearchRequest } from '@/types/knowledgeSearch';

export interface IntakeSourceInfo { author?: string; publisher?: string; original_url?: string; published_at?: string }

export type KnowledgeDuplicateCode = 'duplicate_manual' | 'duplicate_file' | 'duplicate_url';
export type KnowledgeDuplicateMatchMethod = 'url' | 'file_hash' | 'content_hash' | 'simhash';

/** The method describes this intake attempt, independently of older repost sources. */
export interface KnowledgeDuplicateResponse {
  success: false;
  code: KnowledgeDuplicateCode;
  match_method?: KnowledgeDuplicateMatchMethod;
  message?: string;
  data?: { id: string; knowledge_base_id?: string; title?: string; file_name?: string };
}

export function getKnowledgeBase() {
  return get('/api/v1/knowledge-base');
}

export function updateKnowledgeBase(data: { name: string; description?: string; config?: Record<string, unknown> }) {
  return put('/api/v1/knowledge-base', data);
}

// 知识文件 API（基于具体知识库）
export function uploadKnowledgeFile(
  kbId: string,
  data: {
    file: File
    fileName?: string
    process_config?: KnowledgeProcessOverrides | string
    source_info?: IntakeSourceInfo | string
    [key: string]: any
  } = { file: new File([], '') },
  onProgress?: (progressEvent: any) => void,
) {
  const formData = new FormData();
  Object.keys(data).forEach(key => {
    const value = data[key];
    if (value === undefined) return;
    if (key === 'source_info' || key === 'process_config') {
      formData.append(key, typeof value === 'string' ? value : JSON.stringify(value));
      return;
    }
    formData.append(key, value);
  });
  return postUpload(`/api/v1/knowledge-bases/${kbId}/knowledge/file`, formData, onProgress);
}

// 从URL创建知识
export function createKnowledgeFromURL(
  kbId: string,
  data: { url: string; process_config?: KnowledgeProcessOverrides; source_info?: IntakeSourceInfo },
) {
  return post(`/api/v1/knowledge-bases/${kbId}/knowledge/url`, data);
}

// 手工创建知识
export function createManualKnowledge(
  kbId: string,
  data: {
    title: string
    content: string
    status: string
    process_config?: KnowledgeProcessOverrides
    source_info?: IntakeSourceInfo
  },
) {
  return post(`/api/v1/knowledge-bases/${kbId}/knowledge/manual`, data);
}

export function listKnowledgeFiles(
  kbId: string,
  params: {
    page: number;
    page_size: number;
    keyword?: string;
    file_type?: string;
    content_type?: string;
    parse_status?: string;
    source?: string;
    start_time?: string;
    end_time?: string;
  },
) {
  const query = new URLSearchParams();
  query.append('page', String(params.page));
  query.append('page_size', String(params.page_size));
  if (params.keyword) query.append('keyword', params.keyword);
  if (params.file_type) query.append('file_type', params.file_type);
  if (params.content_type) query.append('content_type', params.content_type);
  if (params.parse_status) query.append('parse_status', params.parse_status);
  if (params.source) query.append('source', params.source);
  if (params.start_time) query.append('start_time', params.start_time);
  if (params.end_time) query.append('end_time', params.end_time);
  const qs = query.toString();
  return get(`/api/v1/knowledge-bases/${kbId}/knowledge?${qs}`);
}

export function getKnowledgeDetails(id: string) {
  const query = new URLSearchParams();
  const qs = query.toString();
  return get(qs ? `/api/v1/knowledge/${id}?${qs}` : `/api/v1/knowledge/${id}`);
}

export function updateManualKnowledge(
  id: string,
  data: { title: string; content: string; status: string; process_config?: KnowledgeProcessOverrides },
) {
  return put(`/api/v1/knowledge/manual/${id}`, data);
}

export function reparseKnowledge(id: string, data?: { process_config?: KnowledgeProcessOverrides }) {
  return post(`/api/v1/knowledge/${id}/reparse`, data);
}

export function cancelKnowledgeParse(id: string) {
  return post(`/api/v1/knowledge/${id}/cancel-parse`);
}

export function getKnowledgeSpans(id: string, attempt?: number) {
  const qs = attempt ? `?attempt=${attempt}` : '';
  return get(`/api/v1/knowledge/${id}/spans${qs}`);
}

export function delKnowledgeDetails(id: string) {
  return del(`/api/v1/knowledge/${id}`);
}

// 批量删除（同一知识库内）。后端会校验所有 id 隶属于 kb_id 且具有编辑权限。
export function batchDeleteKnowledge(ids: string[]) {
  return post(`/api/v1/knowledge/batch-delete`, { ids });
}

export function downKnowledgeDetails(id: string) {
  return getDown(`/api/v1/knowledge/${id}/download`);
}

export function previewKnowledgeFile(id: string) {
  return getDown(`/api/v1/knowledge/${id}/preview`);
}

export function reviewKnowledge(id: string, data: { action: 'retry' | 'discard'; source_info?: IntakeSourceInfo }) {
  return post(`/api/v1/knowledge/${id}/review`, data);
}

/** @param idsQueryString - query string with ids (e.g. ids=xxx&ids=yyy) */
export function batchQueryKnowledge(idsQueryString: string) {
  return get(`/api/v1/knowledge/batch?${idsQueryString}`);
}

export const KNOWLEDGE_CHUNK_PAGE_SIZE = 25;

export function getKnowledgeDetailsCon(id: string, page: number) {
  return get(`/api/v1/chunks/${id}?page=${page}&page_size=${KNOWLEDGE_CHUNK_PAGE_SIZE}`);
}

export function updateKnowledgeMetadata(knowledgeId: string, customMetadata: Record<string, unknown>) {
  return put(`/api/v1/knowledge/${knowledgeId}`, { custom_metadata: customMetadata });
}

export function reprocessKnowledge(knowledgeId: string) {
  return post(`/api/v1/knowledge/${knowledgeId}/reprocess`, {});
}

// Get chunk by chunk_id only (new endpoint - to be added to backend)
export function getChunkByIdOnly(chunkId: string) {
  return get(`/api/v1/chunks/by-id/${chunkId}`);
}

const buildQuery = (params?: Record<string, any>) => {
  if (!params) return '';
  const query = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return;
    query.append(key, String(value));
  });
  const queryString = query.toString();
  return queryString ? `?${queryString}` : '';
};

export function searchKnowledge(
  keyword: string,
  offset = 0,
  limit = 20,
  fileTypes?: string[],
  options?: { recent?: boolean }
) {
  const query = new URLSearchParams();
  if (keyword) {
    query.set('keyword', keyword);
  }
  query.set('offset', String(offset));
  query.set('limit', String(limit));
  if (fileTypes && fileTypes.length > 0) {
    query.set('file_types', fileTypes.join(','));
  }
  if (options?.recent) query.set('recent', 'true');
  return get(`/api/v1/knowledge/search?${query.toString()}`);
}

export function knowledgeSemanticSearch(data: KnowledgeSearchRequest) {
  return post('/api/v1/knowledge-search', data);
}

export function batchReparseKnowledge(ids: string[], processConfig?: KnowledgeProcessOverrides) {
  return post(`/api/v1/knowledge/batch-reparse`, {
    ids,
    process_config: processConfig,
  });
}

export interface KnowledgeRepostSource { id: string; knowledge_id: string; original_url?: string; publisher?: string; author?: string; published_at?: string; published_at_missing?: boolean; match_method: string }
