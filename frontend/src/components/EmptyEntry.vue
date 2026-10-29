<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>
<template>
  <div class="empty">
    <t-icon class="empty-icon" name="file" aria-hidden="true" />
    <p class="empty-title">{{ t('library.emptyArticles') }}</p>
    <p class="empty-hint">{{ t('library.ingestOnlyHint') }}</p>
  </div>
</template>
<style scoped lang="less">
.empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  padding: 24px;
  text-align: center;
}
.empty-icon { font-size: 64px; color: var(--td-text-color-placeholder); }
.empty-title { margin: 16px 0 8px; color: var(--td-text-color-primary); font-size: 16px; font-weight: 600; }
.empty-hint { margin: 0; max-width: 560px; color: var(--td-text-color-secondary); font-size: 14px; overflow-wrap: anywhere; }
</style>
