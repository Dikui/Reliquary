import assert from 'node:assert/strict'
import test from 'node:test'

import {
  classifyDateBucket,
  groupSessions,
  resolveSessionOrigin,
  shouldShowSessionSourceBadge,
} from './sessionGrouping.ts'

const sourceLabels = {
  web: 'Web',
  api: 'API',
}

test('groupSessions date mode keeps sessions pinned sessions in their own bucket', () => {
  const now = new Date().toISOString()
  const groups = groupSessions(
    'date',
    [
      { id: 'p', is_pinned: true, updated_at: now },
      { id: 't', updated_at: now },
    ],
    {
      pinnedLabel: 'Pinned',
      bucketLabels: {
        pinned: 'Pinned',
        today: 'Today',
        yesterday: 'Yesterday',
        last7Days: '7d',
        last30Days: '30d',
        lastYear: 'Year',
        earlier: 'Earlier',
      },
      categorizeDate: () => 'today',
      sourceLabels,
    },
  )
  assert.deepEqual(
    groups.map((g) => g.key),
    ['pinned', 'today'],
  )
})

test('classifyDateBucket buckets recent sessions as today', () => {
  assert.equal(classifyDateBucket(new Date().toISOString()), 'today')
})

test('shouldShowSessionSourceBadge is off when channels use separate folders', () => {
  assert.equal(shouldShowSessionSourceBadge('date'), false)
  assert.equal(shouldShowSessionSourceBadge('none'), false)
})
