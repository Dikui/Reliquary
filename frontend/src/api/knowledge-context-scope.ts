export type KnowledgeContextScope = 'knowledge_base' | 'domain' | 'team'
export interface KnowledgeContextScopeParams {
  scope?: KnowledgeContextScope
  domain_id?: string
  knowledge_base_id?: string
}
export function knowledgeContextBasePath(kbId: string, scope?: KnowledgeContextScope): string {
  return !scope || scope === 'knowledge_base'
    ? `/api/v1/knowledge-bases/${encodeURIComponent(kbId)}/context`
    : '/api/v1/knowledge-context'
}
export function knowledgeContextScopeParams(scope: KnowledgeContextScope, kbId: string, domainId: string): KnowledgeContextScopeParams {
  if (scope === 'domain') return { scope, domain_id: domainId }
  if (scope === 'team') return { scope }
  return { scope, knowledge_base_id: kbId }
}
