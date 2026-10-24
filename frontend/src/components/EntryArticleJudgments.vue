<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import EntryStarToggle from './EntryStarToggle.vue'
import type { ClassifiedEntry } from '@/types/entryClassification'
defineProps<{ entry: ClassifiedEntry & { id: string }; disabled?: boolean }>()
const emit = defineEmits<{ updated: [value: any] }>()
const { t } = useI18n()
</script>

<template>
  <section class="article-judgments" :aria-label="t('articleJudgments.title')">
    <span>{{ t('articleJudgments.starLabel') }}</span>
    <EntryStarToggle :entry="entry" :disabled="disabled" @updated="emit('updated', $event)" />
  </section>
</template>

<style scoped lang="less">
.article-judgments { display: flex; align-items: center; gap: 12px; padding-top: 16px; margin-top: 16px; border-top: 1px solid var(--td-component-stroke); color: var(--td-text-color-secondary); font-size: 13px; }
</style>
