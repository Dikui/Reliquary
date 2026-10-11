import assert from 'node:assert/strict'
import test from 'node:test'
import { isKnowledgeParseInFlight, knowledgeNeedsStatusPolling } from './knowledgeStatusPolling.ts'

for (const parse_status of ['pending', 'processing', 'finalizing']) {
  test(`${parse_status} knowledge continues polling while ingestion runs`, () => {
    assert.equal(isKnowledgeParseInFlight(parse_status), true)
    assert.equal(knowledgeNeedsStatusPolling({ parse_status }), true)
  })
}

for (const parse_status of ['completed', 'failed', 'cancelled', 'rejected', 'draft']) {
  test(`${parse_status} knowledge stops polling after processing ends`, () => {
    assert.equal(knowledgeNeedsStatusPolling({ parse_status }), false)
  })
}
