<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Dialog, Field, Input, Select, Textarea } from '@/components/ui'
import type { LibraryCatalogItem } from '@/api/library'
import type { ShelfCondition, ShelfNode } from '@/api/shelf'
import type { Entity } from '@/api/entities'
import { listEntityRelations, type EntityRelation } from '@/api/relations'
import { shelfEntities, shelfWriteError, writeShelf } from '@/api/shelf-files'
import type { ShelfReviewOperation } from '@/api/shelf-review'
import ShelfConditionsEditor from './ShelfConditionsEditor.vue'
import ShelfParametersEditor from './ShelfParametersEditor.vue'
import ManagedAttributes from './ManagedAttributes.vue'

const props = defineProps<{ library: LibraryCatalogItem; node: ShelfNode; mode: string; lineId?: string }>()
const emit = defineEmits<{ close: []; saved: [] }>(), { t } = useI18n()
// Capture the revision with the input. A failed write must not silently rebase it.
const entity = props.node.entity
const name = ref(entity.canonical_name), description = ref(entity.description || ''), code = ref(entity.code || ''), aliases = ref((entity.aliases || []).join('\n'))
const attributes = ref<Record<string, unknown>>(JSON.parse(JSON.stringify(entity.attributes || {})))
const originalAttributes = JSON.stringify(attributes.value)
const stage = ref(String(entity.attributes?.current_stage || ''))
const title = computed(() => t(({ model: 'shelfModel.edit', stage: 'shelfModel.setStage', composition: 'shelfModel.assignParts', certification: 'shelfModel.addCertification' } as Record<string, string>)[props.mode]!))
const busy = ref(false), error = ref('')
const parameterFields = computed(() => (props.library.entity_schema?.fields || []).filter(field => !field.archived && field.product_line_ids?.includes(props.lineId || '')))
const fields = computed(() => (props.library.entity_schema?.fields || []).filter(field => !field.archived && (!field.kinds?.length || field.kinds.includes('model')) && !['current_stage', 'sale_statuses', 'parameter_values', 'combination_rules'].includes(field.key) && !field.product_line_ids?.length))
const certification = ref<Record<string, string>>({ name: '', standard: '', certification_object: '', region: '', cell_part_id: '', year: '', agency: '', progress: '', start_date: '', obtained_date: '', issued_date: '', validity: '', number: '', cloud_url: '' })
const conditions = ref<ShelfCondition[]>([])
const composition = props.node.shelf?.composition || []
const certLabels = computed(() => ({ agency: t('shelfModel.agency'), progress: t('shelfModel.progress'), start_date: t('shelfModel.start_date'), obtained_date: t('shelfModel.obtained_date'), issued_date: t('shelfModel.issued_date'), validity: t('shelfModel.validity'), number: t('shelfModel.number'), cloud_url: t('shelfModel.cloudLink') }))
const certFields = ['agency', 'progress', 'start_date', 'obtained_date', 'issued_date', 'validity', 'number', 'cloud_url'] as const
const parts = ref<Entity[]>([]), relations = ref<EntityRelation[]>([]), loading = ref(false)
const action = ref('add'), module = ref(''), part = ref('')
const availableParts = computed(() => action.value === 'add' ? parts.value.filter(item => (!item.attributes?.module_type || item.attributes.module_type === module.value) && !relations.value.some(relation => relation.attributes?.module_type === module.value && relation.peer.id === item.id)).map(item => ({ id: item.id, name: item.canonical_name })) : relations.value.filter(item => item.attributes?.module_type === module.value).map(item => ({ id: item.id, name: item.peer.canonical_name })))
let active = true
async function loadComposition() {
  loading.value = true; error.value = ''
  try {
    const [nodes, response] = await Promise.all([shelfEntities(props.library.id, 'part'), listEntityRelations(entity.id)])
    if (!response.success) throw new Error(response.message)
    if (active) { parts.value = nodes; relations.value = response.data.flatMap(group => group.relations).filter(relation => relation.direction === 'outgoing' && relation.relation_type === 'composed_of') }
  } catch (cause) { if (active) error.value = shelfWriteError(cause, t('shelfFiles.loadFailed')) }
  finally { if (active) loading.value = false }
}
onMounted(() => { if (props.mode === 'composition') void loadComposition() })
onBeforeUnmount(() => { active = false })
async function save() {
  if (busy.value || loading.value) return
  if (!name.value.trim() || (props.mode === 'composition' && (!module.value || !availableParts.value.some(item => item.id === part.value)))) { error.value = t('managed.required'); return }
  if (props.mode === 'certification' && (['name', 'standard', 'certification_object'].some(key => !certification.value[key]?.trim()) || (certification.value.region && !props.library.shelf_config?.regions?.some(region => region.key === certification.value.region)) || conditions.value.some(condition => !condition.module_type || !condition.part_ids.length) || (certification.value.year && (!Number.isInteger(Number(certification.value.year)) || Number(certification.value.year) <= 0)))) { error.value = t('managed.required'); return }
  busy.value = true; error.value = ''
  try {
    const stageAttributes = { ...attributes.value }; if (stage.value) stageAttributes.current_stage = stage.value; else delete stageAttributes.current_stage
    const patch = props.mode === 'stage' ? { revision: entity.revision, attributes: stageAttributes } : { revision: entity.revision, name: name.value, description: description.value, code: code.value, aliases: aliases.value.split('\n').filter(value => value.trim()), ...(JSON.stringify(attributes.value) !== originalAttributes ? { attributes: attributes.value } : {}) }
    let operation: ShelfReviewOperation = { id: 'model', type: 'update_entity', entity_id: entity.id, revision: entity.revision, patch }
    if (props.mode === 'composition') {
      const relation = relations.value.find(item => item.id === part.value)
      operation = action.value === 'add' ? { id: 'composition', type: 'save_relation', relation: { subject_id: entity.id, object_id: part.value, relation_type: 'composed_of', attributes: { module_type: module.value } } } : { id: 'composition', type: 'end_relation', relation_id: relation!.id, revision: relation!.version }
    }
    if (props.mode === 'certification') {
      const record = Object.fromEntries(Object.entries(certification.value).filter(([, value]) => value !== ''))
      operation = { id: 'certification', type: 'create_certification', certification: { ...record, name: certification.value.name, owner_id: entity.id, standard: certification.value.standard!, certification_object: certification.value.certification_object!, region: certification.value.region!, ...(certification.value.year ? { year: Number(certification.value.year) } : {}), conditions: conditions.value } }
    }
    const response = await writeShelf(props.library.id, [operation])
    if (!response.success || response.data.status !== 'confirmed') throw new Error(response.message)
    emit('saved')
  } catch (cause) { error.value = shelfWriteError(cause, t('shelfFiles.saveFailed')) }
  finally { busy.value = false }
}
</script>
<template>
  <Dialog :model-value="true" :title="title" :closable="!busy" :close-on-overlay-click="false" :close-on-esc-keydown="!busy" @update:model-value="!$event && !busy && emit('close')">
    <form class="rq-model-editor" novalidate @submit.prevent="save">
      <p v-if="loading" role="status">{{ t('common.loading') }}</p>
      <fieldset :disabled="busy || loading">
        <template v-if="mode === 'model'">
        <Field id="model-name" :label="t('ikb18.name')"><Input id="model-name" v-model="name" required /></Field>
        <Field id="model-description" :label="t('ikb18.description')"><Textarea id="model-description" v-model="description" /></Field>
        <Field id="model-code" :label="t('managed.code')"><Input id="model-code" v-model="code" /></Field>
        <Field id="model-aliases" :label="t('managed.aliases')"><Textarea id="model-aliases" v-model="aliases" /></Field>
        <ManagedAttributes v-model="attributes" :fields="fields" :library-id="library.id" kind="model" />
        <ShelfParametersEditor v-model="attributes" :fields="parameterFields" :library="library" :composition="composition" />
        </template>
        <template v-if="mode === 'certification'">
          <Field id="cert-name" :label="t('shelfReview.cert.name')"><Input id="cert-name" v-model="certification.name" required /></Field>
          <Field id="cert-standard" :label="t('shelfModel.standard')"><Select id="cert-standard" v-model="certification.standard" required><option value="" disabled>{{ t('shelfModel.standard') }}</option><option v-for="item in library.shelf_config?.standards" :key="item.key" :value="item.key">{{ item.name }}</option></Select></Field>
          <Field id="cert-object" :label="t('shelfModel.certObject')"><Input id="cert-object" v-model="certification.certification_object" required /></Field>
          <Field id="cert-region" :label="t('shelfReview.cert.region')"><Select id="cert-region" v-model="certification.region"><option value="">{{ t('shelfSettings.allRegions') }}</option><option v-for="region in library.shelf_config?.regions" :key="region.key" :value="region.key">{{ region.name }}</option></Select></Field>
          <Field id="cert-year" :label="t('shelfReview.fields.year')"><Input id="cert-year" v-model="certification.year" type="number" min="1" step="1" /></Field>
          <Field id="cert-cell" :label="t('shelfReview.fields.cell_part_id')"><Select id="cert-cell" v-model="certification.cell_part_id"><option value="">{{ t('shelfFiles.none') }}</option><option v-for="item in composition.flatMap(group => group.parts)" :key="item.id" :value="item.id">{{ item.canonical_name }}</option></Select></Field>
          <Field v-for="key in certFields" :key="key" :id="`cert-${key === 'cloud_url' ? 'cloud' : key}`" :label="certLabels[key]"><Input :id="`cert-${key === 'cloud_url' ? 'cloud' : key}`" v-model="certification[key]" :type="key.endsWith('_date') ? 'date' : key === 'cloud_url' ? 'url' : 'text'" /></Field>
          <ShelfConditionsEditor v-model="conditions" :library="library" :composition="composition" />
        </template>
        <template v-if="mode === 'composition'">
          <p>{{ t('shelfModel.compositionHint') }}</p>
          <Field id="composition-action" :label="t('shelfModel.compositionAction')"><Select id="composition-action" v-model="action" @update:model-value="part = ''"><option value="add">{{ t('shelfModel.addPart') }}</option><option value="remove">{{ t('shelfModel.removePart') }}</option></Select></Field>
          <Field id="composition-module" :label="t('shelfModel.module')"><Select id="composition-module" v-model="module" @update:model-value="part = ''"><option value="" disabled>{{ t('shelfModel.module') }}</option><option v-for="item in library.shelf_config?.module_types" :key="item.key" :value="item.key">{{ item.name }}</option></Select></Field>
          <Field id="composition-part" :label="t('shelfModel.part')"><Select id="composition-part" v-model="part"><option value="" disabled>{{ t('shelfModel.part') }}</option><option v-for="item in availableParts" :key="item.id" :value="item.id">{{ item.name }}</option></Select></Field>
          <p v-if="module && !availableParts.length">{{ t('productShelf.noRecords') }}</p>
        </template>
        <Field v-if="mode === 'stage'" id="model-stage" :label="t('shelfModel.currentStage')"><Select id="model-stage" v-model="stage"><option value="">{{ t('shelfModel.inheritedStage') }}</option><option v-for="item in library.shelf_config?.stages" :key="item.key" :value="item.key">{{ item.name }}</option></Select></Field>
      </fieldset>
      <p v-if="error" role="alert">{{ error }} <Button v-if="mode === 'composition' && !busy" @click="loadComposition">{{ t('common.retry') }}</Button></p>
      <footer><Button :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</Button><Button type="submit" variant="primary" :loading="busy" :disabled="loading">{{ t('shelfStatus.confirm') }}</Button></footer>
    </form>
  </Dialog>
</template>
<style scoped>
.rq-model-editor, .rq-model-editor > fieldset { display:grid; gap:var(--space-4); min-width:0; }
.rq-model-editor > fieldset { border:0; padding:0; margin:0; }
footer { display:flex; justify-content:flex-end; gap:var(--space-2); }
[role=alert] { color:var(--danger); margin:0; }
</style>
