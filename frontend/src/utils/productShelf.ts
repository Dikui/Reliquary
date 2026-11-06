import type { EntityField, EntitySchema, LibraryCatalogItem, LibraryRelationType } from '../api/library'

export const SHELF_LEVEL_KINDS = ['product_line', 'platform', 'product', 'model'] as const
export type ShelfSection = 'attrs' | 'market' | 'certs' | 'docs'
type Translate = (key: string, values?: Record<string, string>) => string

const SIDE_KINDS = ['configuration', 'market_scope', 'verification_scope', 'certification_task', 'credential', 'criterion', 'verification', 'source_record'] as const
const ALL_KINDS = [...SHELF_LEVEL_KINDS, ...SIDE_KINDS]
const RELATIONS = ['belongs_to', 'candidate_parent', 'configuration_of', 'sale_status', 'covers', 'evidence_for', 'has_document', 'verification_item', 'source_for'] as const

export function shelfLevel(kind: string): number | null {
  const level = SHELF_LEVEL_KINDS.findIndex(value => value === kind)
  return level < 0 ? null : level
}

export function shelfSection(kind: string): ShelfSection {
  if (kind === 'market_scope') return 'market'
  if (kind === 'credential' || kind === 'source_record') return 'docs'
  if (['verification_scope', 'certification_task', 'criterion', 'verification'].includes(kind)) return 'certs'
  return 'attrs'
}

export function shelfRelationLabel(name: string): string | null {
  return RELATIONS.some(value => value === name) ? `productShelf.relations.${name}` : null
}

export function isProductShelf(schema?: EntitySchema): boolean {
  return SHELF_LEVEL_KINDS.every(key => schema?.kinds?.some(kind => kind.key === key))
}

const sameKeys = (a: string[] = [], b: string[] = []) => a.length === b.length && a.every(key => b.includes(key))
const allows = (existing: string[] = [], required: string[] = []) => !existing.length || (required.length > 0 && required.every(key => existing.includes(key)))
const cloneField = (field: EntityField): EntityField => ({ ...field, kinds: [...(field.kinds || [])], ...(field.options ? { options: [...field.options] } : {}) })

function compatibleField(existing: EntityField, expected: EntityField): boolean {
  return existing.type === expected.type && (existing.unit || '') === (expected.unit || '') && !existing.archived
    && allows(existing.kinds, expected.kinds)
    && (!(expected.type === 'select' || expected.type === 'multiselect') || allows(existing.options, expected.options))
}

/** Adds only absent definitions. Existing incompatible keys leave the entire draft untouched. */
export function addProductShelfSchema(library: Pick<LibraryCatalogItem, 'entity_schema' | 'relation_types'>, t: Translate): { entity_schema: EntitySchema; relation_types: LibraryRelationType[] } {
  const conflict = (key: string): never => { throw new Error(t('productShelf.configConflict', { key })) }
  const kinds = (library.entity_schema?.kinds || []).map(kind => ({ ...kind, parent_kinds: [...(kind.parent_kinds || [])] }))
  const fields = (library.entity_schema?.fields || []).map(cloneField)
  const relations = (library.relation_types || []).map(relation => ({ ...relation,
    subject_kinds: [...(relation.subject_kinds || [])], object_kinds: [...(relation.object_kinds || [])], fields: (relation.fields || []).map(cloneField),
  }))
  for (const key of ALL_KINDS) {
    const level = shelfLevel(key)
    const expected = { key, name: t(`productShelf.kinds.${key}`), parent_kinds: level !== null && level > 0 ? [SHELF_LEVEL_KINDS[level - 1]!] : [] }
    const existing = kinds.filter(kind => kind.key === key)
    if (existing.length > 1 || (existing[0] && !sameKeys(existing[0].parent_kinds, expected.parent_kinds))) conflict(key)
    if (!existing.length) kinds.push(expected)
  }

  const field = (key: string, type: EntityField['type'], kinds: string[] = [], unit?: string): EntityField => ({ key, name: t(`productShelf.fields.${key}`), type, kinds, ...(unit ? { unit } : {}) })
  const commonFields = [
    field('source_file', 'text'), field('source_sheet', 'text'), field('source_cell', 'text'), field('raw_value', 'text'),
    field('source_hidden', 'boolean'), field('conditions', 'text'), field('match_status', 'text'), field('field_keys', 'list'),
  ]
  const expectedFields = [...commonFields,
    field('energy_kwh', 'number', ['model', 'configuration'], 'kWh'), field('power_kw', 'number', ['model', 'configuration'], 'kW'),
    field('voltage_v', 'number', ['model', 'configuration'], 'V'), field('mass_t', 'number', ['model', 'configuration'], 't'),
    field('size_raw', 'text', ['model', 'configuration']), field('spec_raw', 'text', ['model', 'configuration', 'verification_scope']),
    field('status_raw', 'text'), field('standard_raw', 'text', ['verification_scope', 'certification_task', 'credential', 'criterion', 'verification']),
    field('certificate_number', 'text', ['credential']), field('document_url', 'url', ['credential', 'source_record']),
    field('document_attachment', 'attachment', ['credential', 'source_record']), field('progress', 'text', ['certification_task', 'verification']),
  ]
  for (const expected of expectedFields) {
    const existing = fields.filter(value => value.key === expected.key)
    if (existing.length > 1 || (existing[0] && !compatibleField(existing[0], expected))) conflict(expected.key)
    if (!existing.length) fields.push(expected)
  }

  const definitions: Array<[typeof RELATIONS[number], string[], string[], boolean?]> = [
    ['belongs_to', ['platform', 'product', 'model'], ['product_line', 'platform', 'product'], true],
    ['candidate_parent', ['platform', 'product', 'model'], ['product_line', 'platform', 'product']],
    ['configuration_of', ['configuration'], ['model']],
    ['sale_status', ['model', 'configuration'], ['market_scope']],
    ['covers', ['verification_scope'], [...SHELF_LEVEL_KINDS, 'configuration']],
    ['evidence_for', ['certification_task', 'credential', 'verification'], ['verification_scope']],
    ['has_document', [...SHELF_LEVEL_KINDS, 'configuration', 'verification_scope', 'certification_task'], ['credential']],
    ['verification_item', ['certification_task', 'credential', 'verification'], ['criterion']],
    ['source_for', ['source_record'], [...ALL_KINDS]],
  ]
  for (const [name, subject_kinds, object_kinds, hierarchical = false] of definitions) {
    const expectedFields = [...commonFields.map(cloneField), field('status_raw', 'text')]
    const existing = relations.filter(relation => relation.name === name)
    if (existing.length > 1) conflict(name)
    const relation = existing[0]
    if (relation) {
      if (!relation.enabled || !!relation.hierarchical !== hierarchical || !sameKeys(relation.subject_kinds, subject_kinds) || !sameKeys(relation.object_kinds, object_kinds)) conflict(name)
      for (const expected of expectedFields) {
        const matches = relation.fields.filter(value => value.key === expected.key)
        if (matches.length > 1 || (matches[0] && !compatibleField(matches[0], expected))) conflict(`${name}.${expected.key}`)
        if (!matches.length) relation.fields.push(expected)
      }
    } else {
      relations.push({ name, enabled: true, position: relations.length, hierarchical, subject_kinds, object_kinds, fields: expectedFields })
    }
  }
  if (kinds.length > 100 || fields.length > 200 || relations.some(relation => relation.fields.length > 200)) throw new Error(t('productShelf.configLimit'))
  return { entity_schema: { kinds, fields }, relation_types: relations }
}
