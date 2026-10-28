<template>
  <t-tooltip :content="t('menu.settings')" placement="right" :disabled="!collapsed">
    <button type="button" class="settings-button" :class="{ 'settings-button--collapsed': collapsed }"
      :aria-label="t('menu.settings')" @click="openSettings">
      <t-icon name="setting" size="20px" />
      <span v-if="!collapsed">{{ t('menu.settings') }}</span>
    </button>
  </t-tooltip>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useUIStore } from '@/stores/ui'

defineProps<{ collapsed?: boolean }>()
const emit = defineEmits<{ open: [] }>()
const { t } = useI18n()
const router = useRouter()
const uiStore = useUIStore()

function openSettings() {
  uiStore.openSettings()
  void router.push('/platform/settings')
  emit('open')
}
</script>

<style scoped>
.settings-button { display: flex; align-items: center; gap: 12px; width: 100%; min-height: 44px; padding: 10px 12px; border: 0; border-radius: var(--td-radius-medium); background: transparent; color: var(--td-text-color-primary); font: inherit; text-align: left; cursor: pointer; }
.settings-button--collapsed { justify-content: center; }
.settings-button:hover { background: var(--td-bg-color-container-hover); }
.settings-button:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
</style>
