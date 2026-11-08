<script setup lang="ts">
import { computed, ref, useId, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Combobox, Select, Textarea } from '@/components/ui'
import type { LibraryCatalogItem } from '@/api/library'
import { findShelf, SALE_STATUSES } from '@/api/shelf'

// The same field tree renders saved values and edits business payloads. Envelope
// metadata and concurrency tokens stay outside the editor and are preserved.
const props = withDefaults(defineProps<{ value: unknown; name?: string; context?: string; library: LibraryCatalogItem; readonly?: boolean }>(), { name: '', context: '', readonly: false })
const emit = defineEmits<{ change: [value: unknown] }>()
const { t, te } = useI18n(), id = useId()
const hidden = new Set(['id', 'type', 'revision', 'version', 'space_id', 'library_id', 'entity_type', 'created_at', 'updated_at', 'candidate_entity_id', 'import_location', 'certification_owner', 'issues', 'auto_match', 'merged_into_id', 'source', 'library_updated_at', 'relation_id'])
const entries = computed(() => props.value && typeof props.value === 'object' && !Array.isArray(props.value)
  ? Object.entries(props.value).filter(([key]) => !['', 'entity', 'patch', 'relation', 'library'].includes(props.name) || !hidden.has(key)) : null)
const field = computed(() => props.library.entity_schema?.fields?.find(field => field.key === props.name))
const label = computed(() => (props.name === 'parent_id' ? t('shelfReview.cert.parent') : '') || (props.name === 'region' && props.context === 'certification' ? t('shelfReview.cert.region') : '') || field.value?.name || (te(`shelfReview.fields.${props.name}`) ? t(`shelfReview.fields.${props.name}`) : props.name))
const options = computed(() => {
  if (props.name === 'status') return SALE_STATUSES.map(value => ({ key: value, name: value }))
  if (props.name === 'kind') return props.library.entity_schema?.kinds || []
  if (props.name === 'module_type') return props.library.shelf_config?.module_types || []
  if (props.name === 'region' && props.context === 'sale_statuses') return props.library.shelf_config?.regions || []
  if (props.name === 'standard') return props.library.shelf_config?.standards || []
  if (props.name === 'current_stage') return props.library.shelf_config?.stages || []
  if (props.name === 'stage') return [{ key: 'draft', name: t('shelfModel.draft') }, { key: 'final', name: t('shelfModel.final') }]
  if (field.value?.type === 'select') return field.value.options?.map(value => ({ key: value, name: value })) || []
  return []
})
const reference = computed(() => ['entity_id', 'owner_id', 'subject_id', 'object_id', 'requirement_id', 'cell_part_id', 'parent_id'].includes(props.name))
const candidates = ref<{ value: string; label: string }[]>([]), loading = ref(false), error = ref('')
const choices = computed(() => [...candidates.value, ...(typeof props.value === 'string' && props.value && !candidates.value.some(item => item.value === props.value) ? [{ value: props.value, label: props.value }] : [])])
let generation = 0
async function search(q: string) {
  const current = ++generation; loading.value = true; error.value = ''
  try {
    const response = await findShelf(props.library.id, q)
    if (!response.success) throw new Error(response.message)
    if (current === generation) candidates.value = response.data.items.map(item => ({ value: item.id, label: `${item.canonical_name} · ${item.kind_label || item.kind || ''}` }))
  } catch { if (current === generation) error.value = t('shelfReview.loadFailed') }
  finally { if (current === generation) loading.value = false }
}
function update(key: string, value: unknown) { emit('change', { ...props.value as object, [key]: value }) }
function updateIndex(index: number, value: unknown) { emit('change', (props.value as unknown[]).map((current, i) => i === index ? value : current)) }
function childContext(key: string) { return ['sale_statuses', 'certification', 'document', 'parameter_values'].includes(key) ? key : props.context }
onBeforeUnmount(() => { generation++ })
</script>
<template>
  <div v-if="entries" class="review-values" :class="{ 'review-values-readonly': readonly }">
    <div v-for="[key, current] in entries" :key="key" class="review-field"><ShelfReviewValue :name="key" :value="current" :context="childContext(key)" :library="library" :readonly="readonly" @change="update(key, $event)" /></div>
  </div>
  <div v-else-if="Array.isArray(value)" class="review-array">
    <strong v-if="name">{{ label }}</strong>
    <template v-if="value.some(current => current && typeof current === 'object')">
      <fieldset v-for="(current, index) in value" :key="index"><legend>{{ index + 1 }}</legend><ShelfReviewValue :value="current" :context="context" :library="library" :readonly="readonly" @change="updateIndex(index, $event)" /><Button v-if="!readonly" variant="ghost" @click="emit('change', value.filter((_, i) => i !== index))">{{ t('shelfReview.removeRecord') }}</Button></fieldset>
    </template>
    <span v-else-if="readonly">{{ value.join('、') || t('shelfStatus.noRecord') }}</span>
    <Textarea v-else :id="id" :aria-label="label" :model-value="value.join('\n')" :rows="3" @update:model-value="emit('change', $event === '' ? [] : $event.split('\n'))" />
  </div>
  <div v-else class="review-scalar">
    <label v-if="name" :for="readonly ? undefined : id">{{ label }}</label>
    <span v-if="readonly">{{ typeof value === 'boolean' ? t(value ? 'managed.yes' : 'managed.no') : options.find(option => option.key === value)?.name ?? value ?? t('shelfStatus.noRecord') }}</span>
    <Combobox v-else-if="reference" :id="id" :aria-label="label" :model-value="String(value || '')" :options="choices" :aria-busy="loading" remote @search="search" @update:model-value="emit('change', $event)" />
    <Select v-else-if="options.length" :id="id" :aria-label="label" :model-value="String(value ?? '')" @update:model-value="emit('change', $event)"><option value="">{{ t('shelfStatus.noRecord') }}</option><option v-if="value && !options.some(option => option.key === value)" :value="String(value)">{{ value }}</option><option v-for="option in options" :key="option.key" :value="option.key">{{ option.name }}</option></Select>
    <input v-else-if="typeof value === 'boolean'" :id="id" type="checkbox" :aria-label="label" :checked="value" @change="emit('change', ($event.target as HTMLInputElement).checked)">
    <input v-else :id="id" class="rq-control" :aria-label="label" :type="typeof value === 'number' ? 'number' : 'text'" step="any" :value="value ?? ''" @input="emit('change', typeof value === 'number' ? Number(($event.target as HTMLInputElement).value) : ($event.target as HTMLInputElement).value)">
    <p v-if="error" role="alert">{{ error }}</p>
  </div>
</template>
<style scoped>
.review-values, .review-field, .review-array, .review-scalar { display:grid; gap:var(--space-2); min-width:0; }
.review-values { grid-template-columns:repeat(auto-fit,minmax(180px,1fr)); gap:var(--space-3); }
.review-values-readonly { grid-template-columns:1fr; gap:var(--space-1); }
.review-values-readonly .review-scalar { display:flex; flex-wrap:wrap; gap:var(--space-2); }
.review-scalar label { font-size:var(--text-xs); font-weight:700; }
.review-values-readonly span { overflow-wrap:anywhere; }
fieldset { min-width:0; margin:0; border:1px solid var(--border); padding:var(--space-2); }
legend { font-family:var(--mono); }
.review-array > strong { font-size:var(--text-xs); }
</style>
