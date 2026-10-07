import assert from 'node:assert/strict'
import test from 'node:test'
import { copyModelTiers, modelTypeForTier, previewModelTiers } from './modelTiers'

test('draft model tier preview overrides only selected tiers and inherits each remaining tier', () => {
  const inherited = { light: { model_id: 'team-light', source: 'team' }, extraction: { model_id: 'domain-extraction', source: 'domain' } }
  const resolved = previewModelTiers({ light: ' kb-light ', extraction: '' }, inherited, 'knowledge_base')
  assert.deepEqual(resolved.light, { model_id: 'kb-light', source: 'knowledge_base' })
  assert.deepEqual(resolved.extraction, inherited.extraction)
  assert.deepEqual(resolved.generation, { model_id: '', source: 'unconfigured' })
  assert.equal(inherited.light.model_id, 'team-light')
})
test('clearing a saved tier selects its parent and copies preserve draft isolation', () => {
  const source = { light: 'saved' }, copy = copyModelTiers(source)
  copy.light = ''
  assert.equal(source.light, 'saved')
  assert.deepEqual(previewModelTiers(copy, { light: { model_id: 'parent', source: 'team' } }, 'domain').light, { model_id: 'parent', source: 'team' })
})
test('embedding and rerank tiers select compatible model types', () => {
  assert.equal(modelTypeForTier('embedding'), 'Embedding')
  assert.equal(modelTypeForTier('rerank'), 'Rerank')
  for (const tier of ['light', 'extraction', 'generation'] as const) assert.equal(modelTypeForTier(tier), 'KnowledgeQA')
})
