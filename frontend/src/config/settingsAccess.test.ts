import assert from 'node:assert/strict'
import test from 'node:test'
import { SETTINGS_SECTIONS, SYSTEM_SETTINGS_SECTIONS } from './settingsAccess'

test('single-user settings expose models and retained system sections', () => {
  for (const section of ['models', 'parser', 'system']) assert.equal(SETTINGS_SECTIONS.has(section), true)
  for (const section of ['domains', 'chathistory', 'mcp']) assert.equal(SETTINGS_SECTIONS.has(section), false)
  assert.deepEqual([...SYSTEM_SETTINGS_SECTIONS], ['system-global', 'runtime-queues'])
})
