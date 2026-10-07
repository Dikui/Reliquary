import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(
  new URL('./SandboxConfigEditorDrawer.vue', import.meta.url), 'utf8')

test('network policy lives in the runtime step, right below the runtime config', () => {
  const runtimeSections = source.indexOf("currentStepKey === 'runtime'")
  const networkSection = source.indexOf('settings.sandbox.sectionNetwork')
  const envSection = source.indexOf('settings.sandbox.sectionEnvironment')

  assert.ok(runtimeSections !== -1, 'runtime step must exist')
  assert.ok(networkSection !== -1, 'network policy section must exist')
  assert.ok(
    networkSection > runtimeSections && networkSection < envSection,
    'network policy must sit between the runtime config and the env vars',
  )
  assert.ok(
    !source.includes("currentStepKey === 'network'"),
    'network policy must not become a fourth wizard step',
  )
})

test('runtime numbers are laid out two per row with per-field tips', () => {
  const runtimeBlock = source.slice(
    source.indexOf('settings.sandbox.sectionRuntime'),
    source.indexOf('settings.sandbox.sectionNetwork'),
  )
  assert.ok(
    runtimeBlock.includes('form-grid form-grid--two'),
    'timeouts must be compressed into the two-column grid',
  )
  // Each number keeps its own sentence; the previous layout put those in
  // block paragraphs, which is what made the step so tall.


  for (const key of [
    'dockerCpuLimitHelp',
    'dockerMemoryLimitHelp',
    'dockerPidsLimitHelp',
  ]) {
    assert.ok(
      runtimeBlock.includes(`:tips="$t('settings.sandbox.${key}')"`),
      `${key} must remain attached to its compressed field`,
    )
  }
})

test('docker network mode moved into the network policy section', () => {
  const networkBlock = source.slice(source.indexOf('settings.sandbox.sectionNetwork'))
  assert.ok(
    networkBlock.includes('settings.sandbox.dockerNetworkMode'),
    'the docker bridge/none selector belongs with the other network controls',
  )
  const runtimeBlock = source.slice(
    source.indexOf('settings.sandbox.sectionRuntime'),
    source.indexOf('settings.sandbox.sectionNetwork'),
  )
  assert.ok(
    !runtimeBlock.includes('settings.sandbox.dockerNetworkMode'),
    'and must no longer be duplicated in the runtime config',
  )
})
