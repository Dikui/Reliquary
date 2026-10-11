<template>
  <div v-if="connection?.state === 'reconnecting'" class="queue-connection-notice" role="alert">
    <strong>{{ t(connection.alert ? 'system.globalSettings.runtime.reconnectAlert' : 'system.globalSettings.runtime.reconnecting') }}</strong>
    <span>{{ t('system.globalSettings.runtime.latestConnectionError') }}: {{ connection.last_error }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getQueueConnectionStatus, type QueueConnectionStatus } from '@/api/system'

const props = defineProps<{ status?: QueueConnectionStatus; poll?: boolean }>()
const { t } = useI18n()
const fetched = ref<QueueConnectionStatus>()
const connection = computed(() => props.status ?? fetched.value)
let timer: ReturnType<typeof setInterval> | undefined
async function refresh() {
  try { fetched.value = await getQueueConnectionStatus() } catch { /* Retain the last known connection state. */ }
}
onMounted(() => {
  if (props.poll) { refresh(); timer = setInterval(refresh, 5000) }
})
onUnmounted(() => { if (timer) clearInterval(timer) })
</script>

<style scoped>
.queue-connection-notice { display: flex; flex-direction: column; gap: 8px; padding: 16px; margin-bottom: 16px; border: 1px solid var(--td-warning-color); border-radius: 8px; overflow-wrap: anywhere; color: var(--td-text-color-primary); background: var(--td-warning-color-1); }
</style>
