<script setup lang="ts">
import { contentTypeLabel } from '@/utils/contentTypeLabel'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { CONTENT_TYPES } from '@/types/entryClassification'
import type { SearchFilters, SearchOptions } from '@/types/search'
import { Button, Checkbox, Select } from '@/components/ui'
import { defaultSearchFilters } from '@/utils/search'

const model = defineModel<SearchFilters>({ required: true })
const props = withDefaults(defineProps<{ options?: SearchOptions; loading?: boolean; disabled?: boolean }>(), {
  options: () => ({ entities: [], topics: [] }), loading: false, disabled: false,
})
const { t } = useI18n()
const contentTypeOptions = computed(() => props.options.content_types?.map(value => ({ value, label: contentTypeLabel(value, t) })) || CONTENT_TYPES.map(value => ({ value, label: t(`library.contentTypes.${value}`) })))
const entityOptions = computed(() => props.options.entities.map(entity => ({ value: entity.id, label: entity.library_id ? `${entity.canonical_name} · ${entity.library_id}` : entity.canonical_name })))
const topicOptions = computed(() => props.options.topics.map(topic => ({ value: topic.id, label: topic.library_id ? `${topic.name} · ${topic.library_id}` : topic.name })))
const relatedFilters = computed(() => [
  { key: 'entity_ids' as const, label: t('knowledgeSearch.entities'), options: entityOptions.value, empty: t('searchPage.noEntities'), placeholder: t('searchPage.selectEntity') },
  { key: 'topic_ids' as const, label: t('knowledgeSearch.topics'), options: topicOptions.value, empty: t('searchPage.noTopics'), placeholder: t('searchPage.selectTopic') },
])
const defaults = defaultSearchFilters()
const hasActiveFilters = computed(() => model.value.content_types.length > 0 || model.value.entity_ids.length > 0 || model.value.topic_ids.length > 0
  || !!model.value.published_from || !!model.value.published_to || model.value.starred !== undefined
  || model.value.collapse_similar !== defaults.collapse_similar || model.value.limit !== defaults.limit)
function addRelatedFilter(key: 'entity_ids' | 'topic_ids', event: Event) {
  const select = event.target as HTMLSelectElement
  if (select.value && !model.value[key].includes(select.value)) model.value[key] = [...model.value[key], select.value]
  select.value = ''
}
function removeRelatedFilter(key: 'entity_ids' | 'topic_ids', id: string) { model.value[key] = model.value[key].filter(value => value !== id) }
const starOptions = computed(() => [
  { value: '', label: t('articleJudgments.allStars') },
  { value: 'true', label: t('articleJudgments.starredOnly') },
  { value: 'false', label: t('articleJudgments.unstarredOnly') },
])
const starFilter = computed({ get: () => typeof model.value.starred === 'boolean' ? String(model.value.starred) : '', set: value => { model.value.starred = value === '' ? undefined : value === 'true' } })
function dateBefore(days: number) {
  const date = new Date()
  date.setDate(date.getDate() - days)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}
const customTime = ref(false)
const timeRange = computed({
  get: () => {
    if (customTime.value) return 'custom'
    if (!model.value.published_from && !model.value.published_to) return 'any'
    for (const days of [7, 30]) if (model.value.published_from === dateBefore(days - 1) && model.value.published_to === dateBefore(0)) return String(days)
    return 'custom'
  },
  set: value => {
    customTime.value = value === 'custom'
    if (value === 'custom') return
    model.value.published_from = value === 'any' ? '' : dateBefore(Number(value) - 1)
    model.value.published_to = value === 'any' ? '' : dateBefore(0)
  },
})
function toggleType(value: string, selected: boolean) {
  model.value.content_types = selected ? [...new Set([...model.value.content_types, value])] : model.value.content_types.filter(item => item !== value)
}
</script>

<template>
  <div class="swiss-search-filters">
    <div class="quick-filters">
      <slot name="libraries" />
      <details class="content-picker">
        <summary :aria-label="t('knowledgeSearch.contentTypes')">{{ model.content_types.length ? model.content_types.map(value => contentTypeLabel(value, t)).join(' · ') : t('searchPage.allContentTypes') }}</summary>
        <div class="content-options">
          <label v-for="option in contentTypeOptions" :key="option.value"><Checkbox :model-value="model.content_types.includes(option.value)" :disabled="disabled" @update:model-value="toggleType(option.value, $event)" />{{ option.label }}</label>
          <Button variant="ghost" :disabled="disabled" @click="model.content_types = []">{{ t('searchPage.allContentTypes') }}</Button>
        </div>
      </details>
      <Select v-model="timeRange" :disabled="disabled" :aria-label="t('searchPage.timeRange')"><option value="any">{{ t('searchPage.anyTime') }}</option><option value="7">{{ t('searchPage.last7Days') }}</option><option value="30">{{ t('searchPage.last30Days') }}</option><option value="custom">{{ t('searchPage.customTime') }}</option></Select>
      <label class="collapse-check"><Checkbox v-model="model.collapse_similar" :disabled="disabled" />{{ t('searchPage.collapseSimilar') }}</label>
    </div>
    <details class="advanced-filters">
      <summary>{{ t('ikb18.moreFilters') }}</summary>
      <div class="advanced-grid">
        <div v-for="filter in relatedFilters" :key="filter.key" class="related-filter">
          <label v-if="filter.options.length">{{ filter.label }}<select class="rq-control rq-select" :disabled="disabled || loading" :aria-label="filter.label" @change="addRelatedFilter(filter.key, $event)"><option value="">{{ filter.placeholder }}</option><option v-for="option in filter.options" :key="option.value" :value="option.value" :disabled="model[filter.key].includes(option.value)">{{ option.label }}</option></select></label>
          <template v-else><span class="related-filter-label">{{ filter.label }}</span><p class="filter-empty" role="status">{{ loading ? t('common.loading') : filter.empty }}</p></template>
          <div v-if="model[filter.key].length" class="selected-filters"><Button v-for="id in model[filter.key]" :key="id" variant="ghost" :disabled="disabled" :aria-label="t('searchPage.removeFilter', { name: filter.options.find(option => option.value === id)?.label || id })" @click="removeRelatedFilter(filter.key, id)">{{ filter.options.find(option => option.value === id)?.label || id }} <span aria-hidden="true">×</span></Button></div>
        </div>
        <label>{{ t('articleJudgments.starLabel') }}<Select v-model="starFilter" :disabled="disabled" :aria-label="t('articleJudgments.starLabel')"><option v-for="option in starOptions" :key="option.value" :value="option.value">{{ option.label }}</option></Select></label>
        <label>{{ t('knowledgeSearch.limit') }}<input v-model.number="model.limit" type="number" min="1" max="100" step="1" class="rq-control" :disabled="disabled" :aria-label="t('knowledgeSearch.limit')"></label>
      </div>
    </details>
    <div v-if="timeRange === 'custom'" class="date-filters">
      <label>{{ t('knowledgeSearch.publishedFrom') }}<input v-model="model.published_from" type="date" class="rq-control" :disabled="disabled" :aria-label="t('knowledgeSearch.publishedFrom')"></label>
      <label>{{ t('knowledgeSearch.publishedTo') }}<input v-model="model.published_to" type="date" class="rq-control" :disabled="disabled" :aria-label="t('knowledgeSearch.publishedTo')"></label>
    </div>
    <Button v-if="hasActiveFilters" class="reset-filters" variant="ghost" :disabled="disabled" @click="model = defaultSearchFilters(); customTime = false">{{ t('knowledgeSearch.resetFilters') }}</Button>
    <p v-if="model.published_from || model.published_to" class="swiss-filter-hint">{{ t('knowledgeSearch.filterHint') }}</p>
  </div>

</template>

<style scoped lang="less">
.swiss-search-filters { display: grid; gap: var(--space-3); }
.reset-filters { justify-self: start; }
.quick-filters, .date-filters { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); font-size: var(--text-sm); }
.quick-filters :deep(.rq-select) { width: auto; min-height: 32px; padding-block: 0; }
.content-picker { position: relative; max-width: 100%; }
.content-picker summary { border: 1px solid var(--border); padding: var(--space-1) var(--space-2); cursor: pointer; overflow-wrap: anywhere; }
.content-options { position: absolute; top: 100%; left: 0; z-index: 2; display: grid; gap: var(--space-2); padding: var(--space-3); min-width: 132px; max-width: min(400px, 80vw); max-height: 320px; overflow-y: auto; background: var(--surface); border: 1px solid var(--rule); box-shadow: var(--shadow-float); }
.content-options label, .collapse-check { display: flex; align-items: center; gap: var(--space-2); }
.advanced-filters summary { cursor: pointer; font-size: var(--text-sm); }
.advanced-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-3); padding-block: var(--space-3); }
.advanced-grid label, .date-filters label { display: grid; gap: var(--space-1); min-width: 0; font-size: var(--text-xs); }
.related-filter-label, .filter-empty { font-size: var(--text-xs); }
.filter-empty { margin: var(--space-1) 0 0; color: var(--muted); }
.selected-filters { display: flex; flex-wrap: wrap; gap: var(--space-1); margin-top: var(--space-1); }
.selected-filters :deep(.rq-button) { min-height: 28px; padding: var(--space-1) var(--space-2); font-size: var(--text-xs); }
.date-filters { align-items: end; }
.swiss-filter-hint { font-size: var(--text-xs); color: var(--muted); margin: 0; }
@media (max-width: 560px) { .advanced-grid { grid-template-columns: minmax(0, 1fr); } }
</style>
