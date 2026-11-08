<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Checkbox, Field, Input, Select, Textarea } from '@/components/ui'
import { SALE_STATUSES } from '@/api/shelf'
import type { LibraryCatalogItem } from '@/api/library'
import type { Entity } from '@/api/entities'
import type { ShelfImportPlanInput, ShelfImportSheet } from '@/api/shelf-import'

const model = defineModel<ShelfImportPlanInput>({ required: true })
const props = defineProps<{ library: LibraryCatalogItem; lines: Entity[]; disabled?: boolean; submitted?: boolean }>()
const emit = defineEmits<{ validity: [valid: boolean] }>()
const { t } = useI18n()
const commonFields = ['name', 'owner', 'standard', 'year', 'certification_object', 'cell_part', 'region', 'agency', 'progress', 'start_date', 'obtained_date', 'issued_date', 'validity', 'number', 'cloud_url', 'sample_description']
const fields = computed(() => ({
  product_map: ['platform', 'product', 'model', 'description', 'sale_status', 'parameter'],
  certification_progress: commonFields,
  certification_report: [...commonFields, 'model', 'pack_model', 'cluster_model', 'container_model'],
  overseas_sales: ['owner', 'standard', 'certification_object', 'cell_part', 'region', 'agency', 'progress', 'validity', 'number', 'cloud_url', 'sale_status', 'certification'],
  atq: ['name', 'name_zh', 'name_en', 'file_category', 'stage_label', 'due_stages', 'owner', 'priority', 'source_list', 'source_version', 'market'],
})[model.value.file_kind])
const rules = reactive({
  checks: model.value.sheets.map(sheet => Object.entries(sheet.check_values || {}).map(([raw, value]) => ({ raw, value }))),
  statuses: Object.entries(model.value.status_values || {}).map(([raw, value]) => ({ raw, ...value })),
  fills: Object.entries(model.value.fill_meanings || {}).map(([raw, meaning]) => ({ raw, meaning })),
  deadlines: Object.entries(model.value.deadlines || {}).map(([raw, value]) => ({ raw, draft_stage: value.draft_stage || '', final_stage: value.final_stage })),
})
const duplicateRules = computed(() => [rules.statuses, rules.fills, rules.deadlines, ...rules.checks].some(rows => new Set(rows.map(row => row.raw)).size !== rows.length || rows.some(row => !row.raw.trim())))
watch(rules, () => {
  rules.checks.forEach((rows, index) => { if (model.value.sheets[index] && rows.length) model.value.sheets[index]!.check_values = Object.fromEntries(rows.map(row => [row.raw, row.value])); else if (model.value.sheets[index]) delete model.value.sheets[index]!.check_values })
  model.value.status_values = Object.fromEntries(rules.statuses.map(({ raw, ...value }) => [raw, value]))
  model.value.fill_meanings = Object.fromEntries(rules.fills.map(row => [row.raw, row.meaning]))
  model.value.deadlines = Object.fromEntries(rules.deadlines.map(({ raw, ...value }) => [raw, value]))
})
watch(duplicateRules, value => emit('validity', !value), { immediate: true })
function addSheet() { model.value.sheets.push({ worksheet: '', header_rows: 1, columns: [] }); rules.checks.push([]) }
function options(sheet: ShelfImportSheet, field: string) {
  if (field === 'parameter') return (props.library.entity_schema?.fields || []).filter(value => !value.archived && value.product_line_ids?.includes(sheet.product_line_id || '') && ['text', 'number', 'boolean', 'date', 'select'].includes(value.type))
  if (field === 'certification') return props.library.shelf_config?.standards || []
  return props.library.shelf_config?.regions || []
}
const usesKey = (field: string) => ['parameter', 'sale_status', 'market', 'certification'].includes(field)
</script>
<template>
  <fieldset :disabled="disabled" class="plan-editor">
    <section v-for="(sheet, index) in model.sheets" :key="index" class="import-section">
      <div class="import-row spread"><h3>{{ t('shelfImport.worksheetNumber', { n: index + 1 }) }}</h3><Button @click="model.sheets.splice(index, 1); rules.checks.splice(index, 1)">{{ t('shelfImport.remove') }}</Button></div>
      <div class="sheet-fields">
        <Field :label="t('shelfImport.worksheet')" :error="submitted && !sheet.worksheet ? t('shelfImport.planError') : undefined"><Input v-model="sheet.worksheet" :aria-label="t('shelfImport.worksheetNumber', { n: index + 1 })" /></Field>
        <label class="import-check"><Checkbox v-model="sheet.ignore" />{{ t('shelfImport.ignoreSheet') }}</label>
        <template v-if="!sheet.ignore">
          <Field :label="t('shelfImport.headerRows')" :error="submitted && (!Number.isInteger(sheet.header_rows) || sheet.header_rows < 0 || sheet.header_rows > 100) ? t('shelfImport.planError') : undefined"><Input v-model.number="sheet.header_rows" type="number" min="0" max="100" :aria-label="`${t('shelfImport.headerRows')} ${index + 1}`" /></Field>
          <Field :label="t('shelfImport.productLine')"><Select v-model="sheet.product_line_id" :aria-label="`${t('shelfImport.productLine')} ${index + 1}`"><option value="">{{ t('shelfImport.unspecified') }}</option><option v-for="line in lines" :key="line.id" :value="line.id">{{ line.canonical_name }}</option></Select></Field>
          <Field v-if="model.file_kind !== 'product_map' && model.file_kind !== 'atq'" :label="t('shelfImport.ownerKind')"><Select v-model="sheet.owner_kind" :aria-label="`${t('shelfImport.ownerKind')} ${index + 1}`"><option value="">{{ t('shelfImport.unspecified') }}</option><option v-for="kind in ['model', 'part', 'product', 'platform']" :key="kind" :value="kind">{{ t(`productShelf.kinds.${kind}`) }}</option></Select></Field>
          <Field v-if="['atq', 'certification_report'].includes(model.file_kind)" :label="t('shelfImport.module')"><Select v-model="sheet.module_type" :aria-label="`${t('shelfImport.module')} ${index + 1}`"><option value="">{{ t('shelfImport.unspecified') }}</option><option v-for="item in library.shelf_config?.module_types || []" :key="item.key" :value="item.key">{{ item.name }}</option></Select></Field>
          <Field v-if="['certification_report', 'overseas_sales'].includes(model.file_kind)" :label="t('shelfImport.completedSpelling')"><Input v-model="sheet.progress" :aria-label="`${t('shelfImport.completedSpelling')} ${index + 1}`" /></Field>
        </template>
      </div>
      <template v-if="!sheet.ignore">
        <p v-if="model.file_kind === 'overseas_sales'" class="import-hint">{{ t('shelfImport.overseasHint') }}</p>
        <p v-if="model.file_kind === 'atq'" class="import-hint">{{ t('shelfImport.bindingHint') }}</p>
        <div class="import-table"><table><thead><tr><th>{{ t('shelfImport.column') }}</th><th>{{ t('shelfImport.mappedField') }}</th><th>{{ t('shelfImport.configuredValue') }}</th><th>{{ t('shelfImport.actions') }}</th></tr></thead><tbody>
          <tr v-for="(column, ci) in sheet.columns" :key="ci">
            <td><Input v-model="column.column" :aria-invalid="submitted && !/^[A-Z]{1,3}$/.test(column.column) || undefined" :aria-describedby="submitted && !/^[A-Z]{1,3}$/.test(column.column) ? 'import-plan-error' : undefined" :aria-label="`${t('shelfImport.column')} ${index + 1}.${ci + 1}`" /></td>
            <td><Select v-model="column.field" :aria-invalid="submitted && !column.field || undefined" :aria-describedby="submitted && !column.field ? 'import-plan-error' : undefined" :aria-label="`${t('shelfImport.mappedField')} ${index + 1}.${ci + 1}`" @update:model-value="column.key = ''"><option value="">{{ t('shelfImport.unspecified') }}</option><option v-for="field in fields" :key="field" :value="field">{{ t(`shelfImport.fields.${field}`) }}</option></Select></td>
            <td><Select v-if="usesKey(column.field)" v-model="column.key" :aria-label="`${t('shelfImport.configuredValue')} ${index + 1}.${ci + 1}`"><option value="">{{ t('shelfImport.unspecified') }}</option><option v-for="option in options(sheet, column.field)" :key="option.key" :value="option.key">{{ option.name }}</option></Select></td>
            <td><Button :aria-label="`${t('shelfImport.removeColumn')} ${index + 1}.${ci + 1}`" @click="sheet.columns.splice(ci, 1)">{{ t('shelfImport.remove') }}</Button></td>
          </tr>
        </tbody></table></div>
        <Button @click="sheet.columns.push({ column: '', field: '' })">{{ t('shelfImport.addColumn') }}</Button>
        <div v-if="model.file_kind === 'overseas_sales'" class="import-section">
          <h3>{{ t('shelfImport.checkValues') }}</h3>
          <div v-for="(row, ci) in rules.checks[index]" :key="ci" class="import-row">
            <Input v-model="row.raw" :aria-label="`${t('shelfImport.raw')} ${index + 1}.${ci + 1}`" />
            <Select :model-value="String(row.value)" :aria-label="`${t('shelfImport.checkMeaning')} ${index + 1}.${ci + 1}`" @update:model-value="next => row.value = next === 'true'"><option value="true">{{ t('shelfImport.present') }}</option><option value="false">{{ t('shelfImport.absent') }}</option></Select>
            <Button @click="rules.checks[index]!.splice(ci, 1)">{{ t('shelfImport.remove') }}</Button>
          </div>
          <Button @click="rules.checks[index]!.push({ raw: '', value: false })">{{ t('shelfImport.addRule') }}</Button>
        </div>
      </template>
    </section>
    <Button @click="addSheet">{{ t('shelfImport.addSheet') }}</Button>
    <section class="import-section"><h3>{{ t('shelfImport.separators') }}</h3><p class="import-hint">{{ t('shelfImport.separatorHint') }}</p>
      <div v-for="(_, index) in model.model_separators || []" :key="index" class="import-row"><Textarea v-model="model.model_separators![index]" :aria-label="`${t('shelfImport.separator')} ${index + 1}`" :rows="2" /><Button @click="model.model_separators!.splice(index, 1)">{{ t('shelfImport.remove') }}</Button></div>
      <Button @click="(model.model_separators ||= []).push('')">{{ t('shelfImport.addSeparator') }}</Button>
    </section>
    <section class="import-section"><h3>{{ t('shelfImport.missingValues') }}</h3>
      <div v-for="(_, index) in model.missing_values || []" :key="index" class="import-row"><Input v-model="model.missing_values![index]" :aria-label="`${t('shelfImport.missingValue')} ${index + 1}`" /><Button @click="model.missing_values!.splice(index, 1)">{{ t('shelfImport.remove') }}</Button></div>
      <Button @click="(model.missing_values ||= []).push('')">{{ t('shelfImport.addMissing') }}</Button>
    </section>
    <section class="import-section"><h3>{{ t('shelfImport.statusValues') }}</h3><p class="import-hint">{{ t('shelfImport.blankHint') }}</p>
      <div v-for="(row, index) in rules.statuses" :key="index" class="import-row"><Input v-model="row.raw" :aria-label="`${t('shelfImport.statusRaw')} ${index + 1}`" /><Select v-model="row.status" :aria-label="`${t('shelfImport.statusMeaning')} ${index + 1}`"><option value="">{{ t('shelfImport.unspecified') }}</option><option v-for="status in SALE_STATUSES" :key="status" :value="status">{{ status }}</option></Select><Input v-model="row.notes" :aria-label="`${t('shelfImport.notes')} ${index + 1}`" /><Button @click="rules.statuses.splice(index, 1)">{{ t('shelfImport.remove') }}</Button></div>
      <Button @click="rules.statuses.push({ raw: '', status: '', notes: '' })">{{ t('shelfImport.addRule') }}</Button>
    </section>
    <section class="import-section"><h3>{{ t('shelfImport.fillMeanings') }}</h3><p class="import-hint">{{ t('shelfImport.fillHint') }}</p>
      <div v-for="(row, index) in rules.fills" :key="index" class="import-row"><Input v-model="row.raw" :aria-label="`${t('shelfImport.fill')} ${index + 1}`" /><Select v-model="row.meaning" :aria-label="`${t('shelfImport.fillMeaning')} ${index + 1}`"><option v-for="meaning in ['uncertain', 'ignore', 'planned_eom']" :key="meaning" :value="meaning">{{ t(`shelfImport.meanings.${meaning}`) }}</option><option v-for="status in SALE_STATUSES" :key="status" :value="status">{{ status }}</option></Select><Button @click="rules.fills.splice(index, 1)">{{ t('shelfImport.remove') }}</Button></div>
      <Button @click="rules.fills.push({ raw: '', meaning: 'uncertain' })">{{ t('shelfImport.addRule') }}</Button>
    </section>
    <section v-if="model.file_kind === 'atq'" class="import-section"><h3>{{ t('shelfImport.deadlines') }}</h3>
      <div v-for="(row, index) in rules.deadlines" :key="index" class="import-row"><Input v-model="row.raw" :aria-label="`${t('shelfImport.deadlineRaw')} ${index + 1}`" /><Select v-model="row.draft_stage" :aria-label="`${t('shelfImport.draftStage')} ${index + 1}`"><option value="">{{ t('shelfImport.unspecified') }}</option><option v-for="stage in library.shelf_config?.stages || []" :key="stage.key" :value="stage.key">{{ stage.name }}</option></Select><Select v-model="row.final_stage" :aria-label="`${t('shelfImport.finalStage')} ${index + 1}`"><option value="">{{ t('shelfImport.unspecified') }}</option><option v-for="stage in library.shelf_config?.stages || []" :key="stage.key" :value="stage.key">{{ stage.name }}</option></Select><Button @click="rules.deadlines.splice(index, 1)">{{ t('shelfImport.remove') }}</Button></div>
      <Button @click="rules.deadlines.push({ raw: '', draft_stage: '', final_stage: '' })">{{ t('shelfImport.addRule') }}</Button>
    </section>
    <p v-if="duplicateRules" role="alert">{{ t('shelfImport.ruleError') }}</p>
  </fieldset>
</template>
<style scoped>
.plan-editor { display: grid; gap: var(--space-5); border: 0; padding: 0; margin: 0; min-width: 0; }
.import-section { display: grid; gap: var(--space-3); min-width: 0; }
.import-section > :deep(button), .plan-editor > :deep(button) { justify-self: start; }
.import-row, .sheet-fields { display: flex; align-items: end; gap: var(--space-3); flex-wrap: wrap; }
.spread { justify-content: space-between; align-items: center; }
.sheet-fields > * { flex: 1 1 180px; }
.import-row > :deep(input), .import-row > :deep(select), .import-row > :deep(textarea) { flex: 1; min-width: 160px; }
.import-check { display: flex; align-items: center; gap: var(--space-2); min-height: var(--control); }
.import-table { overflow: auto; border-top: 2px solid var(--rule); }
table { border-collapse: collapse; width: 100%; min-width: 650px; }
th, td { text-align: left; padding: var(--space-2); border-bottom: 1px solid var(--border); }
th { font-size: var(--text-xs); } td:first-child { width: 110px; } td:last-child { width: 85px; }
h3 { margin: 0; font-size: var(--text-lg); } .import-hint { margin: 0; color: var(--muted); font-size: var(--text-sm); }
</style>
