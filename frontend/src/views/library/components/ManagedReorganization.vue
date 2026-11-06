<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Dialog, Combobox, Input, Select, Textarea, confirm, toast } from '@/components/ui'
import { getEntity, listEntities, mergeManagedEntity, splitManagedEntity, type Entity, type EntityEntry, type ManagedEntityInput } from '@/api/entities'
import { listEntityRelations, type EntityRelation } from '@/api/relations'
import type { LibraryCatalogItem } from '@/api/library'
import { currentNameOverrides } from '@/utils/entityAutoMatch'
import ManagedNameMatching from './ManagedNameMatching.vue'
import ManagedAttributes from './ManagedAttributes.vue'
const props = defineProps<{ entity: Entity; library: LibraryCatalogItem }>()
const emit = defineEmits<{ saved: [entity: Entity] }>()
const { t } = useI18n()
const open = ref(false), mode = ref<'merge' | 'split'>('merge'), loading = ref(false), busy = ref(false), error = ref(''), more = ref(false)
const query = ref('')
const candidates = ref<Entity[]>([]), sourceId = ref(''), source = ref<Entity>(), entries = ref<EntityEntry[]>([]), relations = ref<EntityRelation[]>([]), entryIds = ref<string[]>([]), relationIds = ref<string[]>([]), effectiveAt = ref('')
const copyRelationIds = ref<string[]>([]), documentRelationPreference = ref<'source' | 'target'>('target')
const dropRelationIds = ref<string[]>([]), sourceRelations = ref<EntityRelation[]>([])
const draft = ref<ManagedEntityInput>({ name: '', kind: '', code: '', description: '', aliases: [], attributes: {} }), aliases = ref('')
const selectedSource = ref<Entity>()
const options = computed(() => {
  const rows = [...candidates.value]
  if (selectedSource.value && !rows.some(e => e.id === selectedSource.value!.id)) rows.push(selectedSource.value)
  return rows.filter(e => e.id !== props.entity.id).map(e => ({ value: e.id, label: `${e.canonical_name} · ${e.code || e.id}` }))
})
const fields = computed(() => (props.library.entity_schema?.fields || []).filter(f => !f.archived && (!f.kinds?.length || f.kinds.includes(mode.value === 'merge' ? props.entity.kind || '' : draft.value.kind))))
const archivedTargetAttributes = computed(() => Object.fromEntries((props.library.entity_schema?.fields || [])
  .filter(field => field.archived && Object.hasOwn(props.entity.attributes || {}, field.key))
  .map(field => [field.key, props.entity.attributes![field.key]])))
const excludedSourceAttributes = computed(() => Object.entries(source.value?.attributes || {})
  .filter(([key]) => !fields.value.some(field => field.key === key)))
function attributeName(key: string) { return props.library.entity_schema?.fields.find(field => field.key === key)?.name || key }
function attributeValue(value: unknown) { return typeof value === 'string' && value !== '' ? value : JSON.stringify(value) }
function mergeAttributes(attributes: Record<string, unknown> = {}) {
  const combined = { ...attributes, ...props.entity.attributes }
  return { ...Object.fromEntries(fields.value.filter(field => Object.hasOwn(combined, field.key)).map(field => [field.key, combined[field.key]])), ...archivedTargetAttributes.value }
}
let request = 0, sourceRequest = 0
async function loadCandidates(append = false) {
  const current = ++request; loading.value = true
  try { const result = await listEntities({ library_id: props.library.id, entity: query.value, limit: 100, offset: append ? candidates.value.length : 0 }); if (!result.success) throw new Error(result.error); if (current === request) { candidates.value = append ? [...candidates.value, ...result.data] : result.data; more.value = result.data.length === 100 } }
  catch (cause: any) { if (current === request) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === request) loading.value = false }
}
function search(value: string) { query.value = value.trim(); candidates.value = []; more.value = false; void loadCandidates() }
async function begin(value: 'merge' | 'split') {
  query.value = ''; candidates.value = []; more.value = false
  mode.value = value; open.value = true; error.value = ''; sourceId.value = ''; source.value = undefined; dropRelationIds.value = []; sourceRelations.value = []; entryIds.value = []; relationIds.value = []; copyRelationIds.value = []; documentRelationPreference.value = 'target'; effectiveAt.value = ''; aliases.value = ''
  draft.value = { name: '', code: '', kind: props.entity.kind || '', description: '', aliases: [], attributes: { ...props.entity.attributes } }
  if (value === 'merge') draft.value.attributes = mergeAttributes()
  if (value === 'merge') { await loadCandidates(); return }
  const current = ++request; loading.value = true
  try { const [detail, edges] = await Promise.all([getEntity(props.entity.id), listEntityRelations(props.entity.id)]); if (!detail.success || !edges.success) throw new Error(t('common.operationFailed')); if (current === request) { entries.value = detail.data.entries; relations.value = edges.data.flatMap(g => g.relations) } }
  catch (cause: any) { if (current === request) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === request) loading.value = false }
}
watch(sourceId, async id => {
  selectedSource.value = candidates.value.find(e => e.id === id)
  const current = ++sourceRequest; source.value = undefined; sourceRelations.value = []; dropRelationIds.value = []; error.value = ''
  if (mode.value === 'merge') draft.value.attributes = mergeAttributes()
  if (!id) { loading.value = false; return }
  loading.value = true
  try { const [result, edges] = await Promise.all([getEntity(id), listEntityRelations(id)]); if (!result.success || !edges.success) throw new Error(result.error || edges.message || t('common.operationFailed')); if (current === sourceRequest) { source.value = result.data.entity; selectedSource.value = result.data.entity; sourceRelations.value = edges.data.flatMap(group => group.relations); draft.value.attributes = mergeAttributes(result.data.entity.attributes) } }
  catch (cause: any) { if (current === sourceRequest) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === sourceRequest) loading.value = false }
})
async function apply() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try {
    const effective_at = effectiveAt.value ? new Date(effectiveAt.value).toISOString() : undefined
    const attributes = Object.fromEntries(Object.entries(draft.value.attributes).filter(([key, v]) => v !== '' && v != null && (mode.value !== 'merge' || fields.value.some(field => field.key === key))))
    if (mode.value === 'merge') Object.assign(attributes, archivedTargetAttributes.value)
    const result = mode.value === 'merge'
      ? await mergeManagedEntity(props.entity.id, { revision: props.entity.revision!, source_id: source.value!.id, source_revision: source.value!.revision!, drop_relation_ids: dropRelationIds.value, document_relation_preference: documentRelationPreference.value, attributes, effective_at })
      : await splitManagedEntity(props.entity.id, { revision: props.entity.revision!, new_entity: { ...draft.value, auto_match: currentNameOverrides([draft.value.name, draft.value.code, ...aliases.value.split('\n')], draft.value.auto_match), attributes, aliases: aliases.value.split('\n').map(s => s.trim()).filter(Boolean) }, entry_ids: entryIds.value, relation_ids: relationIds.value, copy_relation_ids: copyRelationIds.value, effective_at })
    if (!result.success) throw new Error(result.error || t('common.operationFailed'))
    open.value = false; emit('saved', result.data); toast.success(t('common.save'))
  } catch (cause: any) { error.value = cause.message || t('common.operationFailed') }
  finally { busy.value = false }
}
function submit() {
  if (loading.value || busy.value) return
  if (mode.value === 'merge' ? !source.value : !draft.value.name.trim() || !draft.value.kind) { error.value = t('managed.required'); return }
  confirm({ header: t(mode.value === 'merge' ? 'managed.merge' : 'managed.split'), body: t('managed.reorganizeConfirm', { name: props.entity.canonical_name }) + (mode.value === 'merge' && dropRelationIds.value.length ? '\n' + t('managed.dropRelationsConfirm', { count: dropRelationIds.value.length }) : ''), onConfirm: apply })
}
watch(() => draft.value.kind, (kind, before) => {
  if (!open.value || mode.value !== 'split' || kind === before) return
  const allowed = new Set((props.library.entity_schema?.fields || []).filter(field => !field.kinds?.length || field.kinds.includes(kind)).map(field => field.key))
  draft.value.attributes = Object.fromEntries(Object.entries(draft.value.attributes).filter(([key]) => allowed.has(key)))
}, { flush: 'sync' })
watch(() => props.library.id, () => { request++; sourceRequest++; open.value = false; query.value = ''; candidates.value = []; more.value = false; sourceId.value = ''; source.value = undefined })
onBeforeUnmount(() => { request++; sourceRequest++ })
</script>
<template>
  <div class="reorganization-actions"><Button :disabled="!!entity.archived_at" @click="begin('merge')">{{ t('managed.merge') }}</Button><Button :disabled="!!entity.archived_at" @click="begin('split')">{{ t('managed.split') }}</Button></div>
  <Dialog v-model="open" :title="t(mode === 'merge' ? 'managed.merge' : 'managed.split')" :closable="!busy" :close-on-overlay-click="false" :close-on-esc-keydown="false">
    <form novalidate class="reorganization-form" @submit.prevent="submit"><fieldset :disabled="busy">
      <p>{{ t('managed.reorganizeHint') }}</p>
      <template v-if="mode === 'merge'"><p>{{ t('managed.mergeTarget') }}: {{ entity.canonical_name }}</p><label>{{ t('managed.mergeSource') }}<Combobox :key="library.id" v-model="sourceId" :options="options" :aria-busy="loading" remote @search="search" /></label><Button v-if="more" :loading="loading" @click="loadCandidates(true)">{{ t('workbench.loadMore') }}</Button><label>{{ t('managed.documentRelationPreference') }}<Select v-model="documentRelationPreference"><option value="target">{{ t('managed.preferTarget') }}</option><option value="source">{{ t('managed.preferSource') }}</option></Select></label><template v-if="source"><h3>{{ t('managed.dropRelations') }}</h3><p>{{ t('managed.dropRelationsHint') }}</p><p v-if="!sourceRelations.length">{{ t('ikb18.noRelations') }}</p><label v-for="relation in sourceRelations" :key="relation.id" class="choice"><input v-model="dropRelationIds" type="checkbox" :value="relation.id">{{ relation.direction === 'outgoing' ? '→' : '←' }} {{ relation.relation_type }} · {{ relation.peer.canonical_name }}</label></template></template>
      <template v-else><label>{{ t('ikb18.name') }}<Input v-model="draft.name" /></label><label>{{ t('managed.kind') }}<Select v-model="draft.kind"><option v-for="kind in library.entity_schema?.kinds" :key="kind.key" :value="kind.key">{{ kind.name }}</option></Select></label><label>{{ t('managed.code') }}<Input v-model="draft.code" /></label><label>{{ t('ikb18.description') }}<Textarea v-model="draft.description" :rows="3" /></label><label>{{ t('managed.aliases') }}<Textarea v-model="aliases" :rows="3" /></label>
        <ManagedNameMatching :model-value="draft.auto_match || {}" :name="draft.name" :code="draft.code" :aliases="aliases.split('\n')" @update:model-value="draft.auto_match = $event" />
        <h3>{{ t('managed.moveDocuments') }}</h3><p v-if="!entries.length">{{ t('workbench.entities.noArticles') }}</p><label v-for="entry in entries" :key="entry.id" class="choice"><input v-model="entryIds" type="checkbox" :value="entry.id">{{ entry.title || entry.file_name || entry.id }}</label>
        <h3>{{ t('managed.moveRelations') }}</h3><p v-if="!relations.length">{{ t('ikb18.noRelations') }}</p><label v-for="relation in relations" :key="relation.id" class="choice"><input v-model="relationIds" type="checkbox" :value="relation.id" :disabled="copyRelationIds.includes(relation.id)">{{ relation.direction === 'outgoing' ? '→' : '←' }} {{ relation.relation_type }} · {{ relation.peer.canonical_name }}</label>
        <h3>{{ t('managed.copyRelations') }}</h3><label v-for="relation in relations" :key="`copy-${relation.id}`" class="choice"><input v-model="copyRelationIds" type="checkbox" :value="relation.id" :disabled="relationIds.includes(relation.id)">{{ relation.direction === 'outgoing' ? '→' : '←' }} {{ relation.relation_type }} · {{ relation.peer.canonical_name }}</label>
      </template>
      <h3>{{ t('managed.resultAttributes') }}</h3><ManagedAttributes v-model="draft.attributes" :fields="fields" :library-id="library.id" :kind="mode === 'merge' ? entity.kind : draft.kind" />
      <section v-if="mode === 'merge' && Object.keys(archivedTargetAttributes).length" data-preserved-archived-attributes>
        <h3>{{ t('managed.mergeArchivedAttributes') }}</h3><p>{{ t('managed.mergeArchivedAttributesHint') }}</p>
        <dl><template v-for="(value, key) in archivedTargetAttributes" :key="key"><dt>{{ attributeName(key) }}</dt><dd>{{ attributeValue(value) }}</dd></template></dl>
      </section>
      <section v-if="mode === 'merge' && excludedSourceAttributes.length" data-excluded-source-attributes>
        <h3>{{ t('managed.mergeExcludedAttributes') }}</h3><p>{{ t('managed.mergeExcludedAttributesHint') }}</p>
        <dl><template v-for="[key, value] in excludedSourceAttributes" :key="key"><dt>{{ attributeName(key) }}</dt><dd>{{ attributeValue(value) }}</dd></template></dl>
      </section>
      <label>{{ t('managed.effectiveAt') }}<input v-model="effectiveAt" class="rq-control" type="datetime-local"></label>
      <p v-if="loading" role="status">{{ t('common.loading') }}</p><p v-if="error" role="alert">{{ error }}</p><Button type="submit" variant="primary" :loading="busy" :disabled="loading">{{ t(mode === 'merge' ? 'managed.merge' : 'managed.split') }}</Button>
    </fieldset></form>
  </Dialog>
</template>
<style scoped>.reorganization-actions { display:flex; flex-wrap:wrap; gap:var(--space-2); } fieldset { display:grid; gap:var(--space-3); border:0; padding:0; min-width:0; } label { display:grid; gap:var(--space-1); } label.choice { display:flex; gap:var(--space-2); align-items:center; } dt,dd { overflow-wrap:anywhere; white-space:pre-wrap; } dd { margin-inline-start:var(--space-3); } [role=alert] { color:var(--danger); }</style>
