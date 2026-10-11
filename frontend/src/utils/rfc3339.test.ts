import assert from 'node:assert/strict'
import test from 'node:test'
import { compareRFC3339, isRFC3339After } from './rfc3339'

test('fractional checkpoints preserve before, equal and after within the same millisecond', () => {
  const checkpoint = '2026-10-11T05:20:30.123400Z'
  assert.equal(compareRFC3339('2026-10-11T05:20:30.123399Z', checkpoint), -1)
  assert.equal(compareRFC3339('2026-10-11T05:20:30.123400000Z', checkpoint), 0)
  assert.equal(compareRFC3339('2026-10-11T05:20:30.123401Z', checkpoint), 1)
  assert.equal(isRFC3339After('2026-10-11T05:20:30.123401Z', checkpoint), true)
  assert.equal(isRFC3339After(checkpoint, checkpoint), false)
  assert.equal(compareRFC3339('2026-10-11T05:20:30.1234567891Z', '2026-10-11T05:20:30.1234567890Z'), 1)
})

test('equivalent time zone offsets and differently padded fractions compare equally', () => {
  assert.equal(compareRFC3339('2026-10-11T13:20:30.1234+08:00', '2026-10-11T05:20:30.123400Z'), 0)
  assert.equal(compareRFC3339('2026-10-10T23:50:30.1234-05:30', '2026-10-11T05:20:30.123400Z'), 0)
  assert.equal(compareRFC3339('2026-10-11T05:20:30Z', '2026-10-11t05:20:30.000000z'), 0)
  assert.equal(compareRFC3339('2026-10-11T00:00:00+08:00', '2026-10-10T16:00:00Z'), 0)
})

test('whole second, day and year boundaries retain chronological order', () => {
  assert.equal(compareRFC3339('2026-10-11T05:20:31Z', '2026-10-11T05:20:30.999999Z'), 1)
  assert.equal(compareRFC3339('2026-10-11T00:00:00Z', '2026-10-10T23:59:59.999999Z'), 1)
  assert.equal(compareRFC3339('2027-01-01T00:00:00Z', '2026-12-31T23:59:59.999999Z'), 1)
  assert.equal(compareRFC3339('0099-01-01T00:00:00Z', '0100-01-01T00:00:00Z'), -1)
})

test('malformed timestamps and impossible dates are rejected without claiming they are new', () => {
  const valid = '2026-10-11T05:20:30.123400Z'
  for (const invalid of ['', 'invalid', '2026-10-11', '2026-02-29T00:00:00Z', '2026-04-31T00:00:00Z', '2026-13-01T00:00:00Z', '2026-10-11T24:00:00Z', '2026-10-11T05:60:00Z', '2026-10-11T05:20:61Z', '2026-10-11T05:20:30+24:00', '2026-10-11T05:20:30+08:60']) {
    assert.equal(compareRFC3339(invalid, valid), null, invalid)
    assert.equal(compareRFC3339(valid, invalid), null, invalid)
    assert.equal(isRFC3339After(invalid, valid), false, invalid)
  }
  assert.equal(compareRFC3339('2024-02-29T00:00:00Z', '2024-03-01T00:00:00Z'), -1)
})
