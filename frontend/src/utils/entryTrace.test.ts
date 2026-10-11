import assert from 'node:assert/strict'
import test from 'node:test'
import { entrySpansPayloadHasTrace } from './entryTrace'

test('only actual persisted processing traces expose the timeline', () => {
  assert.equal(entrySpansPayloadHasTrace(undefined), false)
  assert.equal(entrySpansPayloadHasTrace({ trace: {} }), false)
  assert.equal(entrySpansPayloadHasTrace({ trace: { span_id: 'processing-span' } }), true)
  assert.equal(entrySpansPayloadHasTrace({ trace: {}, current_attempt: 1 }), true)
})
