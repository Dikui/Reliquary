export const ENTITY_TYPES = ['organization', 'person'] as const
export type EntityType = typeof ENTITY_TYPES[number]
