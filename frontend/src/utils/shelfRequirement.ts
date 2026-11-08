import type { Entity } from '@/api/entities'
import type { ShelfRequirement } from '@/api/shelf'

export function requirementLabel(entity: Entity) {
  const list = (entity.attributes?.requirement as ShelfRequirement | undefined)?.list_name
  return list ? `${entity.canonical_name} · ${list}` : entity.canonical_name
}
