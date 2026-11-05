import assert from 'node:assert/strict'
import test from 'node:test'
import { formatDateTime, localDateTimeISO } from './dateTime'

// Regression #8: date-only history queries start at local midnight.
test('history dates use the viewer local midnight', () => {
  const previous = process.env.TZ
  process.env.TZ = 'Asia/Shanghai'
  try {
    assert.equal(localDateTimeISO('2026-11-06'), '2026-11-05T16:00:00.000Z')
    assert.equal(localDateTimeISO('2026-11-06T09:30'), '2026-11-06T01:30:00.000Z')
  } finally { if (previous === undefined) delete process.env.TZ; else process.env.TZ = previous }
})

test('calendar dates keep their day in negative and positive time zones', () => {
  for (const timeZone of ['America/New_York', 'Pacific/Honolulu', 'Asia/Tokyo']) {
    assert.equal(formatDateTime('2026-10-27', 'en-US', { year: 'numeric', month: '2-digit', day: '2-digit', timeZone }), '10/27/2026')
    assert.equal(formatDateTime('2026-10-27', 'zh-CN', { dateStyle: 'medium', timeZone }), '2026年10月27日')
  }
})

test('timestamps still represent instants in the requested time zone', () => {
  assert.equal(formatDateTime('2026-10-27T00:00:00Z', 'en-US', { dateStyle: 'short', timeZone: 'America/New_York' }), '10/26/26')
})

test('calendar dates omit an invented midnight and invalid values use the caller fallback', () => {
  assert.equal(formatDateTime('2026-10-27', 'en-US', { dateStyle: 'medium', timeStyle: 'short' }), 'Oct 27, 2026')
  for (const value of [undefined, '', 'invalid', '2026-02-29', '2026-13-01']) {
    assert.equal(formatDateTime(value, 'en-US', {}, 'unknown'), 'unknown')
  }
  assert.equal(formatDateTime('2028-02-29', 'en-US', { dateStyle: 'medium' }), 'Feb 29, 2028')
})
