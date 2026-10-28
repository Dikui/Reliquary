<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ReparseConfirmHost from '@/components/ReparseConfirmHost.vue'
import ServiceAvailabilityNotice from '@/components/ServiceAvailabilityNotice.vue'
import FeedbackHost from '@/components/ui/FeedbackHost.vue'

// TDesign locale configs
import enUSConfig from 'tdesign-vue-next/esm/locale/en_US'
import zhCNConfig from 'tdesign-vue-next/esm/locale/zh_CN'

const { locale } = useI18n()

const tdLocaleMap: Record<string, object> = {
  'en-US': enUSConfig,
  'zh-CN': zhCNConfig,
}

const tdGlobalConfig = computed(() => tdLocaleMap[locale.value] || enUSConfig)

</script>
<template>
  <t-config-provider :globalConfig="tdGlobalConfig">
    <div id="app">
      <ServiceAvailabilityNotice />
      <RouterView />
      <ReparseConfirmHost />
      <FeedbackHost />
    </div>
  </t-config-provider>
</template>
<style>
html {
  /* 提示 UA 使用对应配色绘制滚动条等，减少主题切换时的额外重绘 */
  color-scheme: light dark;
}

body,
html,
#app {
  width: 100%;
  height: 100%;
  margin: 0;
  padding: 0;
  font-size: 14px;
  font-family: var(--app-font-family);
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  background: var(--td-bg-color-page);
  color: var(--td-text-color-primary);
}

#app {
  /* 独立合成层，减轻 WebKit 全量重绘时整窗与内容的撕裂感（桌面 WebView 尤其明显） */
  isolation: isolate;
  transform: translateZ(0);
  backface-visibility: hidden;
}
</style>
