import assert from 'node:assert/strict'
import test from 'node:test'
import { reactive } from 'vue'
import type { KnowledgeDomainInput } from '@/api/knowledge-domains'
import { copyDomainInput, emptyDomainField, emptyKnowledgeDomain, templateFieldFromOpenField, validateDomainInput } from './domainTemplate'

function validDomain(): KnowledgeDomainInput {
  return { ...emptyKnowledgeDomain(), name: 'AI 行业', entity_types: ['模型'], fields: [{ ...emptyDomainField('context_length'), aliases: ['上下文长度'], value_type: 'number', unit_family: 'tokens', entity_types: ['模型'] }], unit_families: [{ key: 'tokens', canonical_unit: 'token', units: [{ name: 'token', aliases: ['tokens'], factor: 1 }, { name: 'k tokens', aliases: ['千 token'], factor: 1000 }] }] }
}
test('domain editing copies reactive source data without changing the saved template', () => {
  const source = reactive(validDomain())
  const draft = copyDomainInput(source)
  draft.fields[0].aliases.push('context window')
  draft.unit_families[0].units[0].factor = 2
  assert.deepEqual(source.fields[0].aliases, ['上下文长度'])
  assert.equal(source.unit_families[0].units[0].factor, 1)
})
test('domain validation accepts open templates and complete unit families', () => {
  assert.equal(validateDomainInput({ ...emptyKnowledgeDomain(), name: 'Industry' }), undefined)
  assert.equal(validateDomainInput(validDomain()), undefined)
  assert.equal(validateDomainInput(emptyKnowledgeDomain()), 'nameRequired')
})
test('domain validation refuses ambiguous labels across fields and relations', () => {
  const input = validDomain()
  input.relations.push({ key: 'developed_by', label: '上下文长度', aliases: [], subject_types: [], object_types: [] })
  assert.equal(validateDomainInput(input), 'duplicateAliases')
  input.relations[0].key = 'context_length'
  assert.equal(validateDomainInput(input), 'duplicateKeys')
})
test('unit validation requires a canonical factor of one and finite positive factors', () => {
  for (const factor of [0, -1, NaN, Infinity]) {
    const input = validDomain()
    input.unit_families[0].units[1].factor = factor
    assert.equal(validateDomainInput(input), 'invalidUnit')
  }
  const input = validDomain()
  input.unit_families[0].units[0].factor = 2
  assert.equal(validateDomainInput(input), 'invalidUnit')
})
test('unit names cannot identify units in different families', () => {
  const input = validDomain()
  input.unit_families.push({ key: 'distance', canonical_unit: 'meter', units: [{ name: 'meter', aliases: ['tokens'], factor: 1 }] })
  assert.equal(validateDomainInput(input), 'invalidUnit')
})
test('fields require defined units and entity types', () => {
  const input = validDomain()
  input.fields[0].unit_family = 'missing'
  assert.equal(validateDomainInput(input), 'unknownFamily')
  input.fields[0].unit_family = 'tokens'
  input.fields[0].value_type = 'text'
  assert.equal(validateDomainInput(input), 'unknownFamily')
  input.fields[0].unit_family = ''
  input.fields[0].entity_types = ['人物']
  assert.equal(validateDomainInput(input), 'unknownEntityType')
})
test('new template fields keep the original open name as an alias without modifying the draft', () => {
  const draft = emptyDomainField('context_length')
  const field = templateFieldFromOpenField(draft, '上下文窗口')
  assert.deepEqual(field.aliases, ['上下文窗口'])
  assert.deepEqual(draft.aliases, [])
  assert.deepEqual(templateFieldFromOpenField({ ...draft, aliases: ['上下文窗口'] }, '上下文窗口').aliases, ['上下文窗口'])
  assert.deepEqual(templateFieldFromOpenField(emptyDomainField('price'), 'price').aliases, [])
})
