<template>
  <aside class="aside_box" :class="{ 'aside_box--collapsed': sidebarCollapsed }">
    <div class="logo_row">
      <button v-if="!sidebarCollapsed" type="button" class="logo_box" :aria-label="t('menu.library')" @click="navigate('entries')">
        <img class="logo" src="@/assets/img/reliquary.svg" alt="Reliquary">
      </button>
      <t-tooltip :content="t(sidebarCollapsed ? 'menu.expandSidebar' : 'menu.collapseSidebar')" placement="right">
        <button type="button" class="header-icon-btn" :aria-label="t(sidebarCollapsed ? 'menu.expandSidebar' : 'menu.collapseSidebar')" @click="toggleNavigation">
          <t-icon :name="sidebarCollapsed ? 'chevron-right' : 'chevron-left'" size="18px" />
        </button>
      </t-tooltip>
    </div>
    <nav class="menu_top" :aria-label="t('menu.library')">
      <section v-for="group in menuGroups" :key="group.key" class="menu-group" :aria-label="group.title"><h2 v-if="!sidebarCollapsed" class="menu-group-title">{{ group.title }}</h2>
      <t-tooltip v-for="item in group.items" :key="item.key" :content="item.title" placement="right" :disabled="!sidebarCollapsed">
        <button type="button" :class="['menu_item', { menu_item_active: active(item.path, item.tab) }]" :aria-label="item.title" :aria-current="active(item.path, item.tab) ? 'page' : undefined" @click="navigate(item.path, item.tab)">
          <t-icon :name="item.icon" size="20px" /><span v-if="!sidebarCollapsed" class="menu_title">{{ item.title }}</span>
        </button>
      </t-tooltip>
      </section>
    </nav>
    <div class="menu_bottom"><SettingsMenu :collapsed="sidebarCollapsed" @open="navigate('settings')" /></div>
  </aside>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useMenuStore } from '@/stores/menu'
import { useUIStore } from '@/stores/ui'
import SettingsMenu from '@/components/SettingsMenu.vue'
const props = defineProps<{ mobileOverlay?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const route = useRoute(), router = useRouter()
const uiStore = useUIStore()
const { menuGroups } = storeToRefs(useMenuStore())
const sidebarCollapsed = computed(() => !props.mobileOverlay && uiStore.sidebarCollapsed)
const active = (path: string, tab?: string) => path === 'entries'
  ? route.name === 'entries' && (tab ? route.query.tab === tab : !['entities', 'reviews', 'domains'].includes(String(route.query.tab)))
  : route.path === `/platform/${path}` || path === 'stories' && route.name === 'stories'
function toggleNavigation() { if (props.mobileOverlay) emit('close'); else uiStore.toggleSidebar() }
function navigate(path: string, tab?: string) {
  if (path === 'settings') uiStore.openSettings()
  void router.push({ path: `/platform/${path}`, query: tab ? { tab } : {} })
  if (props.mobileOverlay) emit('close')
}
</script>
<style lang="less" scoped>
.aside_box { display: flex; flex-direction: column; width: 240px; min-width: 240px; height: 100%; padding: 20px 12px 12px; box-sizing: border-box; background: var(--td-bg-color-container); border-right: 1px solid var(--td-component-stroke); transition: width .2s, min-width .2s; }
.aside_box--collapsed { width: 64px; min-width: 64px; padding-inline: 8px; }
.logo_row { display: flex; align-items: center; justify-content: space-between; gap: 8px; min-height: 36px; margin-bottom: 20px; }
.menu-group + .menu-group { margin-top: 16px; }
.menu-group-title { margin: 0 12px 8px; font-size: 12px; font-weight: 500; color: var(--td-text-color-secondary); }
.logo_box, .header-icon-btn, .menu_item { border: 0; background: transparent; color: var(--td-text-color-primary); cursor: pointer; border-radius: var(--td-radius-medium); }
.logo_box { padding: 0; display: flex; min-width: 0; }
.logo { max-width: 144px; height: 28px; }
:root[theme-mode="dark"] .logo { filter: invert(1); }
.header-icon-btn { display: grid; place-items: center; width: 36px; height: 36px; flex-shrink: 0; }
.menu_top { flex: 1; min-height: 0; overflow: auto; }
.menu_item { display: flex; align-items: center; gap: 12px; position: relative; width: 100%; min-height: 44px; margin-bottom: 8px; padding: 10px 12px; text-align: left; }
.menu_title { min-width: 0; overflow-wrap: anywhere; font-size: 14px; }
.menu_item:hover, .header-icon-btn:hover { background: var(--td-bg-color-container-hover); }
.menu_item_active { color: var(--td-brand-color); background: var(--td-brand-color-light); }
button:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
.menu_bottom { flex-shrink: 0; padding-top: 12px; border-top: 1px solid var(--td-component-stroke); }
@media (prefers-reduced-motion: reduce) { .aside_box { transition: none; } }
</style>
