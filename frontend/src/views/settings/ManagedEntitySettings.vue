<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui'
import type { LibraryCatalogItem } from '@/api/library'
const library = defineModel<LibraryCatalogItem>({ required: true })
const { t } = useI18n()
const schema = computed(() => {
  const value = library.value.entity_schema ||= { kinds: [], fields: [] }
  value.kinds ||= []; value.fields ||= []
  value.kinds.forEach(kind => { kind.parent_kinds ||= [] }); value.fields.forEach(field => { field.kinds ||= [] })
  return value
})
function values(event: Event) { return (event.target as HTMLInputElement).value.split(',').map(s => s.trim()).filter(Boolean) }
</script>
<template>
  <section class="managed-settings">
    <h2>{{ t('managed.title') }}</h2>
    <label>{{ t('managed.mode') }}<select v-model="library.entity_mode"><option value="extracted">{{ t('managed.extracted') }}</option><option value="managed">{{ t('managed.managed') }}</option></select></label>
    <p>{{ t('managed.modeHint') }}</p>
    <template v-if="library.entity_mode === 'managed'">
      <h3>{{ t('managed.kinds') }}</h3><p>{{ t('managed.keysHint') }}</p>
      <fieldset v-for="(kind, index) in schema.kinds" :key="index">
        <label>{{ t('managed.key') }}<input v-model="kind.key"></label><label>{{ t('ikb18.name') }}<input v-model="kind.name"></label>
        <label>{{ t('managed.parents') }}<input :value="kind.parent_kinds.join(', ')" @input="kind.parent_kinds = values($event)"></label>
      <Button @click="schema.kinds.splice(index, 1)">{{ t('ikb18.remove') }}</Button></fieldset>
      <Button @click="schema.kinds.push({ key: '', name: '', parent_kinds: [] })">{{ t('managed.addKind') }}</Button>
      <h3>{{ t('managed.fields') }}</h3>
      <fieldset v-for="(field, index) in schema.fields" :key="index">
        <label>{{ t('managed.key') }}<input v-model="field.key"></label><label>{{ t('ikb18.name') }}<input v-model="field.name"></label>
        <label>{{ t('managed.fieldType') }}<select v-model="field.type"><option v-for="type in ['text', 'number', 'boolean', 'date', 'select']" :key="type" :value="type">{{ t(type === 'number' ? 'managed.types.number' : type === 'boolean' ? 'managed.types.boolean' : type === 'date' ? 'managed.types.date' : type === 'select' ? 'managed.types.select' : 'managed.types.text') }}</option></select></label>
        <label>{{ t('managed.unit') }}<input v-model="field.unit"></label>
        <label>{{ t('managed.appliesTo') }}<input :value="field.kinds.join(', ')" @input="field.kinds = values($event)"></label>
        <label v-if="field.type === 'select'">{{ t('managed.options') }}<input :value="field.options?.join(', ')" @input="field.options = values($event)"></label>
        <label><input v-model="field.archived" type="checkbox">{{ t('managed.archived') }}</label>
      <Button @click="schema.fields.splice(index, 1)">{{ t('ikb18.remove') }}</Button></fieldset>
      <Button @click="schema.fields.push({ key: '', name: '', type: 'text', kinds: [] })">{{ t('managed.addField') }}</Button>
      <h3>{{ t('ikb18.relations') }}</h3><p>{{ t('managed.hierarchyHint') }}</p>
      <fieldset v-for="(relation, index) in library.relation_types" :key="index">
        <label>{{ t('ikb18.name') }}<input v-model="relation.name"></label>
        <label><input v-model="relation.enabled" type="checkbox">{{ t('ikb18.enabled') }}</label><label><input v-model="relation.hierarchical" type="checkbox">{{ t('managed.hierarchical') }}</label>
        <label>{{ t('managed.subjectKinds') }}<input :value="relation.subject_kinds?.join(', ')" @input="relation.subject_kinds = values($event)"></label>
        <label>{{ t('managed.objectKinds') }}<input :value="relation.object_kinds?.join(', ')" @input="relation.object_kinds = values($event)"></label>
      <Button @click="library.relation_types.splice(index, 1)">{{ t('ikb18.remove') }}</Button></fieldset>
      <Button @click="library.relation_types.push({ name: '', enabled: true, position: library.relation_types.length, hierarchical: false, subject_kinds: [], object_kinds: [] })">{{ t('managed.addRelation') }}</Button>
    </template>
  </section>
</template>
<style scoped>
.managed-settings { border-top: 2px solid var(--rule); padding-block: var(--space-4); }
fieldset { display: flex; flex-wrap: wrap; gap: var(--space-3); border: 1px solid var(--border); padding: var(--space-3); margin-block: var(--space-3); }
label { display: grid; gap: var(--space-2); flex: 1 1 160px; } input, select { min-height: var(--control); min-width: 0; border: 1px solid var(--border); padding: var(--space-2); background: var(--bg); color: var(--text); font: inherit; } input[type='checkbox'] { min-height: auto; } p { color: var(--muted); }
</style>
