import assert from 'node:assert/strict'
import test from 'node:test'
import { isKnowledgeParseInFlight, knowledgeNeedsStatusPolling } from './knowledgeStatusPolling.ts'

for (const parse_status of ['pending', 'processing', 'finalizing']) {
  test(`${parse_status} knowledge continues polling while ingestion runs`, () => {
    assert.equal(isKnowledgeParseInFlight(parse_status), true)
    assert.equal(knowledgeNeedsStatusPolling({ parse_status }), true)
  })
}

test('completed knowledge stops polling once its summary is ready', () => {
  assert.equal(knowledgeNeedsStatusPolling({ parse_status: 'completed', summary_status: 'completed' }), false)
  assert.equal(knowledgeNeedsStatusPolling({ parse_status: 'completed' }), false)
  assert.equal(knowledgeNeedsStatusPolling({ parse_status: 'completed', summary_status: 'processing' }), true)
  assert.equal(knowledgeNeedsStatusPolling({ parse_status: 'completed', summary_status: 'pending' }), true)
})

test('failed knowledge stops polling even if an optional summary remains pending', () => {
  assert.equal(knowledgeNeedsStatusPolling({ parse_status: 'failed', summary_status: 'pending' }), false)
})

for (const summary_status of ['completed', 'skipped', 'failed']) {
  test(`completed knowledge treats ${summary_status} summary as a terminal state`, () => {
    assert.equal(knowledgeNeedsStatusPolling({ parse_status: 'completed', summary_status }), false)
    assert.equal(knowledgeNeedsStatusPolling({ parse_status: 'finalizing', summary_status }), true)
  })
}
