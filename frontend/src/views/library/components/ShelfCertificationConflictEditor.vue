<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Checkbox, Combobox, Field, Input, Select } from '@/components/ui'
import { findShelf } from '@/api/shelf'
import { getEntity } from '@/api/entities'
import type { LibraryCatalogItem } from '@/api/library'
import type { ShelfImportCertification, ShelfReviewItem, ShelfReviewOperation } from '@/api/shelf-review'
import ShelfReviewValue from './ShelfReviewValue.vue'

const props = defineProps<{ operation: ShelfReviewOperation; item: ShelfReviewItem; library: LibraryCatalogItem; disabled?: boolean }>()
const emit = defineEmits<{ resolve: [operations: ShelfReviewOperation[]]; change: [operation: ShelfReviewOperation]; cancel: [] }>()
const { t } = useI18n(), id = useId()
const record = ref<ShelfImportCertification>(JSON.parse(JSON.stringify(props.operation.certification)))
const owner = ref({ ...props.operation.certification_owner! })
const mode = ref(record.value.owner_id ? 'existing' : 'rewrite'), saveAlias = ref(false), parentID = ref('')
const recordType = ref(props.operation.type), error = ref(''), busy = ref(false), searching = ref(false), submitted = ref(false), form = ref<HTMLFormElement>()
const editableRecord = computed(() => Object.fromEntries(Object.entries(record.value).filter(([key]) => !['owner_id', 'name'].includes(key))))
const ownerID = ref(record.value.owner_id)
const options = ref<{ value: string; label: string }[]>(ownerID.value ? [{ value: ownerID.value, label: owner.value.name }] : [])
const parents = ref<{ value: string; label: string }[]>([])
const blocked = computed(() => props.disabled || busy.value)
const ownerChoices = computed(() => [{ key: 'existing', label: t('shelfReview.cert.existing') }, { key: 'rewrite', label: t('shelfReview.cert.rewrite') }, { key: 'new', label: t('shelfReview.cert.newOwner') }])
const ownerKinds = computed(() => (props.library.entity_schema?.kinds || []).filter(kind => ['model', 'part', 'product', 'platform'].includes(kind.key)))
const parentKind = computed(() => ({ model: 'product', product: 'platform', platform: 'product_line' })[owner.value.kind as 'model' | 'product' | 'platform'])
const suggestions = computed(() => props.item.conflicts.flatMap(conflict => {
  const values = (conflict.details as { split_suggestions?: unknown } | undefined)?.split_suggestions
  return Array.isArray(values) && values.every(value => value && typeof value.standard === 'string') ? values as ShelfImportCertification[] : []
}))
let generation = 0
async function search(q: string, parent = false) {
  const current = ++generation; searching.value = true; error.value = ''
  try {
    const response = await findShelf(props.library.id, q)
    if (!response.success) throw new Error(response.message)
    if (current !== generation) return
    const values = response.data.items.filter(item => item.kind === (parent ? parentKind.value : owner.value.kind)).map(item => ({ value: item.id, label: `${item.canonical_name} · ${item.kind_label || item.kind}` }))
    if (parent) parents.value = values; else options.value = values
  } catch (cause) { if (current === generation) error.value = cause instanceof Error ? cause.message : t('shelfReview.cert.loadFailed') }
  finally { if (current === generation) searching.value = false }
}
function changeRecord(value: unknown) { record.value = { ...record.value, ...value as object }; emit('change', { ...props.operation, certification: record.value }) }
function split() {
  if (blocked.value || suggestions.value.length < 2) return
  emit('resolve', suggestions.value.map((suggestion, index) => ({ ...props.operation, id: index === 0 ? props.item.id : crypto.randomUUID(), certification: { ...record.value, ...(suggestion.name !== undefined ? { name: suggestion.name } : {}), standard: suggestion.standard, year: suggestion.year || 0 } })))
}
async function resolve() {
  if (blocked.value) return
  error.value = ''; submitted.value = true
  if ((mode.value === 'existing' && !ownerID.value) || (mode.value !== 'existing' && !owner.value.name.trim()) || (mode.value === 'new' && parentKind.value && !parentID.value)) {
    error.value = mode.value === 'new' && parentKind.value && !parentID.value ? t('shelfReview.cert.parentRequired') : t('shelfReview.cert.required'); await nextTick(); form.value?.querySelector<HTMLElement>('[aria-invalid=true]')?.focus(); return
  }
  busy.value = true
  const operations: ShelfReviewOperation[] = []
  let target = mode.value === 'existing' ? ownerID.value : ''
  try {
    if (mode.value === 'existing' && saveAlias.value) {
      const response = await getEntity(target)
      if (!response.success || !response.data.entity.revision) throw new Error(t('shelfReview.cert.loadFailed'))
      operations.push({ id: props.item.id, type: 'add_alias', entity_id: target, revision: response.data.entity.revision, alias: props.operation.certification_owner!.name })
    }
    if (mode.value === 'new') {
      operations.push({ id: props.item.id, type: 'create_entity', entity: { name: owner.value.name, kind: owner.value.kind }, ...(parentKind.value ? { parent_id: parentID.value } : {}) })
      target = `$${props.item.id}`
    }
    operations.push({ ...props.operation, id: operations.length ? crypto.randomUUID() : props.item.id, type: recordType.value, certification_owner: { ...owner.value }, certification: { ...record.value, owner_id: target } })
    emit('resolve', operations)
  } catch (cause) { error.value = cause instanceof Error ? cause.message : t('shelfReview.cert.loadFailed') }
  finally { busy.value = false }
}
onBeforeUnmount(() => { generation++ })
</script>
<template>
  <form ref="form" class="certification-review" novalidate @submit.prevent="resolve">
    <fieldset :disabled="blocked" :aria-describedby="error ? `${id}-error` : undefined">
      <legend>{{ t('shelfReview.cert.handle') }}</legend>
      <div v-if="suggestions.length > 1" class="certification-split">
        <p>{{ t('shelfReview.cert.splitHint') }}</p>
        <ol><li v-for="(suggestion, index) in suggestions" :key="index">{{ library.shelf_config?.standards?.find(standard => standard.key === suggestion.standard)?.name || suggestion.standard }}<template v-if="suggestion.year"> · {{ suggestion.year }}</template></li></ol>
        <Button :disabled="blocked" @click="split">{{ t('shelfReview.cert.split', { count: suggestions.length }) }}</Button>
      </div>
      <div class="certification-choices" role="group" :aria-label="t('shelfReview.cert.owner')">
        <Button v-for="choice in ownerChoices" :key="choice.key" :disabled="blocked" :aria-pressed="mode === choice.key" @click="mode = choice.key">{{ choice.label }}</Button>
      </div>
      <div class="certification-fields">
        <Field :label="t('shelfReview.cert.ownerKind')"><Select :model-value="owner.kind" @update:model-value="owner.kind = $event; ownerID = ''; options = []; parentID = ''; parents = []; generation++"><option v-for="kind in ownerKinds" :key="kind.key" :value="kind.key">{{ kind.name }}</option></Select></Field>
        <template v-if="mode === 'existing'">
          <Field :label="t('shelfReview.cert.owner')" :error="submitted && !ownerID ? t('shelfReview.cert.required') : ''"><Combobox v-model="ownerID" :options="options" :aria-busy="searching" remote @search="search($event)" /></Field>
          <label class="certification-alias"><Checkbox v-model="saveAlias" />{{ t('shelfReview.cert.saveAlias') }}</label>
          <p v-if="saveAlias">{{ t('shelfReview.cert.aliasHint') }} · {{ operation.certification_owner?.name }}</p>
        </template>
        <Field v-else :label="t('shelfReview.cert.ownerName')" :error="submitted && !owner.name.trim() ? t('shelfReview.cert.required') : ''"><Input v-model="owner.name" /></Field>
        <Field v-if="mode === 'new' && parentKind" :label="t('shelfReview.cert.parent')" :error="submitted && !parentID ? t('shelfReview.cert.parentRequired') : ''"><Combobox v-model="parentID" :options="parents" :aria-busy="searching" remote @search="search($event, true)" /></Field>
        <Field :label="t('shelfReview.cert.recordMode')"><Select v-model="recordType"><option value="update_certification">{{ t('shelfReview.cert.update') }}</option><option value="create_certification">{{ t('shelfReview.cert.create') }}</option></Select></Field>
      </div>
      <Field :label="t('shelfReview.cert.optionalName')"><Input :model-value="record.name || ''" @update:model-value="changeRecord({ name: $event })" /></Field>
      <ShelfReviewValue :value="editableRecord" context="certification" :library="library" @change="changeRecord" />
      <p v-if="error" :id="`${id}-error`" role="alert">{{ error }}</p>
      <div class="certification-actions"><Button type="submit" :disabled="blocked" :loading="busy">{{ t('shelfReview.cert.revalidate') }}</Button><Button :disabled="blocked" @click="emit('cancel')">{{ t('common.cancel') }}</Button></div>
    </fieldset>
  </form>
</template>
<style scoped>
.certification-review { grid-column:1 / -1; }
fieldset { margin:0; border:1px solid var(--border); padding:var(--space-3); min-width:0; display:grid; gap:var(--space-3); }
.certification-fields { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:var(--space-3); }
.certification-choices,.certification-actions { display:flex; flex-wrap:wrap; gap:var(--space-2); }
.certification-choices :deep(button[aria-pressed=true]) { background:var(--text); color:var(--bg); }
.certification-alias { display:flex; align-items:center; gap:var(--space-2); }
p { margin:0; color:var(--muted); font-size:var(--text-xs); }
[role=alert] { color:var(--danger); }
.certification-split { padding:var(--space-3); background:var(--surface-soft); }
.certification-split ol { margin:var(--space-2) 0; }
</style>
