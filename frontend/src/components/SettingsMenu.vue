<template>
  <div ref="menuRef" class="settings-menu" :class="{ 'is-collapsed': collapsed }" @keydown.esc="closeMenu">
    <button ref="trigger" type="button" class="settings-trigger" :aria-label="$t('menu.settings')"
      :aria-expanded="menuVisible" @click="menuVisible = !menuVisible">
      <t-icon name="setting" aria-hidden="true" />
      <span v-if="!collapsed">{{ $t('menu.settings') }}</span>
      <t-icon v-if="!collapsed" :name="menuVisible ? 'chevron-up' : 'chevron-down'" class="chevron" aria-hidden="true" />
    </button>
    <div v-if="menuVisible" class="settings-dropdown">
      <button v-for="item in items" :key="item.section" type="button" @click="navigate(item.section)">
        <t-icon :name="item.icon" aria-hidden="true" />
        <span>{{ item.label }}</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useUIStore } from '@/stores/ui'

defineProps<{ collapsed?: boolean }>()
const { t } = useI18n()
const router = useRouter()
const uiStore = useUIStore()
const menuRef = ref<HTMLElement>()
const trigger = ref<HTMLButtonElement>()
const menuVisible = ref(false)
const items = computed(() => [
  { section: 'general', icon: 'setting', label: t('general.title') },
  { section: 'space', icon: 'system-sum', label: t('settings.workspaceSettings') },
  { section: 'models', icon: 'control-platform', label: t('settings.modelManagement') },
  { section: '', icon: 'app', label: t('general.allSettings') },
  { section: 'system-global', icon: 'server', label: t('settings.navGroups.systemAdministration') },
])

function closeMenu() {
  menuVisible.value = false
  trigger.value?.focus()
}

function navigate(section: string) {
  closeMenu()
  uiStore.openSettings(section || undefined)
  router.push({ path: '/platform/settings', query: section ? { section } : {} })
}

function onOutsideClick(event: MouseEvent) {
  if (!menuRef.value?.contains(event.target as Node)) menuVisible.value = false
}

onMounted(() => document.addEventListener('click', onOutsideClick))
onUnmounted(() => document.removeEventListener('click', onOutsideClick))
</script>

<style scoped lang="less">
.settings-menu { position: relative; width: 100%; }
.settings-trigger,
.settings-dropdown button {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  border: 0;
  border-radius: var(--td-radius-default);
  padding: 9px 12px;
  color: var(--td-text-color-primary);
  background: transparent;
  font: inherit;
  text-align: left;
  cursor: pointer;

  &:hover { background: var(--td-bg-color-container-hover); }
  &:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: -2px; }
  .t-icon { flex-shrink: 0; font-size: 16px; }
}
.chevron { margin-left: auto; }
.settings-dropdown {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 0;
  right: 0;
  z-index: 1000;
  padding: 4px;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--td-radius-default);
  background: var(--td-bg-color-container);
  box-shadow: var(--td-shadow-2);
}
.is-collapsed {
  .settings-trigger { justify-content: center; padding: 9px 3px; }
  .settings-dropdown { left: calc(100% + 8px); right: auto; min-width: 220px; }
}
</style>
