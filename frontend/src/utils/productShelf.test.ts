import assert from 'node:assert/strict'
import test from 'node:test'
import { addProductShelfSchema, isProductShelf, shelfLevel, shelfRelationLabel, shelfSection } from './productShelf'

const t = (key: string, values?: Record<string, string>) => `${key}${values?.key ? `: ${values.key}` : ''}`

// Product shelf request (small contract): adding a template edits only the settings draft.
test('shelf template is additive, repeatable, and keeps candidate parents outside the hierarchy', () => {
  const original = { entity_schema: { kinds: [{ key: 'custom', name: 'Custom', parent_kinds: [] }], fields: [] }, relation_types: [] }
  const before = structuredClone(original)
  const added = addProductShelfSchema(original, t)
  assert.deepEqual(original, before)
  assert.equal(added.entity_schema.kinds[0]?.name, 'Custom')
  assert.equal(isProductShelf(added.entity_schema), true)
  assert.deepEqual(addProductShelfSchema(added, t), added)
  assert.equal(added.relation_types.find(row => row.name === 'belongs_to')?.hierarchical, true)
  assert.equal(added.relation_types.find(row => row.name === 'candidate_parent')?.hierarchical, false)
  assert.deepEqual(added.entity_schema.kinds.find(row => row.key === 'model')?.parent_kinds, ['product'])
  assert.equal(added.entity_schema.fields.find(row => row.key === 'match_status')?.type, 'text')
  assert.deepEqual(added.entity_schema.fields.find(row => row.key === 'source_cell')?.kinds, [])
  assert.equal(shelfLevel('model'), 3)
  assert.equal(shelfLevel('configuration'), null)
  assert.equal(shelfSection('configuration'), 'attrs')
  assert.equal(shelfSection('verification_scope'), 'certs')
  assert.equal(shelfSection('market_scope'), 'market')
  assert.equal(shelfSection('credential'), 'docs')
  assert.equal(shelfRelationLabel('covers'), 'productShelf.relations.covers')
  assert.equal(shelfRelationLabel('custom'), null)
})

// Product shelf request (small failure contract): incompatible user definitions cannot be overwritten.
test('incompatible existing shelf definitions fail without a partial draft update', () => {
  const draft = addProductShelfSchema({ relation_types: [] }, t)
  draft.entity_schema.fields.find(row => row.key === 'energy_kwh')!.type = 'text'
  const before = structuredClone(draft)
  assert.throws(() => addProductShelfSchema(draft, t), /configConflict: energy_kwh/)
  assert.deepEqual(draft, before)

  const relationDraft = addProductShelfSchema({ relation_types: [] }, t)
  relationDraft.relation_types.find(row => row.name === 'candidate_parent')!.hierarchical = true
  const relationBefore = structuredClone(relationDraft)
  assert.throws(() => addProductShelfSchema(relationDraft, t), /configConflict: candidate_parent/)
  assert.deepEqual(relationDraft, relationBefore)
})
