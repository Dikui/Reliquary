<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui'
import type { EntityField } from '@/api/library'
defineProps<{ relationship?: boolean }>()
const model = defineModel<EntityField[]>({ required: true })
const { t } = useI18n()
const types: EntityField['type'][] = ['text', 'number', 'boolean', 'date', 'select', 'multiselect', 'list', 'entity_ref', 'url', 'attachment']
function values(event: Event) { return (event.target as HTMLInputElement).value.split(',').map(s => s.trim()).filter(Boolean) }
</script>
<template>
  <div>
    <fieldset v-for="(field, index) in model" :key="index">
      <label>{{ t('managed.key') }}<input v-model="field.key"></label><label>{{ t('ikb18.name') }}<input v-model="field.name"></label>
      <label>{{ t('managed.fieldType') }}<select v-model="field.type"><option v-for="type in types" :key="type" :value="type">{{ t(`managed.types.${type}`) }}</option></select></label>
      <label>{{ t('managed.unit') }}<input v-model="field.unit"></label><label v-if="!relationship">{{ t('managed.appliesTo') }}<input :value="field.kinds?.join(', ')" @input="field.kinds = values($event)"></label>
      <label v-if="field.type === 'select' || field.type === 'multiselect'">{{ t('managed.options') }}<input :value="field.options?.join(', ')" @input="field.options = values($event)"></label>
      <label><input v-model="field.archived" type="checkbox">{{ t('managed.archived') }}</label><Button @click="model = model.filter((_, i) => i !== index)">{{ t('ikb18.remove') }}</Button>
    </fieldset>
    <Button @click="model = [...model, { key: '', name: '', type: 'text', kinds: [] }]">{{ t('managed.addField') }}</Button>
  </div>
</template>
<style scoped>
fieldset { display:flex; flex-wrap:wrap; gap:var(--space-3); border:1px solid var(--border); padding:var(--space-3); margin-block:var(--space-3); } label { display:grid; gap:var(--space-2); flex:1 1 160px; } input,select { min-height:var(--control); min-width:0; border:1px solid var(--border); padding:var(--space-2); background:var(--bg); color:var(--text); font:inherit; } input[type=checkbox] { min-height:auto; }
</style>
