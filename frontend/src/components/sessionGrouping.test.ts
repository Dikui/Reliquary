import assert from 'node:assert/strict'
import test from 'node:test'

import {
  API_EXTERNAL_USER_SESSION_OWNER_PREFIX,
  classifyDateBucket,
  configuredPlatforms,
  groupSessions,
  groupSessionsBySource,
  resolveSessionOrigin,
  shouldShowSessionSourceBadge,
} from './sessionGrouping.ts'

const sourceLabels = {
  web: 'Web',
  api: 'API',
  imPlatform: (p: string) => `IM ${p}`,
}

test('resolveSessionOrigin distinguishes web, IM, and API sessions', () => {
  assert.deepEqual(resolveSessionOrigin({ id: '1' }), { kind: 'web' })
  assert.deepEqual(resolveSessionOrigin({ id: '2', im_platform: 'feishu' }), {
    kind: 'im',
    platform: 'feishu',
  })
  assert.deepEqual(
    resolveSessionOrigin({ id: '4', user_id: 'api_tenant_key:1:10' }),
    { kind: 'api' },
  )
  assert.deepEqual(
    resolveSessionOrigin({ id: '5', user_id: `${API_EXTERNAL_USER_SESSION_OWNER_PREFIX}1:alice` }),
    { kind: 'api' },
  )
})

test('configuredPlatforms returns distinct platform keys in first-seen order', () => {
  const channels = [
    { platform: 'feishu' },
    { platform: 'feishu' },
    { platform: 'feishu' },
    { platform: '' },
  ]
  assert.deepEqual(configuredPlatforms(channels), ['feishu'])
})

test('groupSessionsBySource orders web and configured IM', () => {
  const sessions = [
    { id: 'w', title: 'web chat' },
    { id: 'f', title: 'feishu', im_platform: 'feishu' },
  ]
  const groups = groupSessionsBySource(sessions, sourceLabels, ['feishu'], 'Pinned')
  assert.deepEqual(
    groups.map((g) => g.key),
    ['web', 'im:feishu'],
  )
})

test('groupSessions date mode keeps pinned sessions in their own bucket', () => {
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
      configuredImPlatforms: [],
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
