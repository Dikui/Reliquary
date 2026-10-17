<template>
  <div class="user-menu" :class="{ 'user-menu--collapsed': sidebarCollapsed }" ref="menuRef">
    <!-- 用户按钮 -->
    <div class="user-button" @click="toggleMenu">
      <div class="user-avatar">
        <img v-if="userAvatar" :src="userAvatar" :alt="$t('common.avatar')" />
        <span v-else class="avatar-placeholder">{{ userInitial }}</span>
      </div>
      <template v-if="!sidebarCollapsed">
        <div class="user-info">

          <div class="user-name">{{ userName }}</div>
          <div class="user-email">{{ userEmail }}</div>
        </div>
        <t-icon :name="menuVisible ? 'chevron-up' : 'chevron-down'" class="dropdown-icon" />
      </template>
    </div>

    <!-- 下拉菜单 -->
    <Transition name="dropdown">
      <div v-if="menuVisible" class="user-dropdown" @click.stop>
        <!-- 弹出菜单：账号（头像+昵称）／当前空间（名称+权限）；底部侧栏样式不改。 -->
        <div v-if="userName" class="dropdown-user-header is-clickable" role="button" tabindex="0"
          @click="handleQuickNav('userprofile')" @keydown.enter.prevent="handleQuickNav('userprofile')"
          @keydown.space.prevent="handleQuickNav('userprofile')">
          <div class="dropdown-user-avatar">
            <img v-if="userAvatar" :src="userAvatar" :alt="$t('common.avatar')" />
            <span v-else class="dropdown-user-avatar-placeholder">{{ userInitial }}</span>
          </div>
          <div class="dropdown-user-meta">
            <div class="dropdown-user-name-row">
              <span class="dropdown-user-name">{{ userName }}</span>
            </div>
            <span v-if="userEmail" class="dropdown-user-email">{{ userEmail }}</span>
          </div>
        </div>

        <div v-if="userName" class="dropdown-space-panel">
          <t-icon name="system-sum" class="menu-icon" aria-hidden="true" />
          <div class="dropdown-space-panel-main">
            <span class="dropdown-space-panel-name" :title="activeSpaceName || userName">
              {{ activeSpaceName || userName }}
            </span>

          </div>

        </div>
        <div class="menu-divider"></div>
        <!-- 账号与空间是头像菜单的核心上下文；基础设施类配置统一收进「全部设置」。 -->
        <div class="menu-item" @click="handleQuickNav('general')">
          <t-icon name="user" class="menu-icon" />
          <span>{{ $t('general.personalSettings') }}</span>
        </div>
        <div class="menu-item" @click="handleQuickNav('space')">
          <t-icon name="user-circle" class="menu-icon" />
          <span>{{ $t('settings.workspaceSettings') }}</span>
        </div>
        <div class="menu-item" @click="handleQuickNav('models')">
          <t-icon name="control-platform" class="menu-icon" />
          <span>{{ $t('settings.modelManagement') }}</span>
        </div>

        <div class="menu-divider"></div>
        <div class="menu-item" @click="handleSettings">
          <t-icon name="setting" class="menu-icon" />
          <span>{{ $t('general.allSettings') }}</span>
        </div>

        <div class="menu-item" @click="handleSystem">
          <t-icon name="server" class="menu-icon" />
          <span>{{ $t('settings.navGroups.systemAdministration') }}</span>
        </div>
        <div class="menu-divider"></div>
        <div class="menu-item" @click="openDocs">
          <t-icon name="help-circle" class="menu-icon" />
          <span class="menu-text-with-icon">
            <span>{{ $t('general.helpAndDocs') }}</span>
            <svg class="menu-external-icon" viewBox="0 0 16 16" aria-hidden="true">
              <path fill="currentColor"
                d="M12.667 8a.667.667 0 0 1 .666.667v4a2.667 2.667 0 0 1-2.666 2.666H4.667a2.667 2.667 0 0 1-2.667-2.666V5.333a2.667 2.667 0 0 1 2.667-2.666h4a.667.667 0 1 1 0 1.333h-4a1.333 1.333 0 0 0-1.333 1.333v7.334A1.333 1.333 0 0 0 4.667 13.333h6a1.333 1.333 0 0 0 1.333-1.333v-4A.667.667 0 0 1 12.667 8Zm2.666-6.667v4a.667.667 0 0 1-1.333 0V3.276l-5.195 5.195a.667.667 0 0 1-.943-.943l5.195-5.195h-2.057a.667.667 0 0 1 0-1.333h4a.667.667 0 0 1 .666.666Z" />
            </svg>
          </span>
        </div>
        <div class="menu-item" :title="$t('common.githubStarTip')" @click="openGithub">
          <t-icon name="logo-github" class="menu-icon" />
          <span class="menu-text-with-icon">
            <span>{{ $t('common.github') }}</span>
            <t-icon name="star-filled" class="menu-github-star-icon" size="16px" aria-hidden="true" />
            <svg class="menu-external-icon" viewBox="0 0 16 16" aria-hidden="true">
              <path fill="currentColor"
                d="M12.667 8a.667.667 0 0 1 .666.667v4a2.667 2.667 0 0 1-2.666 2.666H4.667a2.667 2.667 0 0 1-2.667-2.666V5.333a2.667 2.667 0 0 1 2.667-2.666h4a.667.667 0 1 1 0 1.333h-4a1.333 1.333 0 0 0-1.333 1.333v7.334A1.333 1.333 0 0 0 4.667 13.333h6a1.333 1.333 0 0 0 1.333-1.333v-4A.667.667 0 0 1 12.667 8Zm2.666-6.667v4a.667.667 0 0 1-1.333 0V3.276l-5.195 5.195a.667.667 0 0 1-.943-.943l5.195-5.195h-2.057a.667.667 0 0 1 0-1.333h4a.667.667 0 0 1 .666.666Z" />
            </svg>
          </span>
        </div>
      </div>
    </Transition>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useUIStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { getCurrentUser, userInfoFromApi } from '@/api/auth'
import { useI18n } from 'vue-i18n'
import { getRootZoom, rectToCssPx, cssViewportSize } from '@/utils/zoom'
import { workspaceName } from '@/utils/workspaceCopy'

const { t } = useI18n()

const router = useRouter()
const uiStore = useUIStore()
const props = defineProps<{ collapsed?: boolean }>()
const sidebarCollapsed = computed(() => props.collapsed ?? uiStore.sidebarCollapsed)
const authStore = useAuthStore()

const activeSpaceName = computed(() => workspaceName(authStore.space?.name, t))

const menuRef = ref<HTMLElement>()
const menuVisible = ref(false)

// 用户信息
const userInfo = ref({
  username: t('common.defaultUser'),
  email: 'user@example.com',
  avatar: ''
})

const userName = computed(() => userInfo.value.username)
const userEmail = computed(() => userInfo.value.email)
const userAvatar = computed(() => userInfo.value.avatar)

// 用户名首字母（用于无头像时显示）
const userInitial = computed(() => {
  return userName.value.charAt(0).toUpperCase()
})

// 切换菜单显示
const toggleMenu = () => {
  menuVisible.value = !menuVisible.value
}

// 快捷导航到设置的特定部分
const handleQuickNav = (section: string) => {
  menuVisible.value = false
  uiStore.openSettings()
  router.push({ path: '/platform/settings', query: { section } })
}

// 打开设置
const handleSettings = () => {
  menuVisible.value = false
  uiStore.openSettings()
  router.push('/platform/settings')
}

// Open the platform administration group inside the standard Settings
// modal. Global settings is the group's landing page; task queues, platform
const handleSystem = () => {
  menuVisible.value = false
  uiStore.openSettings('system-global')
  router.push({ path: '/platform/settings', query: { section: 'system-global' } })
}

// Hover-driven submenu controls. A small hide delay tolerates the pointer
// slipping off briefly onto the gap between menu item and submenu pane.
const closeAll = () => {
  menuVisible.value = false
}

const openDocs = () => {
  menuVisible.value = false
  window.open('https://github.com/Dikui/Reliquary/tree/main/docs', '_blank', 'noopener,noreferrer')
}

// 打开 GitHub
const openGithub = () => {
  menuVisible.value = false
  window.open('https://github.com/Dikui/Reliquary', '_blank', 'noopener,noreferrer')
}

// 加载用户信息
const loadUserInfo = async () => {
  try {
    const response = await getCurrentUser()
    if (response.success && response.data && response.data.user) {
      const user = response.data.user
      userInfo.value = {
        username: user.username || t('common.info'),
        email: user.email || 'user@example.com',
        avatar: user.avatar || ''
      }
      // Keep stored user fields consistent with the authentication response.
      authStore.setUser(userInfoFromApi(user))
      if (response.data.space) {
        authStore.setSpace({
          id: String(response.data.space.id),
          name: response.data.space.name,
          owner_id: user.id,
          created_at: response.data.space.created_at,
          updated_at: response.data.space.updated_at
        })
      } else {
        authStore.setSpace(null)
      }

    }
  } catch (error) {
    console.error('Failed to load user info:', error)
  }
}

// 点击外部关闭菜单
const handleClickOutside = (e: MouseEvent) => {
  const target = e.target as Node
  if (menuRef.value && menuRef.value.contains(target)) return
  menuVisible.value = false
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  loadUserInfo()
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style lang="less" scoped>
.user-menu {
  position: relative;
  width: 100%;

  &--collapsed {
    .user-button {
      justify-content: center;
      padding: 6px 3px;
      gap: 0;
    }

    .user-dropdown {
      left: calc(100% + 8px);
      bottom: 0;
      right: auto;
      /* 与展开侧栏时下拉可视宽度对齐（aside 宽 260px） */
      min-width: 260px;
    }
  }
}

.user-button {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 6px;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.2s;
  background: transparent;

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  &:active {
    transform: scale(0.98);
  }
}

.user-avatar {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  overflow: hidden;
  flex-shrink: 0;
  background: linear-gradient(135deg, var(--td-brand-color) 0%, var(--td-brand-color-active) 100%);
  display: flex;
  align-items: center;
  justify-content: center;
  transition: width 0.2s ease, height 0.2s ease;

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .avatar-placeholder {
    color: var(--td-text-color-anti);
    font-size: 12px;
    font-weight: 600;
    line-height: 1;
  }
}

.user-info {
  flex: 1;
  min-width: 0;
  text-align: left;
  display: flex;
  flex-direction: column;
  gap: 2px;
  justify-content: center;

  .user-name {
    font-size: 14px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .user-email {
    font-size: 12px;
    color: var(--td-text-color-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .user-space-name {
    font-size: 14px;
    font-weight: 600;
    letter-spacing: -0.01em;
    color: var(--td-text-color-primary);
    line-height: 1.35;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .user-space-meta {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-top: 0;
    min-width: 0;
    font-size: 12px;
    line-height: 1.35;
    color: var(--td-text-color-secondary);

    .user-space-meta-name {
      flex: 0 1 auto;
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .user-space-meta-sep {
      flex-shrink: 0;
      color: var(--td-text-color-placeholder);
    }

    .user-space-meta-icon {
      flex-shrink: 0;
      color: inherit;
    }

    .user-space-meta-role {
      flex-shrink: 0;
    }
  }
}

.dropdown-icon {
  font-size: 16px;
  color: var(--td-text-color-secondary);
  flex-shrink: 0;
  transition: transform 0.2s;
}

.user-dropdown {
  position: absolute;
  bottom: 100%;
  /* 相对 .user-menu：左右由 left/right 拉宽；右缘用正值内缩，避免与侧栏内容区右边界完全重合 */
  left: -4px;
  right: -5px;
  margin-bottom: 6px;
  background: var(--td-bg-color-container);
  border-radius: 8px;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.12);
  border: 1px solid var(--td-component-stroke);
  overflow: hidden;
  z-index: 1000;
}

// 下拉顶部 — 账号区：24px 头像中心与下方 16px 菜单图标中心同竖线；
// margin-left −4px、gap 6px 保持昵称起点与菜单文案对齐（12 + 24 + 6 − 4 = 38）
.dropdown-user-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 9px 12px;
  min-width: 0;

  &.is-clickable {
    cursor: pointer;
    transition: background-color 0.15s ease;

    &:hover,
    &:focus-visible {
      background: var(--td-bg-color-container-hover);
      outline: none;
    }
  }

  .dropdown-user-avatar {
    width: 24px;
    height: 24px;
    margin-left: -4px;
    border-radius: 50%;
    overflow: hidden;
    flex-shrink: 0;
    background: linear-gradient(135deg, var(--td-brand-color) 0%, var(--td-brand-color-active) 100%);
    display: flex;
    align-items: center;
    justify-content: center;

    img {
      width: 100%;
      height: 100%;
      object-fit: cover;
    }

    .dropdown-user-avatar-placeholder {
      color: var(--td-text-color-anti);
      font-size: 12px;
      font-weight: 600;
      line-height: 1;
    }
  }

  .dropdown-user-meta {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0;
    justify-content: center;
  }

  .dropdown-user-name-row {
    display: flex;
    align-items: center;
    gap: 2px;
    min-width: 0;
  }

  .dropdown-user-name {
    flex: 1;
    min-width: 0;
    font-size: 14px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    line-height: 1.35;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .dropdown-user-email {
    min-width: 0;
    font-size: 12px;
    line-height: 1.35;
    color: var(--td-text-color-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

}

// 下拉 — 当前工作区：与下方 .menu-item 同款对齐（左 16px 图标槽 + 文案列 + 右侧操作图标）
.dropdown-space-panel {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  border-top: 1px solid var(--td-component-stroke);
  background: transparent;
  transition: background 0.15s ease;
  min-width: 0;

  >.menu-icon {
    font-size: 16px;
    color: var(--td-text-color-secondary);
    flex-shrink: 0;
  }

  &.is-clickable {
    cursor: pointer;

    &:hover,
    &.is-open {
      background: var(--td-bg-color-container-hover);

      .dropdown-space-panel-trail {
        color: var(--td-text-color-secondary);
      }
    }
  }

  .dropdown-space-panel-main {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .dropdown-space-panel-name {
    font-size: 14px;
    font-weight: 500;
    color: var(--td-text-color-primary);
    line-height: 1.35;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .dropdown-space-panel-trail {
    flex-shrink: 0;
    font-size: 16px;
    color: var(--td-text-color-placeholder);
    transition: color 0.15s ease;
  }

  .dropdown-space-panel-role {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    line-height: 1.35;
    color: var(--td-text-color-secondary);
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;

    .dropdown-space-panel-role-icon {
      flex-shrink: 0;
      color: inherit;
    }
  }
}

.menu-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  cursor: pointer;
  transition: all 0.2s;
  font-size: 14px;
  color: var(--td-text-color-primary);

  &:hover {
    background: var(--td-bg-color-container-hover);
  }

  // 包含右弹子菜单的菜单项
  &--submenu {
    position: relative;

    .menu-item-label {
      flex: 1;
    }

    .menu-chevron {
      font-size: 16px;
      color: var(--td-text-color-placeholder);
      flex-shrink: 0;
      transition: transform 0.15s;
    }

    &.is-open {
      background: var(--td-bg-color-container-hover);

      .menu-chevron {
        color: var(--td-text-color-secondary);
      }
    }
  }

  .menu-icon {
    font-size: 16px;
    color: var(--td-text-color-secondary);

    &.svg-icon {
      width: 16px;
      height: 16px;
      flex-shrink: 0;
    }

    &--emoji {
      width: 16px;
      height: 16px;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      font-size: 15px;
      line-height: 1;
      flex-shrink: 0;
      color: inherit;
    }
  }

  .menu-text-with-icon {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 6px;
    color: inherit;
    min-width: 0;

    >span:first-of-type {
      display: inline-flex;
      align-items: center;
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
  }

  .menu-new-badge {
    flex-shrink: 0;
    font-size: 10px;
    font-weight: 600;
    line-height: 1.2;
    padding: 2px 5px;
    border-radius: 4px;
    background: var(--td-brand-color-light);
    color: var(--td-brand-color);
    letter-spacing: 0.02em;
  }

  .menu-github-star-icon {
    flex-shrink: 0;
    color: var(--td-warning-color);
  }

  .menu-external-icon {
    width: 16px;
    height: 16px;
    color: var(--td-text-color-disabled);
    flex-shrink: 0;
    transition: color 0.2s ease;
    pointer-events: none;
  }

  &:hover .menu-external-icon {
    color: var(--td-brand-color);
  }
}

.menu-divider {
  height: 1px;
  background: var(--td-component-stroke);
  margin: 3px 0;
}

// 紧跟账号/空间区块后的分隔线：略收紧与上方的留白
.dropdown-user-header+.menu-divider,
.dropdown-space-panel+.menu-divider {
  margin-top: 1px;
}

// 下拉动画
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: translateY(8px);
}

.dropdown-enter-to,
.dropdown-leave-from {
  opacity: 1;
  transform: translateY(0);
}

</style>
