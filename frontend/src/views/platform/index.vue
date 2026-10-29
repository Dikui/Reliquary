<template>
    <div class="rq-platform">
        <Menu v-if="!compactNavigation" />
        <header v-else class="rq-mobile-navigation">
            <RouterLink class="rq-mobile-navigation__brand" to="/platform/libraries" aria-current-value="false"><span aria-hidden="true" />Reliquary</RouterLink>
            <Drawer v-model="mobileSidebarOpen" placement="left" :title="t('menu.mainNavigation')"
                content-class="rq-navigation-drawer" @close-auto-focus="commandPaletteStore.open && $event.preventDefault()">
                <template #trigger>
                    <button type="button" :aria-label="t('menu.expandSidebar')"><svg aria-hidden="true" width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor"><path d="M3 5h14M3 10h14M3 15h14" /></svg></button>
                </template>
                <Menu mobile-overlay @close="mobileSidebarOpen = false" />
            </Drawer>
            <button type="button" :aria-label="t('menu.commandSearch')" @click="commandPaletteStore.openPalette()"><svg aria-hidden="true" width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor"><circle cx="8" cy="8" r="5" /><path d="m12 12 5 5" /></svg></button>
        </header>
        <div v-if="isRouterAlive" class="platform-route-outlet">
            <ContentCheckNotice />
            <RouterView />
        </div>
        <!-- 全局命令面板 (⌘K)，随 platform 路由存活 -->
        <GlobalCommandPalette />
    </div>
</template>
<script setup lang="ts">
import Menu from '@/components/SideMenu.vue'
import ContentCheckNotice from '@/components/ContentCheckNotice.vue'
import { ref, onMounted, onUnmounted, nextTick, provide, watch } from 'vue';
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { Drawer } from '@/components/ui'
import { connectSettingsNavigation } from '@/config/settingsNavigation'
import { useUIStore } from '@/stores/ui'
import GlobalCommandPalette from '@/components/GlobalCommandPalette.vue'
import { useCommandPaletteStore } from '@/stores/commandPalette'
import { useI18n } from 'vue-i18n'

const route = useRoute();
const router = useRouter();
const disconnectSettings = connectSettingsNavigation(router, useUIStore());
const commandPaletteStore = useCommandPaletteStore();
const { t } = useI18n();
const navigationMedia = window.matchMedia('(max-width: 640px)');
const compactNavigation = ref(navigationMedia.matches);
const mobileSidebarOpen = ref(false);
const updateNavigationLayout = (event: MediaQueryListEvent) => {
    compactNavigation.value = event.matches;
    mobileSidebarOpen.value = false;
};
watch(() => route.fullPath, () => { mobileSidebarOpen.value = false; });
watch(() => commandPaletteStore.open, open => { if (open) mobileSidebarOpen.value = false; });

const isRouterAlive = ref(true)
const reloadApp = () => {
    isRouterAlive.value = false
    nextTick(() => {
        isRouterAlive.value = true
    })
}
provide('app:reload', reloadApp)

// 组件挂载时添加全局事件监听器
onMounted(() => {
    navigationMedia.addEventListener('change', updateNavigationLayout);
    // 支持通过 URL 查询参数打开全局命令面板，例如旧路径
    // /platform/search?q=foo 重定向后携带 ?cmdk=foo
    maybeOpenCmdkFromRoute()
});

// 监听路由变化，兼容 SPA 内部跳转时的 ?cmdk= 参数
watch(() => route.query.cmdk, () => {
    maybeOpenCmdkFromRoute()
})

function maybeOpenCmdkFromRoute() {
    if (!('cmdk' in route.query)) return
    const q = String(route.query.cmdk ?? '')
    commandPaletteStore.openPalette(q)
    // 清除 query，避免回退/刷新时反复触发
    const newQuery = { ...route.query }
    delete newQuery.cmdk
    router.replace({ path: route.path, query: newQuery, hash: route.hash })
}

// 组件卸载时移除全局事件监听器
onUnmounted(() => {
    disconnectSettings();
    navigationMedia.removeEventListener('change', updateNavigationLayout);
});
</script>
<style lang="less">
.rq-platform {
    display: flex;
    align-items: stretch;
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
    background: var(--surface);
}

/* 右侧路由区：占满剩余宽度与整列高度，并把 min-height:0 传给子页面以便内部 flex 滚动 */
.platform-route-outlet {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.rq-mobile-navigation {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-shrink: 0;
    padding: var(--space-2) var(--space-4);
    border-bottom: 2px solid var(--rule);
    background: var(--surface);
    color: var(--text);
    font-family: var(--font);
}
.rq-mobile-navigation__brand { display: flex; align-items: center; gap: var(--space-2); margin-right: auto; color: inherit; font-size: var(--text-lg); font-weight: 800; letter-spacing: -0.035em; text-decoration: none; }
.rq-mobile-navigation__brand span { width: 14.4px; height: 14.4px; background: var(--accent); }
.rq-mobile-navigation button { display: grid; place-items: center; width: var(--control); height: var(--control); border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); color: var(--text); cursor: pointer; }
.rq-mobile-navigation button:hover { background: var(--surface-soft); }
.rq-mobile-navigation :is(a, button):focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.rq-navigation-drawer {
    width: min(90vw, 320px);
    padding: var(--space-4) 0 0;
    gap: 0;
    .rq-dialog__header { padding: 0 var(--space-4) var(--space-3); }
    .rq-dialog__body { flex: 1; overflow: hidden; }
}

@media (max-width: 640px) {
    .rq-platform {
        flex-direction: column;
        overflow: hidden;
    }
}

</style>
