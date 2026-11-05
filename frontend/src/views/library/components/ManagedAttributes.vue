<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Combobox, Select, Textarea, toast } from '@/components/ui'
import { listLibraries, type EntityField, type LibraryCatalogItem } from '@/api/library'
import { getEntity, listEntities, getEntityAttachment, uploadEntityAttachment, deleteEntityAttachment, attachmentDownloadURL, type Entity, type EntityAttachment } from '@/api/entities'
import { getDown } from '@/utils/request'
const props = withDefaults(defineProps<{ fields: EntityField[]; libraryId: string; kind?: string; readonly?: boolean }>(), { readonly: false })
const model = defineModel<Record<string, unknown>>({ required: true })
const { t } = useI18n()
const instanceId = useId()
const referenceNames = ref<Record<string, string>>({})
const visible = computed(() => (props.fields || []).filter(f => (!f.archived || props.readonly) && (!props.kind || !f.kinds?.length || f.kinds.includes(props.kind))))
const libraries = ref<LibraryCatalogItem[]>([]), referenceLibrary = ref(''), entities = ref<Entity[]>([]), more = ref(false), loading = ref(false), error = ref(''), uploading = ref('')
const attachments = ref<Record<string, EntityAttachment>>({})
const entityOptions = computed(() => entities.value.map(e => ({ value: e.id, label: e.canonical_name })))
let request = 0, metadataRequest = 0, active = true
const uploaded = new Set<string>()
async function releaseUpload(id: string) {
  try { const result = await deleteEntityAttachment(id); if (!result.success) throw new Error(result.error) }
  catch (cause: any) { if (cause.status !== 409) toast.error(cause.message || t('common.operationFailed')) }
}
async function loadEntities(append = false) {
  const current = ++request; loading.value = true
  try { const result = await listEntities({ library_id: referenceLibrary.value, limit: 100, offset: append ? entities.value.length : 0 }); if (!result.success) throw new Error(result.error); if (current === request) { entities.value = append ? [...entities.value, ...result.data] : result.data; more.value = result.data.length === 100 } }
  catch (cause: any) { if (current === request) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === request) loading.value = false }
}
function setValue(key: string, value: unknown) { model.value = { ...model.value, [key]: value } }
function clear(key: string) { const next = { ...model.value }; delete next[key]; model.value = next }
function lines(value: unknown) { return Array.isArray(value) ? value.join('\n') : '' }
async function upload(key: string, event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]; if (!file) return
  uploading.value = key; error.value = ''
  try { const response = await uploadEntityAttachment(props.libraryId, file); if (!response.success) throw new Error(response.error); uploaded.add(response.data.id); if (!active) { void releaseUpload(response.data.id); return }; attachments.value[response.data.id] = response.data; setValue(key, response.data.id) }
  catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
  finally { uploading.value = ''; (event.target as HTMLInputElement).value = '' }
}
async function download(id: string) {
  error.value = ''
  try { const blob = await getDown(attachmentDownloadURL(id)); const url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url; link.download = attachments.value[id]?.name || id; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000) }
  catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
}
watch(() => visible.value.some(f => f.type === 'entity_ref'), async needed => {
  if (!needed) return
  try { const response = await listLibraries(); if (!response.success) throw new Error(response.message); libraries.value = response.data.filter(l => l.entity_mode === 'managed'); referenceLibrary.value = libraries.value.find(l => l.id === props.libraryId)?.id || libraries.value[0]?.id || '' }
  catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
}, { immediate: true })
watch(referenceLibrary, id => { entities.value = []; if (id) void loadEntities() })
watch(() => visible.value.filter(f => f.type === 'attachment').map(f => model.value[f.key]), async ids => {
  const current = ++metadataRequest
  await Promise.all(ids.filter((id): id is string => typeof id === 'string' && !!id && !attachments.value[id]).map(async id => { try { const response = await getEntityAttachment(id); if (response.success && current === metadataRequest) attachments.value[id] = response.data } catch { /* Keep the file ID available when metadata cannot be loaded. */ } }))
}, { immediate: true })
watch(() => visible.value.filter(f => f.type === 'entity_ref').map(f => model.value[f.key]), async ids => {
  await Promise.all(ids.filter((id): id is string => typeof id === 'string' && !!id).map(async id => { try { const response = await getEntity(id); if (response.success) referenceNames.value[id] = response.data.entity.canonical_name } catch { /* An unavailable reference retains its ID. */ } }))
}, { immediate: true })
onBeforeUnmount(() => { active = false; request++; metadataRequest++; for (const id of uploaded) void releaseUpload(id) })
</script>
<template>
  <div class="managed-attributes">
    <div v-for="field in visible" :key="field.key" class="attribute-field">
      <label :for="`${instanceId}-attribute-${field.key}`">{{ field.name }} <span v-if="field.unit">({{ field.unit }})</span></label>
      <template v-if="readonly">
        <Button v-if="field.type === 'attachment' && model[field.key]" variant="ghost" @click="download(String(model[field.key]))">{{ attachments[String(model[field.key])]?.name || model[field.key] }}</Button>
        <a v-else-if="field.type === 'url' && /^https?:\/\//.test(String(model[field.key]))" :href="String(model[field.key])" target="_blank" rel="noopener noreferrer">{{ model[field.key] }}</a>
        <a v-else-if="field.type === 'entity_ref' && model[field.key]" :href="`/platform/entities?entity_id=${encodeURIComponent(String(model[field.key]))}`">{{ referenceNames[String(model[field.key])] || entities.find(e => e.id === model[field.key])?.canonical_name || model[field.key] }}</a>
        <span v-else>{{ typeof model[field.key] === 'boolean' ? t(model[field.key] ? 'managed.yes' : 'managed.no') : Array.isArray(model[field.key]) ? (model[field.key] as string[]).join(' · ') : model[field.key] ?? '—' }}</span>
      </template>
      <template v-else>
        <Select v-if="field.type === 'select'" :id="`${instanceId}-attribute-${field.key}`" :model-value="String(model[field.key] ?? '')" @update:model-value="$event ? setValue(field.key, $event) : clear(field.key)"><option value="">—</option><option v-for="option in field.options" :key="option" :value="option">{{ option }}</option></Select>
        <fieldset v-else-if="field.type === 'multiselect'"><label v-for="option in field.options" :key="option"><input type="checkbox" :checked="Array.isArray(model[field.key]) && (model[field.key] as string[]).includes(option)" @change="setValue(field.key, ($event.target as HTMLInputElement).checked ? [...((model[field.key] as string[]) || []), option] : ((model[field.key] as string[]) || []).filter(v => v !== option))">{{ option }}</label></fieldset>
        <Textarea v-else-if="field.type === 'list'" :id="`${instanceId}-attribute-${field.key}`" :model-value="lines(model[field.key])" :rows="3" @update:model-value="setValue(field.key, $event.split('\n').map(v => v.trim()).filter(Boolean))" />
        <template v-else-if="field.type === 'entity_ref'"><Select v-model="referenceLibrary" :aria-label="t('managed.targetLibrary')"><option v-for="library in libraries" :key="library.id" :value="library.id">{{ library.name }}</option></Select><Combobox :id="`${instanceId}-attribute-${field.key}`" :model-value="String(model[field.key] || '')" :options="entityOptions" :disabled="loading" @update:model-value="setValue(field.key, $event)" /><Button v-if="more" :loading="loading" @click="loadEntities(true)">{{ t('workbench.loadMore') }}</Button></template>
        <template v-else-if="field.type === 'attachment'"><Button v-if="model[field.key]" variant="ghost" @click="download(String(model[field.key]))">{{ attachments[String(model[field.key])]?.name || model[field.key] }}</Button><input :id="`${instanceId}-attribute-${field.key}`" type="file" :disabled="!!uploading" @change="upload(field.key, $event)"><span v-if="uploading === field.key" role="status">{{ t('common.loading') }}</span></template>
        <input v-else-if="field.type === 'boolean'" :id="`${instanceId}-attribute-${field.key}`" type="checkbox" :checked="model[field.key] === true" @change="setValue(field.key, ($event.target as HTMLInputElement).checked)">
        <input v-else :id="`${instanceId}-attribute-${field.key}`" :value="model[field.key] ?? ''" :type="field.type === 'number' ? 'number' : field.type === 'date' ? 'date' : field.type === 'url' ? 'url' : 'text'" step="any" class="rq-control" @input="setValue(field.key, field.type === 'number' && ($event.target as HTMLInputElement).value !== '' ? Number(($event.target as HTMLInputElement).value) : ($event.target as HTMLInputElement).value)">
        <Button variant="ghost" @click="clear(field.key)">{{ t('common.clear') }}</Button>
      </template>
    </div>
    <p v-if="error" role="alert">{{ error }}</p>
  </div>
</template>
<style scoped>
.managed-attributes,.attribute-field { display:grid; gap:var(--space-2); min-width:0; } .managed-attributes { gap:var(--space-4); } fieldset { border:1px solid var(--border); display:flex; flex-wrap:wrap; gap:var(--space-3); } fieldset label { display:flex; align-items:center; gap:var(--space-1); } a,span { overflow-wrap:anywhere; } [role=alert] { color:var(--danger); }
</style>
