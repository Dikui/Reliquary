export type KnowledgeContextScope = 'knowledge_base' | 'domain' | 'all'
export type KnowledgeContextScopeInput = KnowledgeContextScope
export interface KnowledgeContextScopeParams {
  scope?: KnowledgeContextScopeInput
  domain_id?: string
  knowledge_base_id?: string
}
export function normalizeKnowledgeContextScope(scope?: KnowledgeContextScopeInput): KnowledgeContextScope | undefined {
  if (scope !== undefined && scope !== 'knowledge_base' && scope !== 'domain' && scope !== 'all') {
    throw new TypeError('scope must be knowledge_base, domain or all')
  }
  return scope
}
export function knowledgeContextBasePath(kbId: string, scope?: KnowledgeContextScopeInput): string {
  normalizeKnowledgeContextScope(scope)
  return !scope || scope === 'knowledge_base'
    ? `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/context`
    : '/api/v1/knowledge-context'
}
export function knowledgeContextScopeParams(scope: KnowledgeContextScopeInput, kbId: string, domainId: string): KnowledgeContextScopeParams {
  const normalized = normalizeKnowledgeContextScope(scope)!
  if (normalized === 'domain') return { scope: normalized, domain_id: domainId }
  if (normalized === 'all') return { scope: normalized }
  return { scope: normalized, knowledge_base_id: kbId }
}
