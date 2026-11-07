import assert from 'node:assert/strict'
import test from 'node:test'
import { isProductShelf, shelfLevel, shelfRelationLabel, shelfSection } from './productShelf'

// Small spec, task 02 / ADR 0023: only the persisted library flag enables a shelf.
test('shelf detection uses the explicit flag rather than coincidental kind names', () => {
  assert.equal(isProductShelf({ shelf_enabled: true }), true)
  assert.equal(isProductShelf({ shelf_enabled: false }), false)
  assert.equal(isProductShelf(undefined), false)
})

// Small spec, task 02 / ADR 0023: UI vocabulary follows the fixed shelf structure.
test('shelf records use parts, certifications and document files', () => {
  assert.equal(shelfLevel('model'), 3)
  assert.equal(shelfLevel('part'), null)
  assert.equal(shelfSection('part'), 'attrs')
  assert.equal(shelfSection('certification'), 'certs')
  assert.equal(shelfSection('document_file'), 'docs')
  assert.equal(shelfSection('document_requirement'), 'docs')
  assert.equal(shelfRelationLabel('composed_of'), 'productShelf.relations.composed_of')
  assert.equal(shelfRelationLabel('custom'), null)
})
