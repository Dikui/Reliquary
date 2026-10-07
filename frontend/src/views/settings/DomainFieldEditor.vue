<template>
  <div class="domain-field-grid">
    <label><span>{{ t('knowledgeDomains.key') }}</span><t-input v-model="field.key" :disabled="disabled" /></label>
    <label><span>{{ t('knowledgeDomains.label') }}</span><t-input v-model="field.label" :disabled="disabled" /></label>
    <label class="full-width"><span>{{ t('knowledgeDomains.aliases') }}</span><t-tag-input v-model="field.aliases" :disabled="disabled" :placeholder="t('knowledgeDomains.tagHint')" /></label>
    <label><span>{{ t('knowledgeDomains.valueType') }}</span><t-select v-model="field.value_type" :options="valueTypes" :disabled="disabled" :popup-props="{ zIndex: 6200 }" /></label>
    <label><span>{{ t('knowledgeDomains.unitFamily') }}</span><t-select v-model="field.unit_family" clearable :options="unitOptions" :disabled="disabled || field.value_type !== 'number'" :popup-props="{ zIndex: 6200 }" /></label>
    <label class="full-width"><span>{{ t('knowledgeDomains.appliesTo') }}</span><t-select v-model="field.entity_types" multiple clearable :options="typeOptions" :disabled="disabled" :placeholder="t('knowledgeDomains.allTypes')" :popup-props="{ zIndex: 6200 }" /></label>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DomainField, DomainUnitFamily } from '@/api/knowledge-domains'
const field = defineModel<DomainField>({ required: true })
const props = defineProps<{ entityTypes: string[]; unitFamilies: DomainUnitFamily[]; disabled?: boolean }>()
const { t } = useI18n()
const valueTypes = computed(() => ['number', 'text', 'date', 'boolean'].map(value => ({ value, label: t(`knowledgeDomains.valueTypes.${value}`) })))
const typeOptions = computed(() => props.entityTypes.map(value => ({ value, label: value })))
const unitOptions = computed(() => props.unitFamilies.map(family => ({ value: family.key, label: `${family.key} (${family.canonical_unit})` })))
</script>
<style scoped lang="less">
.domain-field-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
label { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.full-width { grid-column: 1 / -1; }
@media (max-width: 600px) { .domain-field-grid { grid-template-columns: 1fr; } }
</style>
