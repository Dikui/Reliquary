import assert from 'node:assert/strict'
import test from 'node:test'
import { SETTINGS_SECTIONS, SYSTEM_SETTINGS_SECTIONS } from './settingsAccess'

test('single-user settings expose models and conversations', () => {
  for (const section of ['models', 'chathistory', 'mcp']) assert.equal(SETTINGS_SECTIONS.has(section), true)
  assert.equal(SETTINGS_SECTIONS.has('domains'), false)
  assert.deepEqual([...SYSTEM_SETTINGS_SECTIONS], ['system-global', 'runtime-queues'])
})
