export const ENTITY_TYPES = ['company', 'person', 'product', 'project', 'organization', 'other'] as const
export type EntityType = typeof ENTITY_TYPES[number]
