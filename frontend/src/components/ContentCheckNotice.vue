<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { checkRuns } from '@/api/content-check'
const route = useRoute(), { t } = useI18n(), failedID = ref('')
let disposed = false, timer: ReturnType<typeof setTimeout> | undefined
async function refresh() {
  try { const page = await checkRuns(1); if (!disposed) failedID.value = page.items[0]?.status === 'failed' ? page.items[0].id : '' }
  catch { /* The diagnostic page exposes retrieval errors and a retry action. */ }
  if (!disposed) timer = setTimeout(refresh, 60000)
}
onMounted(refresh)
onUnmounted(() => { disposed = true; clearTimeout(timer) })
</script>
<template><div v-if="failedID && route.path !== '/platform/content-checks'" class="check-notice" role="alert"><router-link :to="{ path: '/platform/content-checks', query: { run: failedID } }">{{ t('contentCheck.notice') }}</router-link></div></template>
<style scoped>
.check-notice { flex-shrink: 0; padding: 10px 16px; border-bottom: 1px solid var(--td-warning-color); color: var(--td-text-color-primary); background: var(--td-bg-color-container); }
a { color: var(--td-warning-color); cursor: pointer; } a:hover { text-decoration: underline; } a:focus-visible { outline: 2px solid currentColor; }
</style>
