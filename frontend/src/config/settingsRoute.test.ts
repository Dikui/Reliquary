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
      tab: 'api',
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
    buildSettingsRouteQuery(integrationSectionKey('api'), {
      section: 'integrations',
      tab: 'api',
    }),
    { section: 'integration-api' },
  )
  assert.deepEqual(
    buildSettingsRouteQuery(integrationSectionKey('api'), {
      section: 'integrations',
      tab: 'api',
    }),
    { section: 'integration-api' },
  )
})

test('legacy api / integrations / bare-tab query strings normalize to nav keys', () => {
  assert.equal(normalizeSettingsSection('api'), 'integration-api')
  assert.equal(normalizeSettingsSection('api'), 'integration-api')
  assert.equal(normalizeSettingsSection('integrations', 'api'), 'integration-api')
  assert.equal(normalizeSettingsSection('integrations'), 'integration-api')
  assert.equal(normalizeSettingsSection('integration-api'), 'integration-api')
  assert.equal(normalizeSettingsSection('system-global'), 'system-global')
  assert.equal(isIntegrationSection('integration-api'), true)
  assert.equal(isIntegrationSection('models'), false)
  assert.equal(isIntegrationSection('integration-unknown'), false)
})

test('canonical settings query skips a redundant replace', () => {
  assert.equal(
    settingsQueryUnchanged(
      { section: 'integration-api' },
      { section: 'integration-api' },
    ),
    true,
  )
  assert.equal(
    settingsQueryUnchanged(
      { section: 'integrations', tab: 'api' },
      { section: 'integration-api' },
    ),
    false,
  )
})

test('deleted and unknown settings sections fall back to the default section', () => {
  for (const section of ['integration-embed', 'platform-api-keys', 'organizations', 'domains', 'made-up', '']) {
    assert.equal(normalizeSettingsSection(section), 'general')
    assert.deepEqual(buildSettingsRouteQuery(normalizeSettingsSection(section), {
      section,
      tab: 'embed',
    }), { section: 'general' })
  }
  for (const section of ['general', 'space', 'system-global', 'runtime-queues']) {
    assert.equal(normalizeSettingsSection(section), section)
  }
  assert.equal(normalizeSettingsSection('integrations', 'embed'), 'integration-api')
})
