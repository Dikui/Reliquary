<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SearchFilters, SearchOptions } from '@/types/search'

const model = defineModel<SearchFilters>({ required: true })
const props = withDefaults(defineProps<{ options?: SearchOptions; loading?: boolean; disabled?: boolean }>(), {
  options: () => ({ entities: [] }), loading: false, disabled: false,
})
const { t } = useI18n()
const starOptions = computed(() => [
  { value: '', label: t('articleJudgments.allStars') },
  { value: 'true', label: t('articleJudgments.starredOnly') },
  { value: 'false', label: t('articleJudgments.unstarredOnly') },
])
const starFilter = computed({ get: () => typeof model.value.starred === 'boolean' ? String(model.value.starred) : '', set: value => { model.value.starred = value === '' ? undefined : value === 'true' } })
</script>

<template>
  <div class="search-filters">
    <label class="filter-field"><span>{{ t('articleJudgments.starLabel') }}</span><t-select v-model="starFilter" :options="starOptions" :disabled="disabled" :aria-label="t('articleJudgments.starLabel')" /></label>
    <label class="filter-field"><span>{{ t('knowledgeSearch.publishedFrom') }}</span><t-date-picker v-model="model.published_from" clearable allow-input format="YYYY-MM-DD" value-type="YYYY-MM-DD" :disabled="disabled" :aria-label="t('knowledgeSearch.publishedFrom')" :popup-props="{ zIndex: 6200 }" /></label>
    <label class="filter-field"><span>{{ t('knowledgeSearch.publishedTo') }}</span><t-date-picker v-model="model.published_to" clearable allow-input format="YYYY-MM-DD" value-type="YYYY-MM-DD" :disabled="disabled" :aria-label="t('knowledgeSearch.publishedTo')" :popup-props="{ zIndex: 6200 }" /></label>
    <label class="filter-field"><span>{{ t('knowledgeSearch.limit') }}</span><t-input-number v-model="model.limit" :min="1" :max="100" :step="1" :decimal-places="0" :disabled="disabled" :aria-label="t('knowledgeSearch.limit')" /></label>
    <p class="filter-hint">{{ t('knowledgeSearch.filterHint') }}</p>
  </div>
</template>

<style scoped lang="less">
.search-filters { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px 16px; }
.filter-field { display: flex; flex-direction: column; gap: 6px; min-width: 0; font-size: 13px; color: var(--td-text-color-secondary); }
.filter-field :deep(.t-date-picker), .filter-field :deep(.t-input-number) { width: 100%; }
.filter-hint { grid-column: 1 / -1; margin: 0; color: var(--td-text-color-secondary); font-size: 12px; line-height: 1.6; }
@media (max-width: 560px) { .search-filters { grid-template-columns: minmax(0, 1fr); } }
</style>
