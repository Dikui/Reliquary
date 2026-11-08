<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Checkbox, Dialog, Field, Input, Select } from '@/components/ui'
import type { LibraryCatalogItem } from '@/api/library'
import { deleteEntityAttachment, uploadEntityAttachment, type Entity } from '@/api/entities'
import { shelfEntities, shelfWriteError, writeShelf, type ShelfDocumentInput } from '@/api/shelf-files'
import type { ShelfDocumentVersion } from '@/api/shelf'
const props = defineProps<{ library: LibraryCatalogItem; file?: Entity; current?: ShelfDocumentVersion; classify?: boolean }>()
const emit = defineEmits<{ close: []; saved: [fileId: string] }>(), { t } = useI18n()
const nodes = ref<Entity[]>([]), loading = ref(true), busy = ref(false), error = ref(''), chosen = ref<File>(), attachmentId = ref(''), committed = ref(false)
const owner = ref(props.current ? props.current.owner_id || 'unassigned' : ''), name = ref(props.file?.canonical_name || ''), requirement = ref(props.current?.requirement_id || ''), number = ref(props.current?.certificate_number || '')
const label = ref(''), stage = ref<'draft' | 'final'>(props.current?.stage || 'draft'), cloud = ref(''), origin = ref(''), category = ref(props.current?.file_category || '')
const conditions = ref(JSON.parse(JSON.stringify(props.current?.conditions || [])) as { module_type: string; part_ids: string[] }[])
const owners = computed(() => nodes.value.filter(n => ['model', 'product', 'platform', 'part', 'certification'].includes(n.kind || '')))
const requirements = computed(() => nodes.value.filter(n => n.kind === 'document_requirement'))
const parts = computed(() => nodes.value.filter(n => n.kind === 'part'))
const title = computed(() => props.classify ? t('shelfFiles.classify') : props.file ? t('shelfFiles.newVersion') : t('shelfFiles.upload'))
const limit = computed(() => props.library.shelf_config?.max_file_bytes || 32 * 1024 * 1024)
const selectedOwner = computed(() => nodes.value.find(node => node.id === owner.value))
let active = true
async function load() {
  loading.value = true; error.value = ''
  try { const data = await shelfEntities(props.library.id); if (active) nodes.value = data }
  catch { if (active) error.value = t('shelfFiles.loadFailed') }
  finally { if (active) loading.value = false }
}
async function choose(event: Event) {
  const input = event.target as HTMLInputElement, file = input.files?.[0]; input.value = ''
  if (!file) return
  if (!file.size) { error.value = t('shelfFiles.fileError'); return }
  if (file.size > limit.value) { error.value = t('shelfFiles.sizeError'); return }
  if (attachmentId.value) { try { const removed = await deleteEntityAttachment(attachmentId.value); if (!removed.success) throw new Error(); attachmentId.value = '' } catch { error.value = t('shelfFiles.saveFailed'); return } }
  chosen.value = file; if (!name.value) name.value = file.name; error.value = ''
}
async function save() {
  if (busy.value || loading.value) return
  error.value = ''
  if (!owner.value || !name.value.trim() || conditions.value.some(c => !c.module_type || !c.part_ids.length) || (!props.classify && !chosen.value && !cloud.value)) { error.value = t('shelfFiles.fileError'); return }
  if (cloud.value) { try { const url = new URL(cloud.value); if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw new Error() } catch { error.value = t('shelfFiles.fileError'); return } }
  const certification = selectedOwner.value?.kind === 'certification'
  if (certification && !number.value) { error.value = t('shelfFiles.fileError'); return }
  busy.value = true
  try {
    if (!props.classify && chosen.value && !attachmentId.value) {
      const upload = await uploadEntityAttachment(props.library.id, chosen.value)
      if (!upload.success) throw new Error(upload.error)
      attachmentId.value = upload.data.id
    }
    const classification = { owner_id: owner.value === 'unassigned' ? '' : owner.value, requirement_id: owner.value === 'unassigned' ? '' : requirement.value, certificate_number: certification ? number.value : '', conditions: owner.value === 'unassigned' ? [] : conditions.value }
    const document: ShelfDocumentInput = { ...classification, name: name.value, unassigned: owner.value === 'unassigned', attachment_id: attachmentId.value || undefined, cloud_url: cloud.value || undefined, stage: stage.value, version_label: label.value, origin: origin.value, file_category: category.value }
    const response = await writeShelf(props.library.id, [{ id: 'document', type: props.classify ? 'classify_document' : 'add_document_version', ...(props.file ? { entity_id: props.file.id, revision: props.file.revision } : {}), ...(props.classify ? { classification } : { document }) }])
    if (!response.success || response.data.status !== 'confirmed') throw new Error(response.message)
    committed.value = true; emit('saved', response.data.items?.find(item => item.id === 'document')?.entity_id || props.file?.id || '')
  } catch (cause) { error.value = shelfWriteError(cause, t('shelfFiles.saveFailed')) }
  finally { busy.value = false }
}
function changeOwner() { requirement.value = ''; number.value = ''; conditions.value = [] }
function selectPart(index: number, id: string, checked: boolean) { const c = conditions.value[index]!; c.part_ids = checked ? [...c.part_ids, id] : c.part_ids.filter(value => value !== id) }
onMounted(load)
onBeforeUnmount(() => { active = false; if (attachmentId.value && !committed.value) void deleteEntityAttachment(attachmentId.value).catch(() => {}) })
</script>
<template>
  <Dialog :model-value="true" :title="title" :closable="!busy" :close-on-overlay-click="!busy" :close-on-esc-keydown="!busy" @update:model-value="!$event && !busy && emit('close')">
    <form class="rq-shelf-file-form" @submit.prevent="save">
      <p v-if="loading" role="status">{{ t('common.loading') }}</p>
      <p v-if="error" role="alert" class="rq-shelf-error">{{ error }} <Button v-if="!nodes.length && !busy" @click="load">{{ t('common.retry') }}</Button></p>
      <fieldset :disabled="busy || loading" class="rq-shelf-file-fields">
        <Field :label="t('shelfFiles.fileName')" id="shelf-file-name"><Input id="shelf-file-name" v-model="name" :aria-label="t('shelfFiles.fileName')" required :disabled="!!file" /></Field>
        <Field :label="t('shelfFiles.owner')" id="shelf-file-owner"><Select id="shelf-file-owner" v-model="owner" :aria-label="t('shelfFiles.owner')" required :disabled="!!file && !classify" @update:model-value="changeOwner"><option value="" disabled>{{ t('shelfFiles.owner') }}</option><option v-if="!classify" value="unassigned">{{ t('shelfFiles.unassigned') }}</option><option v-for="node in owners" :key="node.id" :value="node.id">{{ library.entity_schema?.kinds.find(kind => kind.key === node.kind)?.name || node.kind }} · {{ node.canonical_name }}</option></Select></Field>
        <template v-if="owner && owner !== 'unassigned'">
          <Field :label="t('shelfFiles.requirement')" id="shelf-file-requirement"><Select id="shelf-file-requirement" v-model="requirement" :disabled="!!file && !classify"><option value="">{{ t('shelfFiles.none') }}</option><option v-for="node in requirements" :key="node.id" :value="node.id">{{ node.canonical_name }}</option></Select></Field>
          <Field v-if="selectedOwner?.kind === 'certification'" :label="t('shelfFiles.certificateNumber')" id="shelf-file-number"><Input id="shelf-file-number" v-model="number" required /></Field>
          <fieldset><legend>{{ t('shelfFiles.conditions') }}</legend>
            <p v-if="!conditions.length">{{ t('shelfFiles.allOptions') }}</p>
            <div v-for="(condition, index) in conditions" :key="index" class="rq-shelf-condition">
              <Select v-model="condition.module_type" :aria-label="t('shelfFiles.module')" @update:model-value="condition.part_ids = []"><option value="" disabled>{{ t('shelfFiles.module') }}</option><option v-for="module in library.shelf_config?.module_types" :key="module.key" :value="module.key">{{ module.name }}</option></Select>
              <label v-for="part in parts.filter(part => !part.attributes?.module_type || part.attributes.module_type === condition.module_type)" :key="part.id"><Checkbox :model-value="condition.part_ids.includes(part.id)" @update:model-value="selectPart(index, part.id, $event)" /> {{ part.canonical_name }}</label>
              <Button @click="conditions.splice(index, 1)">{{ t('shelfFiles.remove') }}</Button>
            </div><Button :disabled="!library.shelf_config?.module_types?.length" @click="conditions.push({ module_type: '', part_ids: [] })">{{ t('shelfFiles.addCondition') }}</Button>
          </fieldset>
        </template>
        <template v-if="!classify">
          <Field :label="t('shelfFiles.original')" id="shelf-file-original" :hint="t('shelfFiles.uploadLimit', { size: limit / 1024 / 1024 })"><input id="shelf-file-original" type="file" :aria-label="t('shelfFiles.chooseOriginal')" @change="choose"><span v-if="chosen">{{ chosen.name }}</span></Field>
          <Field :label="t('shelfFiles.cloud')" id="shelf-file-cloud"><Input id="shelf-file-cloud" v-model="cloud" type="url" :aria-label="t('shelfFiles.cloud')" /></Field>
          <Field :label="t('shelfFiles.versionLabel')" id="shelf-file-label"><Input id="shelf-file-label" v-model="label" /></Field>
          <Field :label="t('shelfFiles.stage')" id="shelf-file-stage"><Select id="shelf-file-stage" v-model="stage"><option value="draft">{{ t('shelfFiles.draft') }}</option><option value="final">{{ t('shelfFiles.final') }}</option></Select></Field>
          <Field :label="t('shelfFiles.category')" id="shelf-file-category"><Input id="shelf-file-category" v-model="category" /></Field>
          <Field :label="t('shelfFiles.origin')" id="shelf-file-origin"><Input id="shelf-file-origin" v-model="origin" /></Field>
        </template>
      </fieldset>
      <p>{{ t('shelfFiles.saveHint') }}</p><div class="rq-shelf-actions"><Button :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</Button><Button variant="primary" type="submit" :loading="busy" :disabled="loading">{{ t('shelfFiles.save') }}</Button></div>
    </form>
  </Dialog>
</template>
<style scoped>
.rq-shelf-file-form, .rq-shelf-file-fields { display: flex; flex-direction: column; gap: var(--space-4); }
.rq-shelf-file-fields { border: 0; padding: 0; margin: 0; }
.rq-shelf-condition { display: flex; flex-direction: column; gap: var(--space-2); padding: var(--space-2) 0; }
.rq-shelf-actions { display: flex; gap: var(--space-2); justify-content: flex-end; }
.rq-shelf-error { color: var(--danger); }
</style>
