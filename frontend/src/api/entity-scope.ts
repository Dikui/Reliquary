export type EntityScope = 'knowledge_base' | 'all'
export const ENTITY_TYPES = ['company', 'person', 'product', 'project', 'organization', 'other'] as const
export type EntityType = typeof ENTITY_TYPES[number]
export interface EntityScopeParams { scope?: EntityScope; knowledge_base_id?: string }
export function entityScopeParams(scope: EntityScope, kbId: string): EntityScopeParams {
  if (scope === 'knowledge_base') return { scope, knowledge_base_id: kbId }
  if (scope === 'all') return { scope }
  throw new TypeError('scope must be knowledge_base or all')
}
