import assert from 'node:assert/strict'
import test from 'node:test'

import {
  MODEL_IN_USE_ERROR_CODE,
  ModelInUseError,
  modelInUseErrorFromRequest,
  modelUsageBindingI18nKey,
  modelUsageLibrarySection,
  modelUsageListTruncated,
  modelUsageResourceCount,
  modelUsageResourceRoute,
  parseModelUsageDetails,
} from './model-usage'

const details = {
  libraries: [
    { id: 'library-1', name: 'Product docs', bindings: ['generation_model_tier'] },
    { id: 'library-2', name: 'Engineering', bindings: ['generation_model_tier'] },
  ],
  library_total: 2,
}

test('parses grouped model usage details without dropping multiple bindings', () => {
  assert.deepEqual(parseModelUsageDetails(details), details)
})

test('fills missing totals from the listed collections', () => {
  const { library_total, ...withoutTotals } = details
  assert.deepEqual(parseModelUsageDetails(withoutTotals), details)
  assert.equal(library_total, 2)
})

test('keeps untruncated totals when the listed collections are capped', () => {
  const parsed = parseModelUsageDetails({
    ...details,
    library_total: 80,
  })
  assert.equal(parsed?.library_total, 80)
  assert.equal(modelUsageResourceCount(parsed!.libraries, parsed!.library_total), 80)
  assert.equal(modelUsageListTruncated(parsed!.libraries, parsed!.library_total), true)
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
  assert.equal(parseModelUsageDetails({ libraries: [] }), null)
  assert.equal(parseModelUsageDetails({
    libraries: [],
  }), null)
  assert.equal(parseModelUsageDetails({
    libraries: [{ id: 'library-1', name: 'Docs', bindings: [] }],
  }), null)
  assert.equal(parseModelUsageDetails({
    libraries: [{ id: 'library-1', name: 'Docs', bindings: 'generation_model_tier' }],
  }), null)
  assert.equal(modelInUseErrorFromRequest({
    error: {
      code: MODEL_IN_USE_ERROR_CODE,
      message: 'model is used by 1 library(s)',
      details: null,
    },
  }), null)
})

test('keeps navigation and localization stable for all resource kinds', () => {
  assert.deepEqual(
    modelUsageResourceRoute('library', 'library/with slash'),
    { path: '/platform/settings', query: { section: 'library', subsection: 'models' } },
  )
  assert.equal(modelUsageBindingI18nKey('generation_model_tier'), 'modelSettings.usage.bindings.generation_model_tier')
  assert.equal(modelUsageBindingI18nKey('future_binding'), 'modelSettings.usage.bindings.unknown')
})

test('maps library bindings to the configuration section that owns them', () => {
  assert.equal(modelUsageLibrarySection(['embedding_model']), 'models')
  assert.equal(modelUsageLibrarySection(['summary_model']), 'models')
  assert.equal(modelUsageLibrarySection(['generation_model_tier']), 'models')
  assert.equal(modelUsageLibrarySection(['generation_model_tier']), 'models')
  assert.equal(modelUsageLibrarySection(['future_binding']), 'models')
  assert.equal(modelUsageLibrarySection(['summary_model', 'generation_model_tier']), 'models')
})

test('model tier bindings stay attached to the unique knowledge settings', () => {
  const details = parseModelUsageDetails({ libraries: [{ id: 'library-1', name: 'Library', bindings: ['light_model_tier', 'generation_model_tier'] }] })
  assert.equal(details?.libraries.length, 1)
  assert.deepEqual(details?.libraries[0]?.bindings, ['light_model_tier', 'generation_model_tier'])
  assert.equal(modelUsageBindingI18nKey('light_model_tier'), 'modelSettings.usage.bindings.light_model_tier')
})
