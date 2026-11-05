import assert from 'node:assert/strict'
import test from 'node:test'
import { currentNameOverrides, defaultEntityAutoMatch, entityMatchNames, normalizeEntityName } from './entityAutoMatch'

test('matching switches use distinct normalized names and protect short names by default', () => {
  assert.equal(normalizeEntityName('  ＡＢ\t Model  '), 'ab model')
  const names = entityMatchNames(['天启', 'ＡＢ', 'ab', '天启储能', 'AB model'])
  assert.deepEqual(names.map(({ key, enabled }) => [key, enabled]), [['天启', false], ['ab', false], ['天启储能', true], ['ab model', true]])
  assert.equal(defaultEntityAutoMatch('42'), false)
  assert.deepEqual(currentNameOverrides(['天启', 'ＡＢ'], { '天启': true, ab: false, removed: true }), { '天启': true, ab: false })
})
