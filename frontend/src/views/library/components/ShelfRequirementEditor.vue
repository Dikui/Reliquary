<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Checkbox, Dialog, Field, Input, Select } from '@/components/ui'
import { confirm } from '@/components/ui/feedback'
import type { LibraryCatalogItem } from '@/api/library'
import type { Entity } from '@/api/entities'
import { listEntityRelations, type EntityRelation } from '@/api/relations'
import type { ShelfRequirement } from '@/api/shelf'
import { shelfWriteError, writeShelf } from '@/api/shelf-files'
import type { ShelfReviewOperation } from '@/api/shelf-review'
const props = defineProps<{ library: LibraryCatalogItem; entity?: Entity; nodes: Entity[] }>()
const emit = defineEmits<{ close: []; saved: [] }>(), { t } = useI18n()
const name = ref(props.entity?.canonical_name || ''), metadata = ref<ShelfRequirement>(JSON.parse(JSON.stringify(props.entity?.attributes?.requirement || { final_due_stage: '', markets: {} })))
metadata.value.markets ||= {}
const selectedLines = ref<string[]>([]), selectedModules = ref<string[]>([]), relations = ref<EntityRelation[]>([]), loading = ref(false), loaded = ref(false), busy = ref(false), error = ref('')
let active = true
const lines = computed(() => props.nodes.filter(n => n.kind === 'product_line'))
const textFields = computed(() => [{ key: 'name_en' as const, label: t('shelfSettings.english') }, { key: 'file_category' as const, label: t('shelfFiles.category') }, { key: 'stage_label' as const, label: t('shelfSettings.stageLabel') }, { key: 'due_text' as const, label: t('shelfSettings.dueText') }, { key: 'owner' as const, label: t('shelfSettings.owner') }, { key: 'priority' as const, label: t('shelfSettings.priority') }, { key: 'source_list' as const, label: t('shelfSettings.sourceList') }, { key: 'source_version' as const, label: t('shelfSettings.sourceVersion') }])
function toggle(list: string[], id: string, checked: boolean) { const index = list.indexOf(id); if (checked && index < 0) list.push(id); else if (!checked && index >= 0) list.splice(index, 1) }
async function load() {
  loading.value = true; error.value = ''
  try {
    if (props.entity) {
      const response = await listEntityRelations(props.entity.id)
      if (!response.success) throw new Error(response.message)
      if (!active) return
      relations.value = [...new Map(response.data.flatMap(g => g.relations).filter(r => r.relation_type === 'applies_to' && r.direction === 'outgoing').map(r => [r.id, r])).values()]
      selectedLines.value = relations.value.filter(r => r.peer.kind === 'product_line').map(r => r.peer.id)
      selectedModules.value = relations.value.flatMap(r => r.peer.id === props.entity?.id ? (r.attributes?.module_types as string[] || []) : [])
    }
    loaded.value = true
  } catch { if (active) error.value = t('shelfFiles.loadFailed') }
  finally { if (active) loading.value = false }
}
async function save() {
  if (busy.value || !loaded.value) return
  if (!name.value.trim() || !metadata.value.final_due_stage) { error.value = t('shelfSettings.invalid'); return }
  busy.value = true; error.value = ''
  try {
    const attributes = { ...props.entity?.attributes, requirement: { ...metadata.value, name_zh: name.value } }
    const id = props.entity?.id || '$requirement'
    const ops: ShelfReviewOperation[] = [{ id: 'requirement', type: props.entity ? 'update_entity' : 'create_entity', ...(props.entity ? { entity_id: props.entity.id, revision: props.entity.revision, patch: { name: name.value, revision: props.entity.revision, attributes } } : { entity: { kind: 'document_requirement', name: name.value, attributes } }) }]
    for (const relation of relations.value) {
      const keep = relation.peer.kind === 'product_line' ? selectedLines.value.includes(relation.peer.id) : relation.peer.id === props.entity?.id && JSON.stringify([...(relation.attributes?.module_types as string[] || [])].sort()) === JSON.stringify([...selectedModules.value].sort())
      if (!keep) ops.push({ id: `end_${ops.length}`, type: 'end_relation', relation_id: relation.id, revision: relation.version })
    }
    for (const line of selectedLines.value) if (!relations.value.some(r => r.peer.id === line)) ops.push({ id: `line_${ops.length}`, type: 'save_relation', relation: { subject_id: id, object_id: line, relation_type: 'applies_to' } })
    if (selectedModules.value.length && !relations.value.some(r => r.peer.id === props.entity?.id && JSON.stringify([...(r.attributes?.module_types as string[] || [])].sort()) === JSON.stringify([...selectedModules.value].sort()))) ops.push({ id: 'modules', type: 'save_relation', relation: { subject_id: id, object_id: id, relation_type: 'applies_to', attributes: { module_types: selectedModules.value } } })
    const response = await writeShelf(props.library.id, ops)
    if (!response.success || response.data.status !== 'confirmed') throw new Error(response.message)
    emit('saved')
  } catch (cause) { error.value = shelfWriteError(cause, t('shelfSettings.saveFailed')) }
  finally { busy.value = false }
}
async function archive() {
  if (!props.entity || busy.value || !await confirm({ header: t('shelfSettings.archiveConfirm'), confirmBtn: t('shelfSettings.archive'), cancelBtn: t('common.cancel') }).result) return
  busy.value = true
  try { const response = await writeShelf(props.library.id, [{ id: 'archive', type: 'archive_entity', entity_id: props.entity.id, revision: props.entity.revision }]); if (!response.success || response.data.status !== 'confirmed') throw new Error(response.message); emit('saved') }
  catch (cause) { error.value = shelfWriteError(cause, t('shelfSettings.saveFailed')) }
  finally { busy.value = false }
}
onMounted(load)
onBeforeUnmount(() => { active = false })
</script>
<template>
  <Dialog :model-value="true" :title="t('shelfSettings.requirements')" :closable="!busy" :close-on-overlay-click="!busy" :close-on-esc-keydown="!busy" @update:model-value="!$event && !busy && emit('close')">
    <form class="rq-shelf-requirement" @submit.prevent="save">
      <p v-if="loading" role="status">{{ t('common.loading') }}</p><p v-if="error" role="alert" class="rq-shelf-error">{{ error }} <Button v-if="!loaded" @click="load">{{ t('common.retry') }}</Button></p>
      <fieldset :disabled="busy || loading || !loaded" class="rq-shelf-fields">
        <Field :label="t('shelfSettings.name')" id="requirement-name"><Input id="requirement-name" v-model="name" :aria-label="t('shelfSettings.name')" required /></Field>
        <Field v-for="field in textFields" :key="field.key" :label="field.label" :id="`requirement-${field.key}`"><Input :id="`requirement-${field.key}`" v-model="metadata[field.key]" /></Field>
        <Field :label="t('shelfSettings.draftDue')" id="requirement-draft"><Select id="requirement-draft" v-model="metadata.draft_due_stage"><option value="">{{ t('shelfFiles.none') }}</option><option v-for="stage in library.shelf_config?.stages" :key="stage.key" :value="stage.key">{{ stage.name }}</option></Select></Field>
        <Field :label="t('shelfSettings.finalDue')" id="requirement-final"><Select id="requirement-final" v-model="metadata.final_due_stage" required><option value="" disabled>{{ t('shelfSettings.finalDue') }}</option><option v-for="stage in library.shelf_config?.stages" :key="stage.key" :value="stage.key">{{ stage.name }}</option></Select></Field>
        <fieldset><legend>{{ t('shelfFiles.markets') }}</legend><Field v-for="region in library.shelf_config?.regions" :key="region.key" :label="region.name" :id="`market-${region.key}`"><Select :id="`market-${region.key}`" :model-value="metadata.markets![region.key] || ''" @update:model-value="$event ? metadata.markets![region.key] = $event : delete metadata.markets![region.key]"><option value="">{{ t('shelfFiles.untitled') }}</option><option value="required">{{ t('shelfSettings.required') }}</option><option value="optional">{{ t('shelfSettings.optional') }}</option><option value="not_needed">{{ t('shelfSettings.not_needed') }}</option></Select></Field></fieldset>
        <fieldset><legend>{{ t('shelfSettings.appliesTo') }} · {{ t('shelfSettings.line') }}</legend><label v-for="line in lines" :key="line.id"><Checkbox :model-value="selectedLines.includes(line.id)" @update:model-value="toggle(selectedLines, line.id, $event)" /> {{ line.canonical_name }}</label></fieldset>
        <fieldset><legend>{{ t('shelfSettings.appliesTo') }} · {{ t('shelfSettings.modules') }}</legend><label v-for="module in library.shelf_config?.module_types" :key="module.key"><Checkbox :model-value="selectedModules.includes(module.key)" @update:model-value="toggle(selectedModules, module.key, $event)" /> {{ module.name }}</label></fieldset>
      </fieldset>
      <footer><Button v-if="entity" variant="danger" :disabled="busy" @click="archive">{{ t('shelfSettings.archive') }}</Button><Button :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</Button><Button variant="primary" type="submit" :loading="busy" :disabled="!loaded">{{ t('shelfSettings.save') }}</Button></footer>
    </form>
  </Dialog>
</template>
<style scoped>
.rq-shelf-requirement, .rq-shelf-fields { display: flex; flex-direction: column; gap: var(--space-4); } .rq-shelf-fields { border: 0; padding: 0; margin: 0; } fieldset label { display: flex; gap: var(--space-2); margin-block: var(--space-2); } footer { display: flex; justify-content: flex-end; gap: var(--space-2); } .rq-shelf-error { color: var(--danger); }
</style>
