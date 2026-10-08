import assert from 'node:assert/strict'
import test from 'node:test'
import { SETTINGS_SECTIONS, SYSTEM_SETTINGS_SECTIONS } from './settingsAccess'

test('single-user settings expose models, conversations and domains', () => {
  for (const section of ['models', 'chathistory', 'mcp', 'domains']) assert.equal(SETTINGS_SECTIONS.has(section), true)
  assert.deepEqual([...SYSTEM_SETTINGS_SECTIONS], ['system-global', 'runtime-queues'])
})
