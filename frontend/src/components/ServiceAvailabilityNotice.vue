<template>
  <Teleport to="body">
  <div v-if="serviceUnavailable" class="service-availability-notice" role="alert" aria-live="polite">
    {{ t('error.serviceUnavailable') }}
  </div>
  </Teleport>
</template>
<script setup lang="ts">
import { onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { serviceUnavailable, stopServiceAvailabilityRetry } from '@/utils/request'
const { t } = useI18n()
onUnmounted(stopServiceAvailabilityRetry)
</script>
<style scoped>
.service-availability-notice { position: fixed; top: 8px; left: 50%; transform: translateX(-50%); z-index: 10000; padding: 10px 18px; border: 1px solid var(--td-warning-color); border-radius: 8px; background: var(--td-warning-color-1); color: var(--td-text-color-primary); max-width: calc(100% - 32px); }
</style>
