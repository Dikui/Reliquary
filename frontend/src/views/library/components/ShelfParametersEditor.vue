<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button, Field, Input } from '@/components/ui'
import type { EntityField, LibraryCatalogItem } from '@/api/library'
import type { ShelfCondition, ShelfNode } from '@/api/shelf'
import ManagedAttributes from './ManagedAttributes.vue'
import ShelfConditionsEditor from './ShelfConditionsEditor.vue'
const props = defineProps<{ fields: EntityField[]; library: LibraryCatalogItem; composition: NonNullable<ShelfNode['shelf']>['composition'] }>()
const attributes = defineModel<Record<string, unknown>>({ required: true }), { t } = useI18n()
type Value = { value: unknown; conditions: ShelfCondition[] }
const rows = (key: string) => (attributes.value[key] || []) as Value[]
function update(key: string, index: number, patch: Partial<Value>) {
  attributes.value = { ...attributes.value, [key]: rows(key).map((row, i) => i === index ? { ...row, ...patch } : row) }
}
function add(field: EntityField) {
  const value = field.type === 'number' ? 0 : field.type === 'boolean' ? false : ['list', 'multiselect'].includes(field.type) ? [] : ''
  attributes.value = { ...attributes.value, [field.key]: [...rows(field.key), { value, conditions: [] }] }
}
function remove(key: string, index: number) {
  const next = { ...attributes.value }, values = rows(key).filter((_, i) => i !== index)
  if (values.length) next[key] = values; else delete next[key]
  attributes.value = next
}
const range = (value: unknown): value is { min: number; max: number } => !!value && typeof value === 'object' && 'min' in value && 'max' in value
</script>
<template>
  <section v-for="field in props.fields" :key="field.key" class="rq-model-parameter">
    <h3>{{ field.name }} <small v-if="field.unit">({{ field.unit }})</small></h3>
    <fieldset v-for="(row, index) in rows(field.key)" :key="index">
      <legend>{{ field.name }} {{ index + 1 }}</legend>
      <template v-if="field.type === 'number' && range(row.value)">
        <Field :id="`parameter-${field.key}-${index}-min`" :label="t('shelfStatus.min')"><Input :id="`parameter-${field.key}-${index}-min`" type="number" step="any" :model-value="row.value.min" @update:model-value="update(field.key, index, { value: { ...(row.value as object), min: $event === '' ? null : Number($event) } })" /></Field>
        <Field :id="`parameter-${field.key}-${index}-max`" :label="t('shelfStatus.max')"><Input :id="`parameter-${field.key}-${index}-max`" type="number" step="any" :model-value="row.value.max" @update:model-value="update(field.key, index, { value: { ...(row.value as object), max: $event === '' ? null : Number($event) } })" /></Field>
      </template>
      <ManagedAttributes v-else :model-value="{ value: row.value }" :fields="[{ ...field, key: 'value', product_line_ids: undefined }]" :library-id="library.id" kind="model" @update:model-value="update(field.key, index, { value: $event.value })" />
      <Button v-if="field.type === 'number'" @click="update(field.key, index, { value: range(row.value) ? row.value.min : { min: row.value, max: row.value } })">{{ t(range(row.value) ? 'shelfModel.singleValue' : 'shelfModel.rangeValue') }}</Button>
      <ShelfConditionsEditor :model-value="row.conditions || []" :id-prefix="`parameter-${field.key}-${index}`" :library="library" :composition="composition" @update:model-value="update(field.key, index, { conditions: $event })" />
      <Button @click="remove(field.key, index)">{{ t('shelfReview.removeRecord') }}</Button>
    </fieldset>
    <Button @click="add(field)">{{ t('shelfModel.addValue') }}</Button>
  </section>
</template>
<style scoped>
.rq-model-parameter, .rq-model-parameter > fieldset { display:grid; gap:var(--space-3); min-width:0; }
.rq-model-parameter > fieldset { margin:0; padding:var(--space-3); border:1px solid var(--border); }
h3 { font-size:var(--text-sm); margin:0; } small { color:var(--muted); }
</style>
