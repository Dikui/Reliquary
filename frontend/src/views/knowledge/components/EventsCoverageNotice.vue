<template>
  <div class="events-coverage-notice" role="status">
    <p>{{ t('knowledgeWorkbench.events.coverageNotice') }}</p>
    <p v-if="loading">{{ t('knowledgeWorkbench.events.countLoading') }}</p>
    <p v-else-if="count === null || count === undefined">{{ t('knowledgeWorkbench.events.countUnavailable') }}</p>
    <p v-else>{{ t('knowledgeWorkbench.events.templateOnlyCount', { count }) }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
withDefaults(defineProps<{ count: number | null; loading?: boolean }>(), { loading: false })
const { t } = useI18n()
</script>

<style scoped lang="less">
.events-coverage-notice { color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.6; margin: 12px 0; overflow-wrap: anywhere; }
.events-coverage-notice p { margin: 4px 0; }
</style>
