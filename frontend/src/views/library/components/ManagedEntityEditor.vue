<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Dialog, Input, Select, Textarea, confirm, toast } from '@/components/ui'
import { createManagedEntity, updateManagedEntity, deleteManagedEntity, entityHistory, type Entity, type ManagedEntityInput } from '@/api/entities'
import type { LibraryCatalogItem } from '@/api/library'
import { OPERATION_TYPES, type OperationLogsData } from '@/api/operation-logs'
import { currentNameOverrides } from '@/utils/entityAutoMatch'
import ManagedNameMatching from './ManagedNameMatching.vue'
import ManagedAttributes from './ManagedAttributes.vue'
import ManagedAttributeHistory from './ManagedAttributeHistory.vue'
import { formatDateTime } from '@/utils/dateTime'
const props = defineProps<{ library: LibraryCatalogItem; entity?: Entity | null; initialKind?: string; allowedKinds?: string[]; triggerLabel?: string; compact?: boolean }>()
const emit = defineEmits<{ saved: [entity: Entity]; deleted: [] }>()
const { t, locale } = useI18n()
const formId = useId()
function operationName(value: string) { return OPERATION_TYPES.includes(value as typeof OPERATION_TYPES[number]) ? t(`operationLogs.types.${value}`) : value }
const open = ref(false), busy = ref(false), error = ref(''), attempted = ref(false)
const draft = ref<ManagedEntityInput>({ name: '', kind: '', code: '', description: '', aliases: [], attributes: {} })
const effectiveAt = ref('')
const aliases = ref(''), baseline = ref(''), originalAttributes = ref('')
const history = ref<OperationLogsData | null>(null), historyPage = ref(1), historyOpen = ref(false), historyBusy = ref(false)
let request = 0
const kinds = computed(() => (props.library.entity_schema?.kinds || []).filter(kind => !props.allowedKinds || props.allowedKinds.includes(kind.key) || kind.key === props.entity?.kind))
const fields = computed(() => (props.library.entity_schema?.fields || []).filter(field => !field.archived && (!field.kinds?.length || field.kinds.includes(draft.value.kind))))
function begin() {
  const e = props.entity
  effectiveAt.value = ''
  draft.value = { name: e?.canonical_name || '', kind: e?.kind || kinds.value.find(kind => kind.key === props.initialKind)?.key || kinds.value[0]?.key || '', code: e?.code || '', description: e?.description || '', aliases: [...(e?.aliases || [])], attributes: { ...e?.attributes }, auto_match: { ...e?.auto_match } }
  originalAttributes.value = JSON.stringify(Object.entries(draft.value.attributes).sort())
  aliases.value = draft.value.aliases.join('\n'); baseline.value = JSON.stringify(draft.value) + aliases.value + effectiveAt.value; error.value = ''; attempted.value = false; open.value = true
}
const invalid = computed(() => !draft.value.name.trim() || !draft.value.kind)
async function save() {
  if (busy.value) return
  attempted.value = true
  if (invalid.value) { error.value = t('managed.required'); return }
  busy.value = true; error.value = ''
  try {
    const attributes = Object.fromEntries(Object.entries(draft.value.attributes).filter(([, value]) => value !== '' && value !== null && value !== undefined))
    const data = { ...draft.value, auto_match: currentNameOverrides([draft.value.name, draft.value.code, ...aliases.value.split('\n')], draft.value.auto_match), ...(effectiveAt.value ? { effective_at: new Date(effectiveAt.value).toISOString() } : {}), attributes, aliases: aliases.value.split('\n').map(s => s.trim()).filter(Boolean) }
    const { attributes: savedAttributes, effective_at: effective, ...identity } = data
    const attributePatch = JSON.stringify(Object.entries(savedAttributes).sort()) === originalAttributes.value ? {} : { attributes: savedAttributes, ...(effective ? { effective_at: effective } : {}) }
    const response = props.entity ? await updateManagedEntity(props.entity.id, { ...identity, ...attributePatch, revision: props.entity.revision! }) : await createManagedEntity(props.library.id, data)
    if (!response.success) throw new Error(response.error || t('common.operationFailed'))
    emit('saved', response.data); open.value = false; toast.success(t('common.save'))
  } catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
  finally { busy.value = false }
}
function close() {
  if (busy.value) return
  if (baseline.value === JSON.stringify(draft.value) + aliases.value + effectiveAt.value) { open.value = false; return }
  confirm({ header: t('managed.edit'), body: t('ikb18.unsaved'), onConfirm: () => { open.value = false } })
}
function archive() {
  const entity = props.entity
  if (!entity || busy.value) return
  confirm({ header: t(entity.archived_at ? 'managed.restore' : 'managed.archive'), body: `${entity.canonical_name}\n${t('managed.archiveHint')}`, onConfirm: async () => {
    busy.value = true
    try { const result = await updateManagedEntity(entity.id, { revision: entity.revision!, archived: !entity.archived_at }); if (!result.success) throw new Error(result.error); emit('saved', result.data) }
    finally { busy.value = false }
  } })
}
function remove() {
  const entity = props.entity
  if (!entity || busy.value) return
  confirm({ header: t('managed.delete'), body: t('managed.deleteHint', { name: entity.canonical_name }), theme: 'danger', confirmBtn: t('managed.delete'), onConfirm: async () => {
    busy.value = true; error.value = ''
    try { const result = await deleteManagedEntity(entity.id, entity.revision!); if (!result.success) throw new Error(result.error); emit('deleted'); toast.success(t('managed.deleted')) }
    catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
    finally { busy.value = false }
  } })
}
watch(() => draft.value.kind, (kind, before) => {
  if (!open.value || kind === before) return
  const allowed = new Set((props.library.entity_schema?.fields || []).filter(field => !field.kinds?.length || field.kinds.includes(kind)).map(field => field.key))
  draft.value.attributes = Object.fromEntries(Object.entries(draft.value.attributes).filter(([key]) => allowed.has(key)))
}, { flush: 'sync' })
async function loadHistory(page = 1) {
  if (!props.entity) return
  const current = ++request; historyOpen.value = true; historyBusy.value = true; error.value = ''
  try { const result = await entityHistory(props.entity.id, page); if (!result.success) throw new Error(result.error); if (current === request) { history.value = result.data; historyPage.value = page } }
  catch (cause: any) { if (current === request) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === request) historyBusy.value = false }
}
watch(() => [props.entity?.id, props.library.id], () => { request++; historyOpen.value = false; open.value = false; history.value = null })
onBeforeUnmount(() => { request++ })
</script>
<template>
  <div class="managed-actions">
    <Button :disabled="busy" @click="begin">{{ triggerLabel || t(entity ? 'managed.edit' : 'managed.create') }}</Button>
    <template v-if="entity && !compact"><ManagedAttributeHistory :entity="entity" :library="library" /><Button :disabled="busy" @click="archive">{{ t(entity.archived_at ? 'managed.restore' : 'managed.archive') }}</Button><Button @click="loadHistory()">{{ t('managed.history') }}</Button><Button variant="danger" :disabled="busy" @click="remove">{{ t('managed.delete') }}</Button></template>
    <p v-if="error && !open && !historyOpen" role="alert">{{ error }}</p>
    <Dialog :model-value="open" :title="t(entity ? 'managed.edit' : 'managed.create')" :closable="!busy" :close-on-overlay-click="false" :close-on-esc-keydown="false" @update:model-value="close">
      <form :id="formId" class="managed-form" novalidate @submit.prevent="save"><fieldset :disabled="busy">
        <label>{{ t('ikb18.name') }}<Input v-model="draft.name" :aria-invalid="attempted && !draft.name.trim()" /></label>
        <label>{{ t('managed.kind') }}<Select v-model="draft.kind" :aria-invalid="attempted && !draft.kind"><option v-for="kind in kinds" :key="kind.key" :value="kind.key">{{ kind.name }}</option></Select></label>
        <label>{{ t('managed.code') }}<Input v-model="draft.code" /></label>
        <label>{{ t('ikb18.description') }}<Textarea v-model="draft.description" :rows="4" /></label>
        <label>{{ t('managed.aliases') }}<Textarea v-model="aliases" :rows="3" /></label>
        <ManagedNameMatching :model-value="draft.auto_match || {}" :name="draft.name" :code="draft.code" :aliases="aliases.split('\n')" @update:model-value="draft.auto_match = $event" />
        <label>{{ t('managed.effectiveAt') }}<input v-model="effectiveAt" type="datetime-local" class="rq-control"></label><p>{{ t('managed.effectiveHint') }}</p>
        <ManagedAttributes v-model="draft.attributes" :fields="fields" :library-id="library.id" :kind="draft.kind" />
        <p v-if="error" role="alert">{{ error }}</p>

      </fieldset></form>
      <template #footer><Button type="submit" :form="formId" variant="primary" :loading="busy">{{ t('common.save') }}</Button><Button :disabled="busy" @click="close">{{ t('common.cancel') }}</Button></template>
    </Dialog>
    <Dialog v-model="historyOpen" :title="t('managed.history')">
      <p v-if="historyBusy" role="status">{{ t('common.loading') }}</p><p v-if="error" role="alert">{{ error }} <Button @click="loadHistory(historyPage)">{{ t('common.retry') }}</Button></p>
      <p v-if="history && !history.items.length">{{ t('managed.noHistory') }}</p>
      <ol><li v-for="item in history?.items" :key="item.id"><strong>{{ operationName(item.operation_type) }}</strong> · {{ formatDateTime(item.created_at, locale) }} · {{ item.actor_name || item.actor }}<details><summary>{{ t('managed.changes') }}</summary><pre>{{ JSON.stringify({ before: item.before, after: item.after }, null, 2) }}</pre></details></li></ol>
      <div class="managed-actions"><Button :disabled="historyBusy || historyPage <= 1" @click="loadHistory(historyPage - 1)">{{ t('follows.previousPage') }}</Button><span>{{ historyPage }}</span><Button :disabled="historyBusy || !history || historyPage * 20 >= history.total" @click="loadHistory(historyPage + 1)">{{ t('follows.nextPage') }}</Button></div>
    </Dialog>
  </div>
</template>
<style scoped>
.managed-actions { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: center; }
.managed-form fieldset { border: 0; padding: 0; display: grid; gap: var(--space-3); min-width: 0; }
.managed-form label { display: grid; gap: var(--space-1); } pre { white-space: pre-wrap; overflow-wrap: anywhere; } [role='alert'] { color: var(--danger); }
</style>
