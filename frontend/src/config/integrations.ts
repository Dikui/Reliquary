import type { DeploymentCapabilityKey } from './deploymentCapabilities'

export type IntegrationTab = 'api'

export const INTEGRATION_TABS: IntegrationTab[] = ['api']

export const INTEGRATION_TAB_CAPABILITY: Partial<Record<IntegrationTab, DeploymentCapabilityKey>> = {
  api: 'integrations.api',
}

export type IntegrationPreviewIcon =
  | { type: 'icon'; name: string }
  | { type: 'emoji'; value: string }

/** Sidebar hover preview + Integrations modal nav — add new entries here. */
export const INTEGRATION_PREVIEW_ITEMS: Array<{
  key: IntegrationTab
  icon: IntegrationPreviewIcon
}> = [
  { key: 'api', icon: { type: 'icon', name: 'secured' } },
]
