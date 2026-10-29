<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ hasUpdate?: boolean; latestEntryId?: string }>()
const { t } = useI18n()
</script>

<template>
  <span v-if="hasUpdate" class="entry-update-notice">
    <a v-if="latestEntryId" :href="`/platform/entries?entry_id=${encodeURIComponent(latestEntryId)}`" @click.stop @keydown.enter.stop @keydown.space.stop>{{ t('ikb18.hasUpdate') }}</a>
    <span v-else>{{ t('ikb18.hasUpdate') }}</span>
  </span>
</template>

<style scoped>
.entry-update-notice { display: inline-block; flex-shrink: 0; font-size: 12px; color: var(--td-warning-color); }
a { color: inherit; text-decoration: underline; }
a:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
</style>
