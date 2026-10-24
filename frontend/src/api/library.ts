import { get, post, put, del } from "../utils/request";
import type { ModelTier, ModelTierConfig } from '@/types/modelTiers';
import type { EntryProcessOverrides } from '@/types/entryProcess';
import type { SearchRequest, SearchSource } from '@/types/search';
import { getApiBaseUrl } from '@/utils/api-base';

export async function getEntryEditableBody(id: string, signal: AbortSignal) {
  const response = await fetch(`${getApiBaseUrl()}/api/v1/entries/${encodeURIComponent(id)}/body`, { signal });
  const hash = response.headers.get('ETag')?.match(/^"([a-f0-9]{64})"$/)?.[1];
  if (response.status !== 200 || !hash) throw new Error('Full Markdown body and ETag are required');
  return { content: await response.text(), bodyHash: hash };
}

export function updateEntryContent(id: string, data: { content: string; base_body_hash: string }) {
  return put<{ success: boolean; message?: string; data: { entry_id: string; status: string } }>(`/api/v1/entries/${encodeURIComponent(id)}/body`, data);
}

export type LibraryTemplate = 'external_timely' | 'external_durable' | 'own' | 'generated';
export interface LibraryContentType { name: string; position: number; enabled: boolean }
export interface LibraryCatalogItem {
  content_types: LibraryContentType[]; feed_enabled: boolean;
  id: string; directory: string; name: string; display_name: string; system_role: string;
  entry_count?: number; template: LibraryTemplate; description: string; review_criteria: string;
  default_review_criteria: string; model_tiers: ModelTierConfig; summary_model_id?: string;
  processing_model_tiers: ModelTier[];
  chunking_config: { chunk_size: number; chunk_overlap: number; separators: string[]; enable_parent_child: boolean; parent_chunk_size: number; child_chunk_size: number; strategy?: string; token_limit?: number; languages?: string[] };
  source_evidence_rule: '' | 'link_required' | 'link_or_publisher_date';
  collect_content: string; exclude_content: string; deduplication_rule: string;
  template_rules: { deduplication: string; source_evidence: string; review: boolean; process: boolean; merge_reposts: boolean; collapse_similar: boolean; fact_source: boolean };
}
export function listLibraries() { return get<{ success: boolean; data: LibraryCatalogItem[]; message?: string }>('/api/v1/libraries'); }
export const getCatalogLibrary = (id: string) => get<{ success: boolean; data: LibraryCatalogItem; message?: string }>(`/api/v1/libraries/${encodeURIComponent(id)}`)
export const createCatalogLibrary = (data: { template: LibraryTemplate; directory: string; name: string; description: string }) => post<{ success: boolean; data: LibraryCatalogItem; message?: string }>('/api/v1/libraries', data)
export const saveCatalogLibrary = (id: string, data: Record<string, unknown>) => put<{ success: boolean; message?: string }>(`/api/v1/libraries/${encodeURIComponent(id)}`, data)
export const deleteCatalogLibrary = (id: string) => del<{ success: boolean; message?: string }>(`/api/v1/libraries/${encodeURIComponent(id)}`)
export type LibraryRebuildScope = 'failed' | 'all'
export const rebuildCatalogLibrary = (id: string, scope: LibraryRebuildScope = 'failed') => post<{ success: boolean; data: { task_id: string; total: number }; message?: string }>(`/api/v1/libraries/${encodeURIComponent(id)}/rebuild`, { scope })

export interface IntakeSourceInfo { author?: string; publisher?: string; original_url?: string; original_url_source?: string; published_at?: string }

export function getLibrary() {
  return get('/api/v1/library');
}

export function updateLibrary(data: { name: string; description?: string; config?: Record<string, unknown> }) {
  return put('/api/v1/library', data);
}

export function listEntryFiles(
  libraryId: string,
  params: {
    page: number;
    page_size: number;
    libraries?: string[];
    entity_ids?: string[]; topic_ids?: string[]; published_from?: string; published_to?: string;
    keyword?: string;
    file_type?: string;
    content_type?: string;
    starred?: boolean;
    parse_status?: string;
    source?: string;
    source_kind?: SearchSource;
    start_time?: string;
    end_time?: string;
  },
) {
  const query = new URLSearchParams();
  query.append('page', String(params.page));
  query.append('page_size', String(params.page_size));
  if (params.libraries?.length) query.set('libraries', params.libraries.join(','));
  if (params.entity_ids?.length) query.set('entity_ids', params.entity_ids.join(','));
  if (params.topic_ids?.length) query.set('topic_ids', params.topic_ids.join(','));
  if (params.published_from) query.set('published_from', params.published_from);
  if (params.published_to) query.set('published_to', params.published_to);
  if (params.keyword) query.append('keyword', params.keyword);
  if (params.file_type) query.append('file_type', params.file_type);
  if (params.content_type) query.append('content_type', params.content_type);
  if (typeof params.starred === 'boolean') query.append('starred', String(params.starred));
  if (params.parse_status) query.append('parse_status', params.parse_status);
  if (params.source) query.append('source', params.source);
  if (params.source_kind) query.append('source_kind', params.source_kind);
  if (params.start_time) query.append('start_time', params.start_time);
  if (params.end_time) query.append('end_time', params.end_time);
  const qs = query.toString();
  return get(`/api/v1/entries?${qs}`);
}

export function getEntryDetails(id: string) {
  const query = new URLSearchParams();
  const qs = query.toString();
  return get(qs ? `/api/v1/entries/${id}?${qs}` : `/api/v1/entries/${id}`);
}

export function reparseEntry(id: string, data?: { process_config?: EntryProcessOverrides }) {
  return post(`/api/v1/entries/${id}/reparse`, data);
}

export function cancelEntryParse(id: string) {
  return post(`/api/v1/entries/${id}/cancel-parse`);
}

export function getEntrySpans(id: string, attempt?: number) {
  const qs = attempt ? `?attempt=${attempt}` : '';
  return get(`/api/v1/entries/${id}/spans${qs}`);
}

export function delEntryDetails(id: string) {
  return del(`/api/v1/entries/${id}`);
}

export function deleteDocument(documentId: string) {
  return del<{ success: boolean; data?: { document_id: string; status: 'deleted' }; message?: string }>(`/api/v1/documents/${encodeURIComponent(documentId)}`);
}

// 批量删除（同一库内）。后端会校验所有 id 隶属于 library_id 且具有编辑权限。
export function batchDeleteEntry(ids: string[]) {
  return post(`/api/v1/entries/batch-delete`, { ids });
}



export function reviewEntry(id: string, data: { action: 'approve' | 'reject' | 'retry' | 'discard'; source_info?: IntakeSourceInfo }) {
  return post(`/api/v1/entries/${id}/review`, data);
}

/** @param idsQueryString - query string with ids (e.g. ids=xxx&ids=yyy) */
export function batchQueryEntry(idsQueryString: string) {
  return get(`/api/v1/entries/batch?${idsQueryString}`);
}

export const ENTRY_CHUNK_PAGE_SIZE = 25;

export function getEntryDetailsCon(id: string, page: number) {
  return get(`/api/v1/chunks/${id}?page=${page}&page_size=${ENTRY_CHUNK_PAGE_SIZE}`);
}

export function updateEntryMetadata(entryId: string, customMetadata: Record<string, unknown>) {
  return put(`/api/v1/entries/${entryId}`, { custom_metadata: customMetadata });
}

/** Compatibility name for the shared processing and resume action. */
export function reprocessEntry(entryId: string) {
  return reparseEntry(entryId);
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

export function searchEntry(
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
  return get(`/api/v1/entries/search?${query.toString()}`);
}

export function semanticSearch(data: SearchRequest) {
  return post('/api/v1/search', data);
}

export function batchReparseEntry(ids: string[], processConfig?: EntryProcessOverrides) {
  return post(`/api/v1/entries/batch-reparse`, {
    ids,
    process_config: processConfig,
  });
}

export interface EntryRepostSource { id: string; entry_id: string; original_url?: string; publisher?: string; author?: string; published_at?: string; published_at_missing?: boolean; match_method: string }
