<template>
  <div class="domain-template-editor">
    <label class="template-label"><span>{{ t('knowledgeDomains.entityTypes') }}</span><t-tag-input v-model="draft.entity_types" :disabled="disabled" :placeholder="t('knowledgeDomains.tagHint')" /></label>
    <t-select v-model="tab" class="template-mobile-nav" :options="tabOptions" :aria-label="t('knowledgeDomains.template')" :popup-props="{ zIndex: 6200 }" />
    <t-tabs v-model="tab" class="template-tabs">
      <t-tab-panel value="fields" :label="t('knowledgeDomains.fields')">
        <p class="template-hint">{{ t('knowledgeDomains.fieldsHint') }}</p>
        <article v-for="(field, index) in draft.fields" :key="index" class="template-entry">
          <div class="entry-heading"><strong>{{ field.label || field.key || t('knowledgeDomains.newField') }}</strong><t-button v-if="!disabled" theme="danger" variant="text" size="small" @click="draft.fields.splice(index, 1)">{{ t('common.delete') }}</t-button></div>
          <DomainFieldEditor v-model="draft.fields[index]" :entity-types="draft.entity_types" :unit-families="draft.unit_families" :disabled="disabled" />
        </article>
        <t-empty v-if="!draft.fields.length" :description="t('knowledgeDomains.noFields')" />
        <t-button v-if="!disabled" variant="outline" @click="draft.fields.push(emptyDomainField())">{{ t('knowledgeDomains.addField') }}</t-button>
      </t-tab-panel>
      <t-tab-panel value="relations" :label="t('knowledgeDomains.relations')">
        <article v-for="(relation, index) in draft.relations" :key="index" class="template-entry">
          <div class="entry-heading"><strong>{{ relation.label || relation.key || t('knowledgeDomains.newRelation') }}</strong><t-button v-if="!disabled" theme="danger" variant="text" size="small" @click="draft.relations.splice(index, 1)">{{ t('common.delete') }}</t-button></div>
          <div class="entry-grid">
            <label><span>{{ t('knowledgeDomains.key') }}</span><t-input v-model="relation.key" :disabled="disabled" /></label>
            <label><span>{{ t('knowledgeDomains.label') }}</span><t-input v-model="relation.label" :disabled="disabled" /></label>
            <label class="full-width"><span>{{ t('knowledgeDomains.aliases') }}</span><t-tag-input v-model="relation.aliases" :disabled="disabled" :placeholder="t('knowledgeDomains.tagHint')" /></label>
            <label><span>{{ t('knowledgeDomains.subjectTypes') }}</span><t-select v-model="relation.subject_types" multiple clearable :options="typeOptions" :disabled="disabled" :placeholder="t('knowledgeDomains.allTypes')" :popup-props="{ zIndex: 6200 }" /></label>
            <label><span>{{ t('knowledgeDomains.objectTypes') }}</span><t-select v-model="relation.object_types" multiple clearable :options="typeOptions" :disabled="disabled" :placeholder="t('knowledgeDomains.allTypes')" :popup-props="{ zIndex: 6200 }" /></label>
          </div>
        </article>
        <t-empty v-if="!draft.relations.length" :description="t('knowledgeDomains.noRelations')" />
        <t-button v-if="!disabled" variant="outline" @click="draft.relations.push({ key: '', label: '', aliases: [], subject_types: [], object_types: [] })">{{ t('knowledgeDomains.addRelation') }}</t-button>
      </t-tab-panel>
      <t-tab-panel value="units" :label="t('knowledgeDomains.unitFamilies')">
        <p class="template-hint">{{ t('knowledgeDomains.unitsHint') }}</p>
        <article v-for="(family, index) in draft.unit_families" :key="index" class="template-entry">
          <div class="entry-heading"><strong>{{ family.key || t('knowledgeDomains.newUnitFamily') }}</strong><t-button v-if="!disabled" theme="danger" variant="text" size="small" @click="draft.unit_families.splice(index, 1)">{{ t('common.delete') }}</t-button></div>
          <div class="entry-grid">
            <label><span>{{ t('knowledgeDomains.key') }}</span><t-input v-model="family.key" :disabled="disabled" /></label>
            <label><span>{{ t('knowledgeDomains.canonicalUnit') }}</span><t-input v-model="family.canonical_unit" :disabled="disabled" /></label>
          </div>
          <div v-for="(unit, unitIndex) in family.units" :key="unitIndex" class="unit-entry">
            <label><span>{{ t('knowledgeDomains.unitName') }}</span><t-input v-model="unit.name" :disabled="disabled" /></label>
            <label><span>{{ t('knowledgeDomains.aliases') }}</span><t-tag-input v-model="unit.aliases" :disabled="disabled" :placeholder="t('knowledgeDomains.tagHint')" /></label>
            <label><span>{{ t('knowledgeDomains.factor') }}</span><t-input-number v-model="unit.factor" :disabled="disabled" /></label>
            <t-button v-if="!disabled" theme="danger" variant="text" :aria-label="t('knowledgeDomains.deleteUnit', { name: unit.name })" @click="family.units.splice(unitIndex, 1)"><t-icon name="delete" /></t-button>
          </div>
          <t-button v-if="!disabled" variant="text" @click="family.units.push({ name: '', aliases: [], factor: 1 })">{{ t('knowledgeDomains.addUnit') }}</t-button>
        </article>
        <t-empty v-if="!draft.unit_families.length" :description="t('knowledgeDomains.noUnits')" />
        <t-button v-if="!disabled" variant="outline" @click="draft.unit_families.push({ key: '', canonical_unit: '', units: [] })">{{ t('knowledgeDomains.addUnitFamily') }}</t-button>
      </t-tab-panel>
      <t-tab-panel value="hints" :label="t('knowledgeDomains.extractionHints')">
        <label class="template-label"><span>{{ t('knowledgeDomains.hintsDescription') }}</span><SafeTextarea v-model="draft.extraction_hints" :disabled="disabled" :autosize="{ minRows: 6, maxRows: 16 }" /></label>
      </t-tab-panel>
    </t-tabs>
  </div>
</template>
<script setup lang="ts">
import SafeTextarea from '@/components/SafeTextarea.vue'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { KnowledgeDomainInput } from '@/api/knowledge-domains'
import DomainFieldEditor from './DomainFieldEditor.vue'
import { emptyDomainField } from './domainTemplate'
const draft = defineModel<KnowledgeDomainInput>({ required: true })
defineProps<{ disabled?: boolean }>()
const { t } = useI18n()
const tab = ref('fields')
const tabOptions = computed(() => [
  { value: 'fields', label: t('knowledgeDomains.fields') },
  { value: 'relations', label: t('knowledgeDomains.relations') },
  { value: 'units', label: t('knowledgeDomains.unitFamilies') },
  { value: 'hints', label: t('knowledgeDomains.extractionHints') },
])
const typeOptions = computed(() => draft.value.entity_types.map(value => ({ value, label: value })))
</script>
<style scoped lang="less">
@import '../../assets/settings-layout.less';

.domain-template-editor { display: flex; flex-direction: column; gap: 18px; min-width: 0; }
.template-mobile-nav { display: none; }
.template-label, .entry-grid label, .unit-entry label { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.template-entry { border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-large); padding: 16px; margin: 14px 0; }
.entry-heading { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; gap: 8px; overflow-wrap: anywhere; }
.entry-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
.unit-entry { display: grid; grid-template-columns: 1fr 1.5fr 110px 36px; align-items: end; gap: 10px; margin-top: 14px; }
.full-width { grid-column: 1 / -1; }
.template-hint { color: var(--td-text-color-secondary); line-height: 1.6; }
:deep(textarea) { resize: none; }
@container settings-content (max-width: @settings-narrow-width) {
  .entry-grid, .unit-entry { grid-template-columns: minmax(0, 1fr); }
  .unit-entry :deep(.t-input-number) { width: 100%; max-width: 100%; }
  .entry-heading { flex-wrap: wrap; }
  .template-tabs :deep(.t-tabs__header) { max-width: 100%; }
}

@media (max-width: 600px) {
  .template-mobile-nav { display: block; }
  .template-tabs > :deep(.t-tabs__header) { display: none; }
  .template-entry { padding: 12px; }
  .entry-grid, .unit-entry { grid-template-columns: minmax(0, 1fr); }
  .unit-entry :deep(.t-input-number) { width: 100%; }
}
</style>
