<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui'
import { listLibraries, getEntityConversion, type LibraryCatalogItem } from '@/api/library'
import { SHELF_KINDS } from '@/utils/productShelf'
import { moveType } from '@/utils/libraryCatalogForm'
import ManagedFieldSchema from './ManagedFieldSchema.vue'
const library = defineModel<LibraryCatalogItem>({ required: true })
const { t } = useI18n()
const schema = computed(() => {
  const value = library.value.entity_schema ||= { kinds: [], fields: [] }
  value.kinds ||= []; value.fields ||= []
  value.kinds.forEach(kind => { kind.parent_kinds ||= [] }); value.fields.forEach(field => { field.kinds ||= [] })
  return value
})
const needsConversion = ref(false)
const shelfLists = [{ key: 'module_types', label: 'productShelf.moduleTypes' }, { key: 'regions', label: 'productShelf.regions' }, { key: 'stages', label: 'productShelf.stages' }] as const
const newValues = ref({ module_types: '', regions: '', stages: '' })
const listErrors = ref({ module_types: '', regions: '', stages: '' })
const shelfConfig = computed(() => library.value.shelf_config ||= {})
function appendValue(key: typeof shelfLists[number]['key']) {
  const name = newValues.value[key]
  const rows = shelfConfig.value[key] ||= []
  if (!name.trim() || name.length > 100 || rows.some(row => row.name.trim().toLowerCase() === name.trim().toLowerCase()) || rows.length >= 100) { listErrors.value[key] = t('productShelf.invalidValue'); return }
  rows.push({ key: `value_${crypto.randomUUID().replaceAll('-', '_')}`, name })
  newValues.value[key] = ''; listErrors.value[key] = ''
}
const libraries = ref<LibraryCatalogItem[]>([]), conversion = ref<Record<string, number>>({}), conversionError = ref('')
let sequence = 0
watch(() => library.value.id, async () => {
  const current = ++sequence
  newValues.value = { module_types: '', regions: '', stages: '' }; listErrors.value = { module_types: '', regions: '', stages: '' }
  conversion.value = {}; conversionError.value = ''; needsConversion.value = library.value.entity_mode !== 'managed'
  try {
    const [catalog, counts] = await Promise.all([listLibraries(), getEntityConversion(library.value.id)])
    if (current !== sequence) return
    if (!catalog.success || !counts.success) throw new Error(t('common.operationFailed'))
    libraries.value = catalog.data.filter(l => l.entity_mode === 'managed' && !l.shelf_enabled && l.id !== library.value.id)
    conversion.value = counts.data.entity_counts || {}
  } catch (cause: any) { if (current === sequence) conversionError.value = cause.message || t('common.operationFailed') }
}, { immediate: true })
onBeforeUnmount(() => { sequence++ })
function values(event: Event) { return (event.target as HTMLInputElement).value.split(',').map(s => s.trim()).filter(Boolean) }
function addShelf() {
  library.value.shelf_enabled = true
  library.value.product_library_ids = []
}
</script>
<template>
  <section class="managed-settings">
    <h2>{{ t('managed.title') }}</h2>
    <label>{{ t('managed.mode') }}<select v-model="library.entity_mode" :disabled="library.shelf_enabled"><option value="extracted">{{ t('managed.extracted') }}</option><option value="managed">{{ t('managed.managed') }}</option></select></label>
    <p>{{ t('managed.conversionHint') }}</p>
    <p v-if="conversionError" role="alert">{{ conversionError }}</p>
    <p v-if="library.shelf_enabled">{{ t('productShelf.explicitLinks') }}</p>
    <template v-else>
    <h3>{{ t('managed.autoProductLibraries') }}</h3>
    <p v-if="library.entity_mode === 'managed'">{{ t('managed.ownProductsAlwaysMatch') }}</p>
    <p v-if="!libraries.length">{{ t('managed.noOtherProductLibraries') }}</p>
    <label v-for="target in libraries" :key="target.id"><input type="checkbox" :checked="library.product_library_ids?.includes(target.id)" @change="library.product_library_ids = ($event.target as HTMLInputElement).checked ? [...(library.product_library_ids || []), target.id] : (library.product_library_ids || []).filter(id => id !== target.id)">{{ target.name }}</label>
    </template>
    <template v-if="library.entity_mode === 'managed'">
      <Button v-if="!library.shelf_enabled" @click="addShelf">{{ t('productShelf.addConfig') }}</Button>
      <p v-if="!library.shelf_enabled">{{ t('productShelf.configHint') }}</p>
      <h3 v-if="needsConversion">{{ t('managed.conversionMapping') }}</h3><label v-for="(count, type) in needsConversion ? conversion : {}" :key="type">{{ type }} ({{ count }})<select :value="library.entity_kind_mapping?.[type] || ''" @change="library.entity_kind_mapping = { ...library.entity_kind_mapping, [type]: ($event.target as HTMLSelectElement).value }"><option value="">{{ t('managed.chooseKind') }}</option><option v-for="kind in schema.kinds" :key="kind.key" :value="kind.key">{{ kind.name || kind.key }}</option></select></label>
      <label>{{ t('managed.documentRelationTypes') }}<input :value="(library.document_relation_types || ['references', 'specification', 'test_report', 'comparison']).join(', ')" @input="library.document_relation_types = values($event)"></label>
      <template v-if="library.shelf_enabled">
        <h3>{{ t('productShelf.title') }}</h3>
        <p>{{ SHELF_KINDS.map(key => t(`productShelf.kinds.${key}`)).join(' · ') }}</p>
        <fieldset v-for="list in shelfLists" :key="list.key" class="shelf-list" :data-shelf-list="list.key">
          <legend>{{ t(list.label) }}</legend>
          <p v-if="list.key === 'stages'">{{ t('productShelf.stageOrder') }}</p>
          <div v-for="(item, index) in shelfConfig[list.key] || []" :key="item.key" class="shelf-value">
            <label>{{ t('ikb18.name') }} {{ index + 1 }}<input v-model="item.name" :aria-invalid="!item.name.trim() || item.name.length > 100" maxlength="100"></label>
            <Button :disabled="index === 0" @click="moveType(shelfConfig[list.key]!, index, -1)">{{ t('ikb18.up') }}</Button>
            <Button :disabled="index === (shelfConfig[list.key]?.length || 0) - 1" @click="moveType(shelfConfig[list.key]!, index, 1)">{{ t('ikb18.down') }}</Button>
          </div>
          <div class="shelf-value">
            <label>{{ t('productShelf.newValue') }}<input v-model="newValues[list.key]" maxlength="100" :aria-invalid="!!listErrors[list.key]" :aria-describedby="listErrors[list.key] ? `shelf-error-${list.key}` : undefined" @keydown.enter.prevent="!$event.isComposing && appendValue(list.key)"></label>
            <Button @click="appendValue(list.key)">{{ t('common.add') }}</Button>
          </div>
          <p v-if="listErrors[list.key]" :id="`shelf-error-${list.key}`" role="alert">{{ listErrors[list.key] }}</p>
        </fieldset>
      </template>
      <h3 v-if="!library.shelf_enabled">{{ t('managed.kinds') }}</h3><p v-if="!library.shelf_enabled">{{ t('managed.keysHint') }}</p>
      <fieldset v-for="(kind, index) in library.shelf_enabled ? [] : schema.kinds" :key="index">
        <label>{{ t('managed.key') }}<input v-model="kind.key"></label><label>{{ t('ikb18.name') }}<input v-model="kind.name"></label>
        <fieldset class="kind-choices"><legend>{{ t('managed.parents') }}</legend><label v-for="option in schema.kinds.filter(k => k.key)" :key="option.key"><input v-model="kind.parent_kinds" type="checkbox" :value="option.key">{{ option.name || option.key }}</label></fieldset>
      <Button @click="schema.kinds.splice(index, 1)">{{ t('ikb18.remove') }}</Button></fieldset>
      <Button v-if="!library.shelf_enabled" @click="schema.kinds.push({ key: '', name: '', parent_kinds: [] })">{{ t('managed.addKind') }}</Button>
      <h3>{{ t('managed.fields') }}</h3>
      <ManagedFieldSchema v-model="schema.fields" />
      <template v-if="!library.shelf_enabled">
      <h3>{{ t('ikb18.relations') }}</h3><p>{{ t('managed.hierarchyHint') }}</p>
      <fieldset v-for="(relation, index) in library.relation_types" :disabled="library.shelf_enabled" :key="index">
        <label>{{ t('ikb18.name') }}<input v-model="relation.name"></label>
        <label><input v-model="relation.enabled" type="checkbox">{{ t('ikb18.enabled') }}</label><label><input v-model="relation.hierarchical" type="checkbox">{{ t('managed.hierarchical') }}</label>
        <fieldset class="kind-choices"><legend>{{ t('managed.subjectKinds') }}</legend><label v-for="option in schema.kinds.filter(k => k.key)" :key="option.key"><input type="checkbox" :checked="relation.subject_kinds?.includes(option.key)" @change="relation.subject_kinds = ($event.target as HTMLInputElement).checked ? [...(relation.subject_kinds || []), option.key] : (relation.subject_kinds || []).filter(key => key !== option.key)">{{ option.name || option.key }}</label></fieldset>
        <fieldset class="kind-choices"><legend>{{ t('managed.objectKinds') }}</legend><label v-for="option in schema.kinds.filter(k => k.key)" :key="option.key"><input type="checkbox" :checked="relation.object_kinds?.includes(option.key)" @change="relation.object_kinds = ($event.target as HTMLInputElement).checked ? [...(relation.object_kinds || []), option.key] : (relation.object_kinds || []).filter(key => key !== option.key)">{{ option.name || option.key }}</label></fieldset>
      <ManagedFieldSchema relationship :model-value="relation.fields || []" @update:model-value="relation.fields = $event" /><Button @click="library.relation_types.splice(index, 1)">{{ t('ikb18.remove') }}</Button></fieldset>
      <Button v-if="!library.shelf_enabled" @click="library.relation_types.push({ name: '', enabled: true, position: library.relation_types.length, hierarchical: false, subject_kinds: [], object_kinds: [] })">{{ t('managed.addRelation') }}</Button>
      </template>
    </template>
  </section>
</template>
<style scoped>
.managed-settings { border-top: 2px solid var(--rule); padding-block: var(--space-4); }
fieldset { display: flex; flex-wrap: wrap; gap: var(--space-3); border: 1px solid var(--border); padding: var(--space-3); margin-block: var(--space-3); }
label { display: grid; gap: var(--space-2); flex: 1 1 160px; } input, select { min-height: var(--control); min-width: 0; border: 1px solid var(--border); padding: var(--space-2); background: var(--bg); color: var(--text); font: inherit; } input[type='checkbox'] { min-height: auto; } p { color: var(--muted); }
.kind-choices { flex: 1 1 200px; margin: 0; } .kind-choices label { display: flex; align-items: center; flex: 0 1 auto; }
.shelf-list { display: grid; } .shelf-value { display: flex; flex-wrap: wrap; align-items: end; gap: var(--space-2); min-width: 0; } .shelf-value label { min-width: 0; }
</style>
