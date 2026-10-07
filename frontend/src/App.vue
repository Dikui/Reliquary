<script setup lang="ts">
import { computed, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ManualKnowledgeEditor from '@/components/manual-knowledge-editor.vue'
import UploadConfirmHost from '@/components/UploadConfirmHost.vue'
import { useAuthStore } from '@/stores/auth'

// TDesign locale configs
import enUSConfig from 'tdesign-vue-next/esm/locale/en_US'
import zhCNConfig from 'tdesign-vue-next/esm/locale/zh_CN'

const { locale } = useI18n()
const authStore = useAuthStore()

const tdLocaleMap: Record<string, object> = {
  'en-US': enUSConfig,
  'zh-CN': zhCNConfig,
}

const tdGlobalConfig = computed(() => tdLocaleMap[locale.value] || enUSConfig)

// Pending invitations poll: fires once on mount (logged-in case) and
// then every 2 minutes. Light enough to keep the avatar-row badge
// near-live without slamming the API, and avoids the cost of a
// dedicated SSE/WebSocket connection. Stopped on logout via the
// computed below.
let invitationPollTimer: ReturnType<typeof setInterval> | null = null
const INVITATION_POLL_INTERVAL_MS = 2 * 60 * 1000

const startInvitationPolling = () => {
  if (invitationPollTimer || !authStore.isLoggedIn) return
  // Immediate fetch so the badge is correct before the first tick.
  authStore.fetchPendingInvitationCount()
  invitationPollTimer = setInterval(() => {
    if (!authStore.isLoggedIn) return
    authStore.fetchPendingInvitationCount()
  }, INVITATION_POLL_INTERVAL_MS)
}

const stopInvitationPolling = () => {
  if (invitationPollTimer) {
    clearInterval(invitationPollTimer)
    invitationPollTimer = null
  }
}

// Keep invitation polling aligned with the current session.
watch(
  () => authStore.isLoggedIn,
  (logged) => {
    if (logged) startInvitationPolling()
    else stopInvitationPolling()
  },
  { immediate: true },
)

onUnmounted(() => {
  stopInvitationPolling()
})

</script>
<template>
  <t-config-provider :globalConfig="tdGlobalConfig">
    <div id="app">
      <RouterView />
      <ManualKnowledgeEditor />
      <UploadConfirmHost />
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
