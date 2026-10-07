import assert from 'node:assert/strict'
import test from 'node:test'
import { modelCallsFromSpan } from './modelCalls'

test('model calls render the budget attached to a processing span without counting it twice', () => {
  const calls = [{ stage: 'intake', tier: 'light', calls: 1 }]
  assert.deepEqual(modelCallsFromSpan({ output: { model_calls: calls }, metadata: { model_calls: calls } }), calls)
  assert.deepEqual(modelCallsFromSpan({ output: { model_calls: calls } }), calls)
  assert.deepEqual(modelCallsFromSpan({ output: { model_calls: [{ stage: 'bad', tier: 'light', calls: -1 }, {}] } }), [])
  assert.deepEqual(modelCallsFromSpan(), [])
})
