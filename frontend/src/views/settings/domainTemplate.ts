import type { DomainField, KnowledgeDomainInput } from '@/api/knowledge-domains'

export const emptyDomainField = (key = ''): DomainField => ({ key, label: key, aliases: [], value_type: 'text', unit_family: '', entity_types: [] })
export const emptyKnowledgeDomain = (): KnowledgeDomainInput => ({ name: '', description: '', entity_types: [], fields: [], relations: [], unit_families: [], extraction_hints: '' })

export function copyDomainInput(domain: KnowledgeDomainInput): KnowledgeDomainInput {
  return JSON.parse(JSON.stringify({
    name: domain.name, description: domain.description || '', entity_types: domain.entity_types || [],
    fields: (domain.fields || []).map(field => ({ ...field, aliases: field.aliases || [], unit_family: field.unit_family || '', entity_types: field.entity_types || [] })),
    relations: (domain.relations || []).map(relation => ({ ...relation, aliases: relation.aliases || [], subject_types: relation.subject_types || [], object_types: relation.object_types || [] })),
    unit_families: (domain.unit_families || []).map(family => ({ ...family, units: (family.units || []).map(unit => ({ ...unit, aliases: unit.aliases || [] })) })),
    extraction_hints: domain.extraction_hints || '',
  }))
}
export type DomainValidationKey = 'nameRequired' | 'keysRequired' | 'duplicateKeys' | 'invalidUnit' | 'unknownFamily' | 'invalidType' | 'duplicateAliases' | 'unknownEntityType'
const normalized = (value: string) => value.trim().toLocaleLowerCase().replace(/\s+/g, '')
export function validateDomainInput(input: KnowledgeDomainInput): DomainValidationKey | undefined {
  if (!input.name.trim()) return 'nameRequired'
  const definitions = [...input.fields, ...input.relations]
  for (const items of [definitions, input.unit_families]) {
    if (items.some(item => !item.key.trim())) return 'keysRequired'
    if (new Set(items.map(item => normalized(item.key))).size !== items.length) return 'duplicateKeys'
  }
  const entityTypes = new Set(input.entity_types.map(normalized))
  if (input.entity_types.some(type => !type.trim()) || entityTypes.size !== input.entity_types.length) return 'unknownEntityType'
  const names = new Map<string, string>()
  for (const definition of definitions) {
    for (const name of [definition.key, definition.label, ...definition.aliases]) {
      const key = normalized(name)
      if (!key) continue
      if (names.has(key) && names.get(key) !== definition.key) return 'duplicateAliases'
      names.set(key, definition.key)
    }
  }
  const families = new Set(input.unit_families.map(family => family.key.trim()))
  for (const field of input.fields) {
    if (!['number', 'text', 'date', 'boolean'].includes(field.value_type)) return 'invalidType'
    if (field.unit_family && (field.value_type !== 'number' || !families.has(field.unit_family.trim()))) return 'unknownFamily'
    if (field.entity_types.some(type => !entityTypes.has(normalized(type)))) return 'unknownEntityType'
  }
  for (const relation of input.relations) {
    if ([...relation.subject_types, ...relation.object_types].some(type => !entityTypes.has(normalized(type)))) return 'unknownEntityType'
  }
  const unitNames = new Map<string, string>()
  for (const family of input.unit_families) {
    if (!family.canonical_unit.trim() || !family.units.length) return 'invalidUnit'
    if (family.units.some(unit => !unit.name.trim() || !Number.isFinite(unit.factor) || unit.factor <= 0)) return 'invalidUnit'
    if (!family.units.some(unit => normalized(unit.name) === normalized(family.canonical_unit) && unit.factor === 1)) return 'invalidUnit'
    for (const [index, unit] of family.units.entries()) {
      const identity = `${family.key}/${index}`
      for (const name of [unit.name, ...unit.aliases]) {
        const key = normalized(name)
        if (!key || (unitNames.has(key) && unitNames.get(key) !== identity)) return 'invalidUnit'
        unitNames.set(key, identity)
      }
    }
  }
}

export function templateFieldFromOpenField(field: DomainField, openField: string): DomainField {
  const result: DomainField = JSON.parse(JSON.stringify(field))
  if (normalized(result.key) !== normalized(openField) && !result.aliases.some(alias => normalized(alias) === normalized(openField))) result.aliases.push(openField)
  return result
}
