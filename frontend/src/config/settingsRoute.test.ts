import assert from 'node:assert/strict'
import test from 'node:test'

import {
  buildSettingsRouteQuery,
  integrationSectionKey,
  isIntegrationSection,
  normalizeSettingsSection,
  settingsQueryUnchanged,
} from './settingsRoute'

test('every settings nav item writes only section', () => {
  assert.deepEqual(
    buildSettingsRouteQuery('models', {
      section: 'system-global',
      tab: 'im',
      agentId: 'agt_1',
    }),
    { section: 'models' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery('general', { section: 'system-global' }),
    { section: 'general' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery('runtime-queues', { section: 'system-global' }),
    { section: 'runtime-queues' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery(integrationSectionKey('im'), {
      section: 'integrations',
      tab: 'im',
    }),
    { section: 'integration-im' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery(integrationSectionKey('api'), {
      section: 'integrations',
      tab: 'im',
      agentId: 'agt_1',
    }),
    { section: 'integration-api', agentId: 'agt_1' },
  )
})

test('legacy api / integrations / bare-tab query strings normalize to nav keys', () => {
  assert.equal(normalizeSettingsSection('api'), 'integration-api')
  assert.equal(normalizeSettingsSection('im'), 'integration-im')
  assert.equal(normalizeSettingsSection('integrations', 'im'), 'integration-im')
  assert.equal(normalizeSettingsSection('integrations'), 'integration-im')
  assert.equal(normalizeSettingsSection('integration-api'), 'integration-api')
  assert.equal(normalizeSettingsSection('system-global'), 'system-global')
  assert.equal(isIntegrationSection('integration-api'), true)
  assert.equal(isIntegrationSection('models'), false)
  assert.equal(isIntegrationSection('integration-unknown'), false)
})

test('canonical settings query skips a redundant replace', () => {
  assert.equal(
    settingsQueryUnchanged(
      { section: 'integration-im' },
      { section: 'integration-im' },
    ),
    true,
  )
  assert.equal(
    settingsQueryUnchanged(
      { section: 'integrations', tab: 'im' },
      { section: 'integration-im' },
    ),
    false,
  )
})

test('deleted and unknown settings sections fall back to the default section', () => {
  for (const section of ['integration-embed', 'platform-api-keys', 'organizations', 'made-up', '']) {
    assert.equal(normalizeSettingsSection(section), 'general')
    assert.deepEqual(buildSettingsRouteQuery(normalizeSettingsSection(section), {
      section,
      tab: 'embed',
      agentId: 'removed-agent',
    }), { section: 'general' })
  }
  for (const section of ['general', 'domains', 'tenant', 'members', 'system-global', 'runtime-queues', 'system-audit-log']) {
    assert.equal(normalizeSettingsSection(section), section)
  }
  assert.equal(normalizeSettingsSection('integrations', 'embed'), 'integration-im')
})
