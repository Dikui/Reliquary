<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button, Checkbox, Field, Select } from '@/components/ui'
import type { LibraryCatalogItem } from '@/api/library'
import type { ShelfCondition, ShelfNode } from '@/api/shelf'
withDefaults(defineProps<{ library: LibraryCatalogItem; composition: NonNullable<ShelfNode['shelf']>['composition']; idPrefix?: string }>(), { idPrefix: 'condition' })
const conditions = defineModel<ShelfCondition[]>({ required: true }), { t } = useI18n()
function toggle(index: number, id: string, checked: boolean) {
  conditions.value = conditions.value.map((condition, i) => i === index ? { ...condition, part_ids: checked ? [...condition.part_ids, id] : condition.part_ids.filter(part => part !== id) } : condition)
}
</script>
<template>
  <fieldset class="rq-model-conditions">
    <legend>{{ t('shelfFiles.conditions') }}</legend>
    <p v-if="!conditions.length">{{ t('shelfFiles.allOptions') }}</p>
    <div v-for="(condition, index) in conditions" :key="index" class="rq-model-condition">
      <Field :id="`${idPrefix}-module-${index}`" :label="t('shelfModel.module')"><Select :id="`${idPrefix}-module-${index}`" v-model="condition.module_type" @update:model-value="condition.part_ids = []"><option value="" disabled>{{ t('shelfModel.module') }}</option><option v-for="group in composition.filter(group => !conditions.some((other, i) => i !== index && other.module_type === group.module_type))" :key="group.module_type" :value="group.module_type">{{ library.shelf_config?.module_types?.find(module => module.key === group.module_type)?.name || group.module_type }}</option></Select></Field>
      <label v-for="part in composition.find(group => group.module_type === condition.module_type)?.parts" :key="part.id"><Checkbox :model-value="condition.part_ids.includes(part.id)" :aria-label="part.canonical_name" @update:model-value="toggle(index, part.id, $event)" />{{ part.canonical_name }}</label>
      <Button @click="conditions = conditions.filter((_, i) => i !== index)">{{ t('shelfFiles.remove') }}</Button>
    </div>
    <Button :disabled="conditions.length >= composition.length" @click="conditions = [...conditions, { module_type: '', part_ids: [] }]">{{ t('shelfFiles.addCondition') }}</Button>
  </fieldset>
</template>
<style scoped>
.rq-model-conditions, .rq-model-condition { display:grid; gap:var(--space-2); min-width:0; }
.rq-model-conditions { margin:0; padding:var(--space-3); border:1px solid var(--border); }
.rq-model-condition label { display:flex; gap:var(--space-2); align-items:center; }
.rq-model-condition { border-bottom:1px solid var(--border); padding-block:var(--space-2); }
p { margin:0; color:var(--muted); }
</style>
