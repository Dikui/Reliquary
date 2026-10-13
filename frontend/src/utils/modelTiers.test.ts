import assert from 'node:assert/strict'
import test from 'node:test'
import { copyModelTiers, modelTypeForTier, previewModelTiers } from './modelTiers'

test('draft model tier preview overrides only selected tiers and inherits each remaining tier', () => {
  const inherited = { light: { model_id: 'global-light', source: 'global' }, entity_resolution: { model_id: 'global-resolution', source: 'global' } }
  const resolved = previewModelTiers({ light: ' library-light ', entity_resolution: '' }, inherited, 'library')
  assert.deepEqual(resolved.light, { model_id: 'library-light', source: 'library' })
  assert.deepEqual(resolved.entity_resolution, inherited.entity_resolution)
  assert.deepEqual(resolved.generation, { model_id: '', source: 'unconfigured' })
  assert.equal(inherited.light.model_id, 'global-light')
})
test('clearing a saved tier selects its parent and copies preserve draft isolation', () => {
  const source = { light: 'saved' }, copy = copyModelTiers(source)
  copy.light = ''
  assert.equal(source.light, 'saved')
  assert.deepEqual(previewModelTiers(copy, { light: { model_id: 'parent', source: 'global' } }, 'library').light, { model_id: 'parent', source: 'global' })
})
test('embedding and rerank tiers select compatible model types', () => {
  assert.equal(modelTypeForTier('embedding'), 'Embedding')
  assert.equal(modelTypeForTier('rerank'), 'Rerank')
  for (const tier of ['light', 'entity_resolution', 'generation'] as const) assert.equal(modelTypeForTier(tier), 'KnowledgeQA')
})
