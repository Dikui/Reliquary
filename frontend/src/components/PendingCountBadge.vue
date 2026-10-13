<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePendingCountsStore } from '@/stores/pendingCounts'

const { t } = useI18n()
const store = usePendingCountsStore()
onMounted(() => { void store.refresh() })
</script>

<template>
  <span v-if="store.total !== null" class="pending-count" :aria-label="t('ingestStatus.navCount', { count: store.total })">{{ store.total }}</span>
</template>

<style scoped lang="less">
.pending-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  padding-inline: 4px;
  border-radius: var(--td-radius-small);
  background: var(--td-bg-color-secondarycontainer);
  font-size: 12px;
  line-height: 18px;
  font-variant-numeric: tabular-nums;
}
</style>
