import assert from 'node:assert/strict'
import test from 'node:test'

import {
  MODEL_IN_USE_ERROR_CODE,
  ModelInUseError,
  modelInUseErrorFromRequest,
  modelUsageBindingI18nKey,
  modelUsageKnowledgeBaseSection,
  modelUsageListTruncated,
  modelUsageResourceCount,
  modelUsageResourceRoute,
  parseModelUsageDetails,
} from './modelUsage'

const details = {
  knowledge_bases: [
    { id: 'kb-1', name: 'Product docs', bindings: ['vlm_model'] },
    { id: 'kb-2', name: 'Engineering', bindings: ['vlm_model'] },
  ],
  knowledge_base_total: 2,
}

test('parses grouped model usage details without dropping multiple bindings', () => {
  assert.deepEqual(parseModelUsageDetails(details), details)
})

test('fills missing totals from the listed collections', () => {
  const { knowledge_base_total, ...withoutTotals } = details
  assert.deepEqual(parseModelUsageDetails(withoutTotals), details)
  assert.equal(knowledge_base_total, 2)
})

test('keeps untruncated totals when the listed collections are capped', () => {
  const parsed = parseModelUsageDetails({
    ...details,
    knowledge_base_total: 80,
  })
  assert.equal(parsed?.knowledge_base_total, 80)
  assert.equal(modelUsageResourceCount(parsed!.knowledge_bases, parsed!.knowledge_base_total), 80)
  assert.equal(modelUsageListTruncated(parsed!.knowledge_bases, parsed!.knowledge_base_total), true)
})

test('recognizes only the dedicated model-in-use code with a valid payload', () => {
  const conflict = modelInUseErrorFromRequest({
    status: 400,
    success: false,
    error: {
      code: MODEL_IN_USE_ERROR_CODE,
      message: 'backwards-compatible server message',
      details,
    },
  })

  assert.ok(conflict instanceof ModelInUseError)
  assert.deepEqual(conflict.details, details)
  assert.equal(modelInUseErrorFromRequest({ error: { code: 1000, details } }), null)
})

test('rejects malformed details instead of guessing from the server message', () => {
  assert.equal(parseModelUsageDetails({ knowledge_bases: [] }), null)
  assert.equal(parseModelUsageDetails({
    knowledge_bases: [],
  }), null)
  assert.equal(parseModelUsageDetails({
    knowledge_bases: [{ id: 'kb-1', name: 'Docs', bindings: [] }],
  }), null)
  assert.equal(parseModelUsageDetails({
    knowledge_bases: [{ id: 'kb-1', name: 'Docs', bindings: 'vlm_model' }],
  }), null)
  assert.equal(modelInUseErrorFromRequest({
    error: {
      code: MODEL_IN_USE_ERROR_CODE,
      message: 'model is used by 1 knowledge base(s)',
      details: null,
    },
  }), null)
})

test('keeps navigation and localization stable for all resource kinds', () => {
  assert.deepEqual(
    modelUsageResourceRoute('knowledge_base', 'kb/with slash'),
    { path: '/platform/knowledge-bases/kb%2Fwith%20slash' },
  )
  assert.equal(modelUsageBindingI18nKey('vlm_model'), 'modelSettings.usage.bindings.vlm_model')
  assert.equal(modelUsageBindingI18nKey('future_binding'), 'modelSettings.usage.bindings.unknown')
})

test('maps knowledge-base bindings to the configuration section that owns them', () => {
  assert.equal(modelUsageKnowledgeBaseSection(['embedding_model']), 'models')
  assert.equal(modelUsageKnowledgeBaseSection(['summary_model']), 'models')
  assert.equal(modelUsageKnowledgeBaseSection(['image_processing_model']), 'multimodal')
  assert.equal(modelUsageKnowledgeBaseSection(['vlm_model']), 'multimodal')
  assert.equal(modelUsageKnowledgeBaseSection(['future_binding']), 'models')
  assert.equal(modelUsageKnowledgeBaseSection(['summary_model', 'vlm_model']), 'models')
})


test('tier-only model usage prevents deletion and preserves global references', () => {
  const details = parseModelUsageDetails({ knowledge_bases: [], model_tiers: [{ scope: 'global', id: '1', name: 'Global defaults', bindings: ['light_model_tier'] }, { scope: 'global', id: '2', name: 'Entity resolution', bindings: ['entity_resolution_model_tier'] }] })
  assert.equal(details?.model_tiers?.length, 2)
  assert.equal(details?.model_tiers?.[1]?.scope, 'global')
  assert.equal(modelUsageBindingI18nKey('light_model_tier'), 'modelSettings.usage.bindings.light_model_tier')
})
