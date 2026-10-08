import assert from 'node:assert/strict'
import test from 'node:test'

import {
  SETTINGS_SECTION_CAPABILITY,
  isDeploymentCapabilitySupported,
  type DeploymentCapabilityMap,
} from './deploymentCapabilities'

test('capability filtering is fail-open unless backend explicitly disables a feature', () => {
  assert.equal(isDeploymentCapabilitySupported({}, 'agents'), true)

  const capabilities: DeploymentCapabilityMap = {
    agents: { supported: false, reason: 'feature_disabled' },
    'integrations.api': { supported: true },
  }
  assert.equal(isDeploymentCapabilitySupported(capabilities, 'agents'), false)
  assert.equal(isDeploymentCapabilitySupported(capabilities, 'integrations.api'), true)
})

test('only route-backed settings sections require deployment capabilities', () => {
  assert.equal(SETTINGS_SECTION_CAPABILITY.mcp, 'settings.mcp')
  assert.equal(SETTINGS_SECTION_CAPABILITY.parser, undefined)
  assert.equal(SETTINGS_SECTION_CAPABILITY['runtime-queues'], undefined)
})
