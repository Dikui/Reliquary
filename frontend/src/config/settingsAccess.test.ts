import assert from 'node:assert/strict'
import test from 'node:test'
import { SETTINGS_SECTIONS, SYSTEM_SETTINGS_SECTIONS } from './settingsAccess'

test('single-user settings expose models, skills, environment variables and domains', () => {
  for (const section of ['models', 'skills', 'sandbox', 'envvars', 'domains']) assert.equal(SETTINGS_SECTIONS.has(section), true)
  assert.deepEqual([...SYSTEM_SETTINGS_SECTIONS], ['system-global', 'runtime-queues'])
})
