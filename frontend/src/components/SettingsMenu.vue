<template>
  <button type="button" class="settings-trigger" :class="{ 'is-collapsed': collapsed }"
    :aria-label="$t('menu.settings')" @click="emit('open')">
    <t-icon name="setting" aria-hidden="true" />
    <span v-if="!collapsed">{{ $t('menu.settings') }}</span>
  </button>
</template>

<script setup lang="ts">
defineProps<{ collapsed?: boolean }>()
const emit = defineEmits<{ open: [] }>()
</script>

<style scoped lang="less">
.settings-trigger {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  border: 0;
  border-radius: var(--td-radius-default);
  padding: 9px 12px;
  color: var(--td-text-color-primary);
  background: transparent;
  font: inherit;
  text-align: left;
  cursor: pointer;

  &:hover { background: var(--td-bg-color-container-hover); }
  &:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: -2px; }
  .t-icon { flex-shrink: 0; font-size: 16px; }
}
.is-collapsed {
  justify-content: center;
  padding: 9px 3px;
}
</style>
