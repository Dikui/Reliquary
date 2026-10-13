<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="visible" class="settings-overlay">
        <div class="settings-modal">
          <!-- 关闭按钮 -->
          <button class="close-btn" @click="handleClose" :aria-label="$t('general.close')">
            <svg width="20" height="20" viewBox="0 0 20 20" fill="currentColor">
              <path d="M15 5L5 15M5 5L15 15" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
            </svg>
          </button>

          <div class="settings-container">
            <!-- 左侧导航 -->
            <div class="settings-sidebar">
              <div class="sidebar-header">
                <h2 class="sidebar-title">{{ $t('general.settings') }}</h2>
              </div>
              <div class="settings-nav">
                <template v-for="group in navGroups" :key="group.key">
                  <div class="nav-group-title">{{ group.label }}</div>
                  <template v-for="item in group.items" :key="item.key">
                    <div :class="['nav-item', {
                      'active': currentSection === item.key,
                      'has-submenu': item.children && item.children.length > 0,
                      'expanded': expandedMenus.includes(item.key)
                    }]" @click="handleNavClick(item)">
                      <span v-if="item.emoji" class="nav-icon nav-icon-emoji">{{ item.emoji }}</span>
                      <t-icon v-else :name="item.icon" class="nav-icon" />
                      <span class="nav-label">{{ item.label }}</span>
                      <t-icon v-if="item.children && item.children.length > 0"
                        :name="expandedMenus.includes(item.key) ? 'chevron-down' : 'chevron-right'"
                        class="expand-icon" />
                    </div>

                    <!-- 子菜单 -->
                    <Transition name="submenu">
                      <div v-if="item.children && expandedMenus.includes(item.key)" class="submenu">
                        <div v-for="(child, childIndex) in item.children" :key="childIndex"
                          :class="['submenu-item', { 'active': currentSubSection === child.key }]"
                          @click.stop="handleSubMenuClick(item.key, child.key)">
                          <span class="submenu-label">{{ child.label }}</span>
                        </div>
                      </div>
                    </Transition>
                  </template>
                </template>
              </div>
            </div>

            <div class="settings-mobile-nav">
              <span>{{ $t('general.settings') }}</span>
              <t-select :value="currentSection" :options="navItems.map(item => ({ value: item.key, label: item.label }))"
                :aria-label="$t('general.settings')" :popup-props="{ zIndex: 6200 }" @change="handleMobileSectionChange" />
            </div>

            <!-- 右侧内容区域 -->
            <div class="settings-content">
              <div class="content-wrapper" :class="{
                'content-wrapper--full': SYSTEM_SECTIONS.has(currentSection) || isIntegrationSection(currentSection),
              }">
                  <!-- 常规设置 -->
                  <div v-if="currentSection === 'general'" class="section">
                    <GeneralSettings />
                  </div>

                  <!-- Ollama 设置 -->
                  <div v-if="currentSection === 'ollama'" class="section">
                    <OllamaSettings />
                  </div>

                  <!-- 模型配置 -->
                  <div v-if="currentSection === 'models'" class="section">
                    <ModelSettings />
                  </div>

                  <div v-if="currentSection === 'library'" class="section"><LibrarySettings ref="librarySettingsView" :initial-section="currentSubSection || undefined" /></div>
                  <div v-if="currentSection === 'vault'" class="section"><VaultStatus /></div>
                  <div v-if="currentSection === 'operation-logs'" class="section"><OperationLogs /></div>
                  <!-- 解析引擎 -->
                  <div v-if="currentSection === 'parser'" class="section">
                    <ParserEngineSettings />
                  </div>

                  <!-- 系统信息 -->
                  <div v-if="currentSection === 'system'" class="section">
                    <SystemInfo />
                  </div>

                  <!-- 全局运行时设置 -->
                  <div v-if="currentSection === 'system-global'" class="section">
                    <SystemSettings />
                  </div>

                  <!-- 任务队列运行状态 -->
                  <div v-if="currentSection === 'runtime-queues'" class="section">
                    <RuntimeQueues />
                  </div>

                  <!-- 用户信息（账户基础信息：ID / 用户名 / 邮箱 / 注册时间）。
                     用户的基本信息不该跟 owner 权限绑定。 -->
                  <div v-if="currentSection === 'userprofile'" class="section">
                    <UserProfile />
                  </div>

                  <!-- 空间信息 -->
                  <div v-if="currentSection === 'space'" class="section">
                    <SpaceInfo ref="spaceInfoView" />
                  </div>

                  <!-- 发布集成 -->
                  <div v-if="isIntegrationSection(currentSection)" class="section">
                    <IntegrationSettingsSection :tab="integrationTabFromSection(currentSection)" />
                  </div>

              </div>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { LocationQueryRaw } from 'vue-router'
import { useUIStore } from '@/stores/ui'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import SystemInfo from './SystemInfo.vue'
import SpaceInfo from './SpaceInfo.vue'
import UserProfile from './UserProfile.vue'
import GeneralSettings from './GeneralSettings.vue'
import ModelSettings from './ModelSettings.vue'
import OllamaSettings from './OllamaSettings.vue'
import ParserEngineSettings from './ParserEngineSettings.vue'
import LibrarySettings from './LibrarySettings.vue'
import VaultStatus from './VaultStatus.vue'
import OperationLogs from '@/views/library/OperationLogs.vue'
import SystemSettings from '@/views/system/SystemSettings.vue'
import RuntimeQueues from '@/views/system/RuntimeQueues.vue'
import IntegrationSettingsSection from '@/views/integrations/IntegrationSettingsSection.vue'
import {
  INTEGRATION_PREVIEW_ITEMS,
  INTEGRATION_TAB_CAPABILITY,
} from '@/config/integrations'
import {
  SYSTEM_SETTINGS_SECTIONS,
} from '@/config/settingsAccess'
import { SETTINGS_SECTION_CAPABILITY } from '@/config/deploymentCapabilities'
import {
  buildSettingsRouteQuery,
  integrationSectionKey,
  integrationTabFromSection,
  isIntegrationSection,
  normalizeSettingsSection as normalizeSettingsSectionFromQuery,
  settingsQueryUnchanged,
} from '@/config/settingsRoute'

const route = useRoute()
const router = useRouter()
const uiStore = useUIStore()
const deploymentCapabilities = useDeploymentCapabilitiesStore()
const { t } = useI18n()

const librarySettingsView = ref<InstanceType<typeof LibrarySettings>>()
const spaceInfoView = ref<InstanceType<typeof SpaceInfo>>()
const canLeaveCurrentSection = () => currentSection.value === 'space'
  ? spaceInfoView.value?.confirmLeave() ?? Promise.resolve(true)
  : currentSection.value === 'library' ? librarySettingsView.value?.confirmLeave() ?? Promise.resolve(true) : Promise.resolve(true)
const currentSection = ref<string>('general')
const currentSubSection = ref<string>('')
const expandedMenus = ref<string[]>([])

type NavItem = {
  key: string
  icon: string
  label: string
  emoji?: string
  children?: Array<{ key: string; label: string }>
}

type NavGroup = {
  key: string
  label: string
  items: NavItem[]
}

const SYSTEM_SECTIONS = SYSTEM_SETTINGS_SECTIONS

const normalizeSettingsSection = (section: string) => {
  return normalizeSettingsSectionFromQuery(section, route.query.tab as string | undefined)
}

const syncSettingsRoute = (sectionKey: string) => {
  if (route.path !== '/platform/settings') return
  const query = buildSettingsRouteQuery(sectionKey, route.query)
  if (settingsQueryUnchanged(route.query, query)) return
  void router.replace({
    path: '/platform/settings',
    query: query as LocationQueryRaw,
  })
}

const isSectionSupported = (key: string): boolean => {
  if (isIntegrationSection(key)) {
    return deploymentCapabilities.isSupported(
      INTEGRATION_TAB_CAPABILITY[integrationTabFromSection(key)],
    )
  }
  return deploymentCapabilities.isSupported(SETTINGS_SECTION_CAPABILITY[key])
}

const navItems = computed(() => {
  const integrationItems: NavItem[] = INTEGRATION_PREVIEW_ITEMS.map((item) => ({
    key: integrationSectionKey(item.key),
    icon: item.icon.type === 'icon' ? item.icon.name : 'integration',
    emoji: item.icon.type === 'emoji' ? item.icon.value : undefined,
    label: t(`integrations.tabs.${item.key}`),
  }))
  const all: NavItem[] = [
    { key: 'general', icon: 'setting', label: t('general.title') },
    { key: 'ollama', icon: 'server', label: 'Ollama' },
    { key: 'models', icon: 'control-platform', label: t('settings.modelManagement') },
    { key: 'library', icon: 'file', label: t('settings.librarySettings') },
    { key: 'vault', icon: 'folder', label: t('settings.vault.title') },
    { key: 'operation-logs', icon: 'history', label: t('operationLogs.title') },
    { key: 'parser', icon: 'file-search', label: t('settings.parserEngine') },
    { key: 'system', icon: 'info-circle', label: t('settings.versionInfo') },
    { key: 'system-global', icon: 'server', label: t('settings.system') },
    { key: 'runtime-queues', icon: 'queue', label: t('settings.taskQueue') },
    { key: 'userprofile', icon: 'user', label: t('userProfile.title') },
    { key: 'space', icon: 'user-circle', label: t('settings.spaceInfo') },
    ...integrationItems,
  ]
  // 入口的策略一致。

  return all.filter((it) => isSectionSupported(it.key))
})

const navGroups = computed<NavGroup[]>(() => {
  const itemMap = new Map(navItems.value.map((item) => [item.key, item]))
  const pickItems = (keys: string[]) => keys.map((key) => itemMap.get(key)).filter(Boolean) as NavItem[]
  // 分组：账户 → 空间 → 模型 → 发布集成 → 数据与扩展 → 系统管理 → 平台
  // 关键调整：把个人偏好(general)和用户信息收进「账户」；
  // 把检索引擎和外部集成合并为「数据与扩展」，避免两个 2~3 项的窄分组。
  return [
    {
      key: 'account',
      label: t('settings.navGroups.account'),
      items: pickItems(['general', 'userprofile', ]),
    },
    {
      key: 'workspace',
      label: t('settings.navGroups.workspace'),
      items: pickItems(['space']),
    },
    {
      key: 'models_runtime',
      label: t('settings.navGroups.modelsRuntime'),
      items: pickItems(['models', 'ollama']),
    },
    {
      key: 'integrations',
      label: t('integrations.title'),
      items: pickItems([
        integrationSectionKey('api'),
      ]),
    },
    {
      key: 'data_extensions',
      label: t('settings.navGroups.dataExtensions'),
      items: pickItems([
        'library', 'vault', 'parser',
      ]),
    },
    {
      key: 'system_administration',
      label: t('settings.navGroups.systemAdministration'),
      items: pickItems(['system-global', 'operation-logs', 'runtime-queues']),
    },
    {
      key: 'platform',
      label: t('settings.navGroups.platform'),
      items: pickItems(['system']),
    },
  ].filter((group) => group.items.length > 0)
})

// 导航项点击处理
const handleNavClick = async (item: any) => {
  if (item.key !== currentSection.value && !await canLeaveCurrentSection()) return
  if (item.children && item.children.length > 0) {
    // 有子菜单，切换展开状态
    const index = expandedMenus.value.indexOf(item.key)
    if (index > -1) {
      expandedMenus.value.splice(index, 1)
    } else {
      expandedMenus.value.push(item.key)
    }
    currentSubSection.value = item.children[0].key
  } else {
    currentSubSection.value = ''
  }

  // 切换到对应页面，并同步 URL 为 ?section=<navKey>（含 integration-claw）。
  // 否则从其它 section 点进来时 query 不变，路由监听会把内容拉回去。
  currentSection.value = item.key
  syncSettingsRoute(item.key)
}

const handleMobileSectionChange = (value: unknown) => {
  const item = navItems.value.find(item => item.key === value)
  if (item) void handleNavClick(item)
}

// 子菜单点击处理
const handleSubMenuClick = (parentKey: string, childKey: string) => {
  currentSection.value = parentKey
  currentSubSection.value = childKey

  // 滚动到对应的模型类型区域
  setTimeout(() => {
    const element = document.querySelector(`[data-model-type="${childKey}"]`)
    if (element) {
      element.scrollIntoView({ behavior: 'smooth', block: 'start' })
    }
  }, 100)
}

// 控制弹窗显示
const visible = computed(() => {
  return route.path === '/platform/settings' || uiStore.showSettingsModal
})

const removeDraftNavigationGuard = router.beforeEach(async (to, from) => {
  if (!visible.value) return true
  const nextSection = typeof to.query.section === 'string' ? normalizeSettingsSection(to.query.section) : ''
  if (to.path !== from.path || (to.path === '/platform/settings' && nextSection && nextSection !== currentSection.value)) return canLeaveCurrentSection()
  return true
})

// 关闭弹窗
const handleClose = async () => {
  if (!await canLeaveCurrentSection()) return
  // Blur before unmount so TDesign textarea autosize won't run on a detached node.
  if (document.activeElement instanceof HTMLElement) {
    document.activeElement.blur()
  }
  uiStore.closeSettings()
  // 如果当前路由是设置页，返回上一页
  if (route.path === '/platform/settings') {
    const sec = route.query.section
    if (!router.options.history.state.back || sec === 'system-global' || sec === 'runtime-queues') {
      router.push('/platform/entries')
    } else {
      router.back()
    }
  }
}

// 监听初始导航设置
watch(() => uiStore.settingsInitialSection, (section) => {
  if (section && visible.value) {
    const normalizedSection = normalizeSettingsSection(section)
    if (deploymentCapabilities.loaded && !isSectionSupported(normalizedSection)) {
      MessagePlugin.warning(t('settings.capabilityUnavailable'))
      currentSection.value = navItems.value[0]?.key || 'general'
      currentSubSection.value = ''
      return
    }
    currentSection.value = normalizedSection
    syncSettingsRoute(normalizedSection)
    const navItem = (navItems.value as any[]).find((item) => item.key === normalizedSection)
    if (navItem && navItem.children && navItem.children.length > 0) {
      if (!expandedMenus.value.includes(section)) {
        expandedMenus.value.push(section)
      }
      currentSubSection.value = uiStore.settingsInitialSubSection || navItem.children[0].key
      if (uiStore.settingsInitialSubSection) {
        setTimeout(() => {
          const element = document.querySelector(`[data-model-type="${uiStore.settingsInitialSubSection}"]`)
          if (element) {
            element.scrollIntoView({ behavior: 'smooth', block: 'start' })
          }
        }, 300)
      }
    } else {
      currentSubSection.value = uiStore.settingsInitialSubSection || ''
    }
  }
}, { immediate: true })

watch(
  () => [visible.value, route.path, route.query.section, deploymentCapabilities.loaded] as const,
  ([isVisible, path, section, capabilitiesLoaded]) => {
    if (!isVisible || path !== '/platform/settings') return
    if (typeof section !== 'string') {
      syncSettingsRoute(currentSection.value || 'general')
      return
    }
    const normalizedSection = normalizeSettingsSectionFromQuery(
      section,
      typeof route.query.tab === 'string' ? route.query.tab : undefined,
    )
    if (capabilitiesLoaded && !isSectionSupported(normalizedSection)) {
      MessagePlugin.warning(t('settings.capabilityUnavailable'))
      const fallback = navItems.value[0]?.key || 'general'
      currentSection.value = fallback
      currentSubSection.value = ''
      syncSettingsRoute(fallback)
      return
    }
    currentSection.value = normalizedSection
    currentSubSection.value = ''
    syncSettingsRoute(normalizedSection)
  },
  { immediate: true },
)

// 如果 currentSection 落到了不再显示的 key 上，就回退到第一个可见项。
watch(navItems, (items) => {
  if (!items.some((item) => item.key === currentSection.value)) {
    const fallback = items[0]?.key || 'general'
    currentSection.value = fallback
    currentSubSection.value = ''
    syncSettingsRoute(fallback)
  }
})

// ESC 键关闭
const handleEscape = (e: KeyboardEvent) => {
  if (e.key === 'Escape' && visible.value) {
    handleClose()
  }
}

// 处理快捷导航事件
const handleSettingsNav = (e: CustomEvent) => {
  const { section, subsection } = e.detail
  if (section) {
    const normalizedSection = normalizeSettingsSection(section)
    if (deploymentCapabilities.loaded && !isSectionSupported(normalizedSection)) {
      MessagePlugin.warning(t('settings.capabilityUnavailable'))
      currentSection.value = navItems.value[0]?.key || 'general'
      currentSubSection.value = ''
      return
    }
    currentSection.value = normalizedSection
    syncSettingsRoute(normalizedSection)
    // 如果有子菜单，自动展开
    const navItem = (navItems.value as any[]).find((item: any) => item.key === normalizedSection)
    if (navItem && navItem.children && navItem.children.length > 0) {
      if (!expandedMenus.value.includes(section)) {
        expandedMenus.value.push(section)
      }
      // 如果有 subsection，选中对应的子菜单项
      currentSubSection.value = subsection || navItem.children[0].key
    }
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleEscape)
  window.addEventListener('settings-nav', handleSettingsNav as EventListener)
})

watch(currentSection, () => {
  if (document.activeElement instanceof HTMLElement) {
    document.activeElement.blur()
  }
})

onUnmounted(() => {
  removeDraftNavigationGuard()
  window.removeEventListener('keydown', handleEscape)
  window.removeEventListener('settings-nav', handleSettingsNav as EventListener)
})
</script>

<style lang="less" scoped>
/* 遮罩层 */
.settings-overlay {
  position: fixed;
  inset: 0;
  z-index: 1100;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  backdrop-filter: blur(4px);
}

/* 弹窗容器 */
.settings-modal {
  position: relative;
  width: 100%;
  // 1080×780 trades a touch of small-screen real estate for noticeably
  // less cramped tables (member list, system settings rows). Outer
  // padding is 20px so 1080 + 40 = 1120, comfortably within typical
  // laptops (1280+). Below 1100px viewport the `width: 100%` kicks in
  // and the modal shrinks to fit minus the 20px padding.
  max-width: 1080px;
  height: 780px;
  max-height: calc(100vh - 40px);
  background: var(--td-bg-color-container);
  border-radius: 12px;
  box-shadow: 0 6px 28px rgba(15, 23, 42, 0.08);
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

/* 关闭按钮 */
.close-btn {
  position: absolute;
  top: 16px;
  right: 16px;
  width: 32px;
  height: 32px;
  border: none;
  background: transparent;
  color: var(--td-text-color-secondary);
  cursor: pointer;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s ease;
  z-index: 10;

  &:hover {
    background: var(--td-bg-color-container-hover);
    color: var(--td-text-color-primary);
  }
}

.settings-container {
  display: flex;
  height: 100%;
  width: 100%;
  overflow: hidden;
}

/* 左侧导航栏：略紧凑于最初版，字号与留白适中 */
.settings-sidebar {
  width: 208px;
  background-color: var(--td-bg-color-settings-modal);
  border-right: 1px solid var(--td-component-stroke);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.sidebar-header {
  padding: 16px 14px 12px;
  border-bottom: 1px solid var(--td-component-stroke);
  flex-shrink: 0;
}

.sidebar-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--td-text-color-primary);
  margin: 0;
}

.settings-nav {
  padding: 8px 8px 12px;
  flex: 1;
  overflow-y: auto;
  min-height: 0;
}

.nav-group-title {
  padding: 9px 14px 4px;
  color: var(--td-text-color-placeholder);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.nav-item {
  display: flex;
  align-items: center;
  padding: 6px 12px;
  margin-bottom: 2px;
  border-radius: 6px;
  cursor: pointer;
  color: var(--td-text-color-primary);
  font-size: 14px;
  transition: all 0.2s ease;
  user-select: none;

  &:hover {
    background-color: var(--td-bg-color-container-hover);
    color: var(--td-text-color-primary);
  }

  &.active {
    background-color: var(--td-bg-color-secondarycontainer);
    color: var(--td-brand-color);
    font-weight: 500;
  }
}

.nav-icon {
  margin-right: 9px;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: inherit;
}

.nav-icon-emoji {
  font-size: 14px;
  line-height: 1;
}

.nav-label {
  flex: 1;
}

.expand-icon {
  margin-left: 4px;
  font-size: 14px;
  transition: transform 0.2s ease;
}

/* 子菜单 */
.submenu {
  margin-left: 28px;
  margin-bottom: 3px;
  overflow: hidden;
}

.submenu-item {
  padding: 5px 12px;
  margin-bottom: 2px;
  border-radius: 4px;
  cursor: pointer;
  color: var(--td-text-color-primary);
  font-size: 13px;
  transition: all 0.2s ease;
  user-select: none;

  &:hover {
    background-color: var(--td-bg-color-container-hover);
    color: var(--td-text-color-primary);
  }

  &.active {
    background-color: var(--td-bg-color-secondarycontainer);
    color: var(--td-brand-color);
    font-weight: 500;
  }
}

.submenu-label {
  display: block;
}

/* 子菜单动画 */
.submenu-enter-active,
.submenu-leave-active {
  transition: all 0.2s ease;
}

.submenu-enter-from {
  opacity: 0;
  max-height: 0;
}

.submenu-enter-to {
  opacity: 1;
  max-height: 300px;
}

.submenu-leave-from {
  opacity: 1;
  max-height: 300px;
}

.submenu-leave-to {
  opacity: 0;
  max-height: 0;
}

/* 右侧内容区域 */
.settings-content {
  flex: 1;
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  background-color: var(--td-bg-color-container);
}

.settings-mobile-nav { display: none; }

.content-wrapper {
  container-name: settings-content;
  container-type: inline-size;
  // Bumped from 600 to 760 when the modal grew from 900→1080 (see
  // .settings-modal). Without this, single-column panes (General,
  // Space, API key, …) leave a wide right-hand gutter inside the
  // wider modal. 760 keeps comfortable reading-width on long
  // descriptions without the form fields stretching to the full
  // panel width — which would look stranger than a small gutter.
  max-width: 760px;
  padding: 40px 48px;

  /* 成员 / 审计表格列多，600px 会把操作列挤到贴边；铺满右侧内容列更稳。 */
  &--wide {
    max-width: none;
    width: 100%;
    padding: 32px 36px 40px;
    box-sizing: border-box;
  }

  &--full {
    max-width: none;
    width: 100%;
    padding: 30px 34px 40px;
    box-sizing: border-box;
  }
}

.section {
  animation: fadeIn 0.3s ease;
}

@keyframes fadeIn {
  from {
    opacity: 0;
    transform: translateY(10px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

/* 弹窗动画 */
.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.2s ease;
}

.modal-enter-active .settings-modal,
.modal-leave-active .settings-modal {
  transition: transform 0.2s ease, opacity 0.2s ease;
}

.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}

.modal-enter-from .settings-modal,
.modal-leave-to .settings-modal {
  transform: scale(0.95);
  opacity: 0;
}

/* 滚动条样式 */
.settings-nav::-webkit-scrollbar,
.settings-content::-webkit-scrollbar {
  width: 6px;
}

.settings-nav::-webkit-scrollbar-track {
  background: var(--td-bg-color-secondarycontainer);
}

.settings-nav::-webkit-scrollbar-thumb {
  background: var(--td-gray-color-5);
  border-radius: 3px;
}

.settings-nav::-webkit-scrollbar-thumb:hover {
  background: var(--td-gray-color-6);
}

.settings-content::-webkit-scrollbar-track {
  background: var(--td-bg-color-container);
}

.settings-content::-webkit-scrollbar-thumb {
  background: var(--td-gray-color-5);
  border-radius: 3px;
}

.settings-content::-webkit-scrollbar-thumb:hover {
  background: var(--td-gray-color-6);
}

@media (max-width: 1100px) {
  .content-wrapper, .content-wrapper--wide, .content-wrapper--full {
    padding-left: 24px;
    padding-right: 24px;
  }
}

@media (max-width: 900px) {
  .settings-sidebar { width: 176px; }
  .nav-label { min-width: 0; overflow-wrap: anywhere; }
}

@media (max-width: 640px) {
  .settings-overlay { padding: 12px; }
  .settings-modal { max-height: calc(100dvh - 24px); }
  .settings-container { flex-direction: column; }
  .settings-sidebar { display: none; }
  .settings-mobile-nav {
    display: flex;
    flex-direction: column;
    flex-shrink: 0;
    gap: 8px;
    padding: 16px 56px 16px 16px;
    border-bottom: 1px solid var(--td-component-stroke);
    background: var(--td-bg-color-settings-modal);
    color: var(--td-text-color-primary);
  }
  .content-wrapper, .content-wrapper--wide, .content-wrapper--full {
    width: 100%;
    max-width: none;
    box-sizing: border-box;
    padding: 20px 16px;
  }
  .section { min-width: 0; }
}
</style>
