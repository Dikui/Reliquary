import type { ShelfCondition, ShelfNode } from '../api/shelf'
import type { LibraryCatalogItem, ShelfOption } from '../api/library'

export const SHELF_LEVEL_KINDS = ['product_line', 'platform', 'product', 'model'] as const
export const SHELF_KINDS = [...SHELF_LEVEL_KINDS, 'part', 'document_requirement', 'certification', 'document_file'] as const
export type ShelfSection = 'attrs' | 'market' | 'certs' | 'docs'
const RELATIONS = ['belongs_to', 'composed_of', 'applies_to', 'has_certification', 'has_document', 'fulfills'] as const

export function shelfLevel(kind: string): number | null {
  const level = SHELF_LEVEL_KINDS.findIndex(value => value === kind)
  return level < 0 ? null : level
}

export function shelfSection(kind: string): ShelfSection {
  if (kind === 'certification') return 'certs'
  if (kind === 'document_file' || kind === 'document_requirement') return 'docs'
  return 'attrs'
}

export function shelfRelationLabel(name: string): string | null {
  return RELATIONS.some(value => value === name) ? `productShelf.relations.${name}` : null
}

export function isProductShelf(library?: Pick<LibraryCatalogItem, 'shelf_enabled'>): boolean {
  return library?.shelf_enabled === true
}

// Conditions keep configured module names and explicitly linked part identities.
export function shelfConditionText(
  conditions: ShelfCondition[] | null | undefined,
  modules: ShelfOption[] = [],
  composition: NonNullable<ShelfNode['shelf']>['composition'] = [],
): string {
  return (conditions || []).map(condition => `${modules.find(module => module.key === condition.module_type)?.name || condition.module_type} ∈ {${condition.part_ids.map(id => composition.flatMap(group => group.parts).find(part => part.id === id)?.canonical_name || id).join(', ')}}`).join(' · ')
}
