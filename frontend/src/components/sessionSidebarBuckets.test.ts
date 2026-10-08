import assert from 'node:assert/strict'
import test from 'node:test'

import {
  applyBucketCountProbe,
  bucketVisible,
  buildBucketDefinitions,
  createEmptyBucket,
  prependSessionToWebBucket,
  type BucketDefinition,
} from './sessionSidebarBuckets.ts'

test('prependSessionToWebBucket inserts new session at front and bumps total', () => {
  const webDef: BucketDefinition = {
    key: 'web',
    apiSource: 'web',
    label: 'Chats',
    kind: 'web',
  }
  const bucket = { ...createEmptyBucket(webDef), total: 2, items: [{ id: 'a' }] }
  const next = prependSessionToWebBucket(bucket, { id: 'b', title: 'New' })
  assert.deepEqual(next.items.map((s) => s.id), ['b', 'a'])
  assert.equal(next.total, 3)
})

test('prependSessionToWebBucket is idempotent for existing session', () => {
  const webDef: BucketDefinition = {
    key: 'web',
    apiSource: 'web',
    label: 'Chats',
    kind: 'web',
  }
  const bucket = { ...createEmptyBucket(webDef), total: 1, items: [{ id: 'a' }] }
  const next = prependSessionToWebBucket(bucket, { id: 'a', title: 'Same' })
  assert.equal(next, bucket)
})

test('buildBucketDefinitions contains the single user session list', () => {
  assert.deepEqual(buildBucketDefinitions({ web: 'Chats' }).map(def => def.key), ['web'])
})
