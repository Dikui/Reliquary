import assert from 'node:assert/strict'
import test from 'node:test'
import {
  DEFAULT_SESSION_BUCKET_KEY,
  findSessionBucketKey,
} from './sessionSidebarSourceFilter.ts'

test('findSessionBucketKey locates session bucket', () => {
  const key = findSessionBucketKey(
    {
      web: { items: [{ id: 'a' }] },
      'api': { items: [{ id: 'b' }] },
    },
    'b',
  )
  assert.equal(key, 'api')
})
