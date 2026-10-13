<template>
    <div class="main">
        <Menu v-if="!compactNavigation" />
        <div v-else class="mobile-navigation-bar">
            <t-button variant="text" :aria-label="t('menu.expandSidebar')" @click="mobileSidebarOpen = true">
                <template #icon><t-icon name="menu" /></template>
                {{ t('menu.expandSidebar') }}
            </t-button>
        </div>
        <t-drawer v-if="compactNavigation" v-model:visible="mobileSidebarOpen" placement="left"
            :header="t('menu.knowledgeBase')" :footer="false" size="min(280px, 90vw)"
            drawer-class-name="platform-mobile-navigation">
            <Menu mobile-overlay @close="mobileSidebarOpen = false" />
        </t-drawer>
        <div v-if="isRouterAlive" class="platform-route-outlet">
            <RouterView />
        </div>
        <!-- 全局设置模态框，供所有 platform 子路由使用 -->
        <Settings />
        <!-- 全局命令面板 (⌘K)，随 platform 路由存活 -->
        <GlobalCommandPalette />
    </div>
</template>
<script setup lang="ts">
import Menu from '@/components/SideMenu.vue'
import { ref, onMounted, onUnmounted, nextTick, provide, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router'
import Settings from '@/views/settings/Settings.vue'
import GlobalCommandPalette from '@/components/GlobalCommandPalette.vue'
import { useCommandPaletteStore } from '@/stores/commandPalette'
import { useI18n } from 'vue-i18n'

const route = useRoute();
const router = useRouter();
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
    delete (newQuery as any).cmdk
    router.replace({ path: route.path, query: newQuery, hash: route.hash })
}

// 组件卸载时移除全局事件监听器
onUnmounted(() => {
    navigationMedia.removeEventListener('change', updateNavigationLayout);
});
</script>
<style lang="less">
.main {
    display: flex;
    align-items: stretch;
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
    /* 统一整页背景，让左侧菜单与右侧内容区视觉连贯 */
    background: var(--td-bg-color-container);
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

.mobile-navigation-bar {
    flex-shrink: 0;
    padding: 6px 12px;
    border-bottom: 1px solid var(--td-component-stroke);
}

.platform-mobile-navigation {
    .t-drawer__body {
        padding: 0;
        overflow: hidden;
    }

    .aside_box {
        width: 100%;
        min-width: 0;
        height: 100%;
        border-right: 0;
    }
}

@media (max-width: 640px) {
    .main {
        flex-direction: column;
        overflow: hidden;
    }
}

img {
    -webkit-user-drag: none;
    -khtml-user-drag: none;
    -moz-user-drag: none;
    -o-user-drag: none;
    user-drag: none;
}
</style>
