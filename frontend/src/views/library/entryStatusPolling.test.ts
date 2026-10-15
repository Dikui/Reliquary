import assert from 'node:assert/strict'
import test from 'node:test'
import { isEntryParseInFlight, entryNeedsStatusPolling } from './entryStatusPolling.ts'

for (const parse_status of ['pending', 'processing', 'finalizing']) {
  test(`${parse_status} entry continues polling while ingestion runs`, () => {
    assert.equal(isEntryParseInFlight(parse_status), true)
    assert.equal(entryNeedsStatusPolling({ parse_status }), true)
  })
}

for (const parse_status of ['completed', 'failed', 'cancelled', 'rejected', 'draft']) {
  test(`${parse_status} entry stops polling after processing ends`, () => {
    assert.equal(entryNeedsStatusPolling({ parse_status }), false)
  })
}
