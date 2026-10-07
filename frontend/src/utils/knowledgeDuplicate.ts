import type { KnowledgeDuplicateCode, KnowledgeDuplicateMatchMethod } from '../api/knowledge-base';

export interface KnowledgeDuplicateNotice {
  id: string;
  knowledgeBaseId: string;
  title: string;
  inputLabel: string;
  code: KnowledgeDuplicateCode;
  matchMethod?: KnowledgeDuplicateMatchMethod;
}

export function resolveKnowledgeDuplicate(
  value: unknown,
  knowledgeBaseId = '',
  inputLabel = '',
): KnowledgeDuplicateNotice | null {
  if (!value || typeof value !== 'object') return null;
  const candidate = value as any;
  const response = candidate.response?.data || candidate;
  const code = response.code || response.error?.code;
  if (!['duplicate_manual', 'duplicate_file', 'duplicate_url'].includes(code)) return null;
  const data = response.data || {};
  const method = response.match_method;
  return {
    id: typeof data.id === 'string' ? data.id : '',
    knowledgeBaseId: data.knowledge_base_id || knowledgeBaseId,
    title: data.title || data.file_name || '',
    inputLabel,
    code,
    matchMethod: ['url', 'file_hash', 'content_hash', 'simhash'].includes(method) ? method : undefined,
  };
}

export function knowledgeDuplicateMessageKey(duplicate: KnowledgeDuplicateNotice): string {
  return duplicate.matchMethod === 'simhash'
    ? 'knowledgeBase.duplicateSimilarFound'
    : 'knowledgeBase.duplicateFound';
}
