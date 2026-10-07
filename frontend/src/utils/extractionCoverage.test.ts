import assert from 'node:assert/strict'
import test from 'node:test'
import { shouldShowExtractionCoverage } from './extractionCoverage'

test('a newly queued evidence snapshot does not imply complete extraction', () => {
  assert.equal(shouldShowExtractionCoverage({ extraction_coverage: 'complete', extraction_status: 'pending', processed_segments: 0, total_segments: 0 }), false)
})
test('partial failures show coverage even when no segment succeeded', () => {
  assert.equal(shouldShowExtractionCoverage({ extraction_coverage: 'partial', extraction_status: 'failed', processed_segments: 0, total_segments: 3 }), true)
})
test('finished evidence shows its extraction coverage', () => {
  assert.equal(shouldShowExtractionCoverage({ extraction_coverage: 'complete', extraction_status: 'complete', processed_segments: 1, total_segments: 1 }), true)
  assert.equal(shouldShowExtractionCoverage({ extraction_status: 'complete' }), false)
})
