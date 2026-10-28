<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, RouterLink, useRoute, useRouter } from 'vue-router'
import type { LocationQueryRaw } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { toast } from '@/components/ui'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import { INTEGRATION_TAB_CAPABILITY } from '@/config/integrations'
import { integrationTabFromSection, isIntegrationSection, normalizeSettingsSection } from '@/config/settingsRoute'
import GeneralSettings from './GeneralSettings.vue'
import LibrarySettings from './LibrarySettings.vue'
import ModelSettings from './ModelSettings.vue'
import OllamaSettings from './OllamaSettings.vue'
import VaultStatus from './VaultStatus.vue'
import SystemInfo from './SystemInfo.vue'
import SpaceInfo from './SpaceInfo.vue'
import UserProfile from './UserProfile.vue'
import OperationLogs from '@/views/library/OperationLogs.vue'
import SystemSettings from '@/views/system/SystemSettings.vue'
import RuntimeQueues from '@/views/system/RuntimeQueues.vue'
import IntegrationSettingsSection from '@/views/integrations/IntegrationSettingsSection.vue'

const { t } = useI18n()
const route = useRoute(), router = useRouter()
const capabilities = useDeploymentCapabilitiesStore()
const librarySettingsView = ref<InstanceType<typeof LibrarySettings>>()
const spaceInfoView = ref<InstanceType<typeof SpaceInfo>>()
const content = ref<HTMLElement>()
const currentSection = computed(() => normalizeSettingsSection(String(route.query.section || 'general'), typeof route.query.tab === 'string' ? route.query.tab : undefined))
const subsection = computed(() => typeof route.query.subsection === 'string' ? route.query.subsection : undefined)
const supported = (key: string) => !isIntegrationSection(key) || capabilities.isSupported(INTEGRATION_TAB_CAPABILITY[integrationTabFromSection(key)])
const labels = computed<Record<string, string>>(() => ({
  general: t('settingsPage.general'), library: t('settingsPage.library'), vault: t('settingsPage.vault'),
  models: t('settings.modelManagement'), ollama: 'Ollama', 'runtime-queues': t('settingsPage.queues'),
  'integration-api': t('settingsPage.api'), 'system-global': t('settings.system'),
  'operation-logs': t('operationLogs.title'), system: t('settings.versionInfo'),
  space: t('settings.spaceInfo'), userprofile: t('userProfile.title'),
}))
const navGroups = computed(() => [
  { key: 'workspace', label: t('settingsPage.workspace'), items: ['general', 'library', 'vault'] },
  { key: 'models', label: t('settingsPage.modelsRuntime'), items: ['models', 'ollama', 'runtime-queues'] },
  { key: 'integrations', label: t('integrations.title'), items: ['integration-api'] },
  { key: 'system', label: t('settingsPage.systemGroup'), items: ['system-global', 'operation-logs', 'system'] },
].map(group => ({ ...group, items: group.items.filter(supported) })).filter(group => group.items.length))
const canLeave = () => currentSection.value === 'library' ? librarySettingsView.value?.confirmLeave() ?? true
  : currentSection.value === 'space' ? spaceInfoView.value?.confirmLeave() ?? true : true
onBeforeRouteLeave(canLeave)
onBeforeRouteUpdate(to => {
  const nextSection = normalizeSettingsSection(String(to.query.section || 'general'), typeof to.query.tab === 'string' ? to.query.tab : undefined)
  return nextSection !== currentSection.value || to.query.subsection !== route.query.subsection ? canLeave() : true
})
watch(() => [route.query.section, route.query.tab, capabilities.loaded], () => {
  let section = currentSection.value
  if (capabilities.loaded && !supported(section)) {
    toast.warning(t('settings.capabilityUnavailable'))
    section = 'general'
  }
  if (route.query.section !== section || route.query.tab != null) {
    const query: LocationQueryRaw = { ...route.query, section }
    delete query.tab
    void router.replace({ path: '/platform/settings', query })
  }
}, { immediate: true })
watch(() => [currentSection.value, subsection.value], async () => {
  await nextTick()
  content.value?.scrollTo?.({ top: 0 })
})
</script>

<template>
  <main class="settings-page">
    <nav class="settings-sidebar" :aria-label="t('settingsPage.navigation')">
      <div v-for="group in navGroups" :key="group.key" class="settings-nav-group">
        <h2>{{ group.label }}</h2>
        <RouterLink v-for="key in group.items" :key="key" :to="{ path: '/platform/settings', query: { section: key } }"
          :aria-current="currentSection === key ? 'page' : undefined">{{ labels[key] }}</RouterLink>
      </div>
    </nav>
    <div ref="content" class="settings-content">
      <div class="content-wrapper">
        <h1 class="settings-title">{{ labels[currentSection] }}</h1>
        <GeneralSettings v-if="currentSection === 'general'" />
        <LibrarySettings v-else-if="currentSection === 'library'" ref="librarySettingsView" :initial-section="subsection" />
        <VaultStatus v-else-if="currentSection === 'vault'" />
        <ModelSettings v-else-if="currentSection === 'models'" :initial-section="subsection" />
        <OllamaSettings v-else-if="currentSection === 'ollama'" />
        <RuntimeQueues v-else-if="currentSection === 'runtime-queues'" />
        <IntegrationSettingsSection v-else-if="isIntegrationSection(currentSection)" :tab="integrationTabFromSection(currentSection)" />
        <SystemSettings v-else-if="currentSection === 'system-global'" />
        <OperationLogs v-else-if="currentSection === 'operation-logs'" />
        <SystemInfo v-else-if="currentSection === 'system'" />
        <SpaceInfo v-else-if="currentSection === 'space'" ref="spaceInfoView" />
        <UserProfile v-else-if="currentSection === 'userprofile'" />
      </div>
    </div>
  </main>
</template>

<style scoped lang="less">
@import './settingsPage.less';
</style>
