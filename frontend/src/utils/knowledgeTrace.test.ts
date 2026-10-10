import assert from 'node:assert/strict'
import test from 'node:test'
import { knowledgeSpansPayloadHasTrace } from './knowledgeTrace'

test('only actual persisted processing traces expose the timeline', () => {
  assert.equal(knowledgeSpansPayloadHasTrace(undefined), false)
  assert.equal(knowledgeSpansPayloadHasTrace({ trace: {} }), false)
  assert.equal(knowledgeSpansPayloadHasTrace({ trace: { span_id: 'processing-span' } }), true)
  assert.equal(knowledgeSpansPayloadHasTrace({ trace: {}, current_attempt: 1 }), true)
})
