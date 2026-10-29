<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePendingCountsStore } from '@/stores/pendingCounts'

const { t, n } = useI18n()
const store = usePendingCountsStore()
onMounted(() => { void store.refresh() })
</script>

<template>
  <span v-if="store.total !== null" class="pending-count" :aria-label="t('ingestStatus.navCount', { count: store.total })">{{ n(store.total) }}</span>
</template>

<style scoped lang="less">
.pending-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  font: var(--text-xs)/var(--leading) var(--mono);
  opacity: 0.65;
}
</style>
