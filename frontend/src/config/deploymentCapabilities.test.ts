import assert from 'node:assert/strict'
import test from 'node:test'

import {
  SETTINGS_SECTION_CAPABILITY,
  isDeploymentCapabilitySupported,
  type DeploymentCapabilityMap,
} from './deploymentCapabilities'

test('capability filtering is fail-open unless backend explicitly disables a feature', () => {
  assert.equal(isDeploymentCapabilitySupported({}, 'integrations.api'), true)

  const capabilities: DeploymentCapabilityMap = {
    'integrations.api': { supported: false, reason: 'feature_disabled' },
  }
  assert.equal(isDeploymentCapabilitySupported(capabilities, 'integrations.api'), false)
  assert.equal(isDeploymentCapabilitySupported({}, 'integrations.api'), true)
})

test('only route-backed settings sections require deployment capabilities', () => {
  assert.equal(Object.keys(SETTINGS_SECTION_CAPABILITY).length, 0)
  assert.equal(SETTINGS_SECTION_CAPABILITY.parser, undefined)
  assert.equal(SETTINGS_SECTION_CAPABILITY['runtime-queues'], undefined)
})
