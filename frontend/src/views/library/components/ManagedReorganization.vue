<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Dialog, Combobox, Input, Select, Textarea, confirm, toast } from '@/components/ui'
import { getEntity, listEntities, mergeManagedEntity, splitManagedEntity, type Entity, type EntityEntry, type ManagedEntityInput } from '@/api/entities'
import { listEntityRelations, type EntityRelation } from '@/api/relations'
import type { LibraryCatalogItem } from '@/api/library'
import ManagedAttributes from './ManagedAttributes.vue'
const props = defineProps<{ entity: Entity; library: LibraryCatalogItem }>()
const emit = defineEmits<{ saved: [entity: Entity] }>()
const { t } = useI18n()
const open = ref(false), mode = ref<'merge' | 'split'>('merge'), loading = ref(false), busy = ref(false), error = ref(''), more = ref(false)
const candidates = ref<Entity[]>([]), sourceId = ref(''), source = ref<Entity>(), entries = ref<EntityEntry[]>([]), relations = ref<EntityRelation[]>([]), entryIds = ref<string[]>([]), relationIds = ref<string[]>([]), effectiveAt = ref('')
const draft = ref<ManagedEntityInput>({ name: '', kind: '', code: '', description: '', aliases: [], attributes: {} }), aliases = ref('')
const options = computed(() => candidates.value.filter(e => e.id !== props.entity.id).map(e => ({ value: e.id, label: `${e.canonical_name} · ${e.code || e.id}` })))
const fields = computed(() => (props.library.entity_schema?.fields || []).filter(f => !f.archived && (!f.kinds?.length || f.kinds.includes(mode.value === 'merge' ? props.entity.kind || '' : draft.value.kind))))
let request = 0, sourceRequest = 0
async function loadCandidates(append = false) {
  const current = ++request; loading.value = true
  try { const result = await listEntities({ library_id: props.library.id, limit: 100, offset: append ? candidates.value.length : 0 }); if (!result.success) throw new Error(result.error); if (current === request) { candidates.value = append ? [...candidates.value, ...result.data] : result.data; more.value = result.data.length === 100 } }
  catch (cause: any) { if (current === request) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === request) loading.value = false }
}
async function begin(value: 'merge' | 'split') {
  mode.value = value; open.value = true; error.value = ''; sourceId.value = ''; source.value = undefined; entryIds.value = []; relationIds.value = []; effectiveAt.value = ''; aliases.value = ''
  draft.value = { name: '', code: '', kind: props.entity.kind || '', description: '', aliases: [], attributes: { ...props.entity.attributes } }
  if (value === 'merge') { await loadCandidates(); return }
  const current = ++request; loading.value = true
  try { const [detail, edges] = await Promise.all([getEntity(props.entity.id), listEntityRelations(props.entity.id)]); if (!detail.success || !edges.success) throw new Error(t('common.operationFailed')); if (current === request) { entries.value = detail.data.entries; relations.value = edges.data.flatMap(g => g.relations) } }
  catch (cause: any) { if (current === request) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === request) loading.value = false }
}
watch(sourceId, async id => {
  const current = ++sourceRequest; source.value = undefined
  if (!id) return
  loading.value = true
  try { const result = await getEntity(id); if (!result.success) throw new Error(result.error); if (current === sourceRequest) { source.value = result.data.entity; draft.value.attributes = { ...result.data.entity.attributes, ...props.entity.attributes } } }
  catch (cause: any) { if (current === sourceRequest) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === sourceRequest) loading.value = false }
})
async function apply() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try {
    const effective_at = effectiveAt.value ? new Date(effectiveAt.value).toISOString() : undefined
    const attributes = Object.fromEntries(Object.entries(draft.value.attributes).filter(([, v]) => v !== '' && v != null))
    const result = mode.value === 'merge'
      ? await mergeManagedEntity(props.entity.id, { revision: props.entity.revision!, source_id: source.value!.id, source_revision: source.value!.revision!, attributes, effective_at })
      : await splitManagedEntity(props.entity.id, { revision: props.entity.revision!, new_entity: { ...draft.value, attributes, aliases: aliases.value.split('\n').map(s => s.trim()).filter(Boolean) }, entry_ids: entryIds.value, relation_ids: relationIds.value, effective_at })
    if (!result.success) throw new Error(result.error || t('common.operationFailed'))
    open.value = false; emit('saved', result.data); toast.success(t('common.save'))
  } catch (cause: any) { error.value = cause.message || t('common.operationFailed'); throw cause }
  finally { busy.value = false }
}
function submit() {
  if (loading.value || busy.value) return
  if (mode.value === 'merge' ? !source.value : !draft.value.name.trim() || !draft.value.kind) { error.value = t('managed.required'); return }
  confirm({ header: t(mode.value === 'merge' ? 'managed.merge' : 'managed.split'), body: t('managed.reorganizeConfirm', { name: props.entity.canonical_name }), onConfirm: apply })
}
onBeforeUnmount(() => { request++; sourceRequest++ })
</script>
<template>
  <div class="reorganization-actions"><Button :disabled="!!entity.archived_at" @click="begin('merge')">{{ t('managed.merge') }}</Button><Button :disabled="!!entity.archived_at" @click="begin('split')">{{ t('managed.split') }}</Button></div>
  <Dialog v-model="open" :title="t(mode === 'merge' ? 'managed.merge' : 'managed.split')" :closable="!busy" :close-on-overlay-click="false" :close-on-esc-keydown="false">
    <form novalidate class="reorganization-form" @submit.prevent="submit"><fieldset :disabled="busy">
      <p>{{ t('managed.reorganizeHint') }}</p>
      <template v-if="mode === 'merge'"><p>{{ t('managed.mergeTarget') }}: {{ entity.canonical_name }}</p><label>{{ t('managed.mergeSource') }}<Combobox v-model="sourceId" :options="options" :disabled="loading" /></label><Button v-if="more" :loading="loading" @click="loadCandidates(true)">{{ t('workbench.loadMore') }}</Button></template>
      <template v-else><label>{{ t('ikb18.name') }}<Input v-model="draft.name" /></label><label>{{ t('managed.kind') }}<Select v-model="draft.kind"><option v-for="kind in library.entity_schema?.kinds" :key="kind.key" :value="kind.key">{{ kind.name }}</option></Select></label><label>{{ t('managed.code') }}<Input v-model="draft.code" /></label><label>{{ t('ikb18.description') }}<Textarea v-model="draft.description" :rows="3" /></label><label>{{ t('managed.aliases') }}<Textarea v-model="aliases" :rows="3" /></label>
        <h3>{{ t('managed.moveDocuments') }}</h3><p v-if="!entries.length">{{ t('workbench.entities.noArticles') }}</p><label v-for="entry in entries" :key="entry.id" class="choice"><input v-model="entryIds" type="checkbox" :value="entry.id">{{ entry.title || entry.file_name || entry.id }}</label>
        <h3>{{ t('managed.moveRelations') }}</h3><p v-if="!relations.length">{{ t('ikb18.noRelations') }}</p><label v-for="relation in relations" :key="relation.id" class="choice"><input v-model="relationIds" type="checkbox" :value="relation.id">{{ relation.direction === 'outgoing' ? '→' : '←' }} {{ relation.relation_type }} · {{ relation.peer.canonical_name }}</label>
      </template>
      <h3>{{ t('managed.resultAttributes') }}</h3><ManagedAttributes v-model="draft.attributes" :fields="fields" :library-id="library.id" :kind="mode === 'merge' ? entity.kind : draft.kind" />
      <label>{{ t('managed.effectiveAt') }}<input v-model="effectiveAt" class="rq-control" type="datetime-local"></label>
      <p v-if="loading" role="status">{{ t('common.loading') }}</p><p v-if="error" role="alert">{{ error }}</p><Button type="submit" variant="primary" :loading="busy" :disabled="loading">{{ t(mode === 'merge' ? 'managed.merge' : 'managed.split') }}</Button>
    </fieldset></form>
  </Dialog>
</template>
<style scoped>.reorganization-actions { display:flex; flex-wrap:wrap; gap:var(--space-2); } fieldset { display:grid; gap:var(--space-3); border:0; padding:0; min-width:0; } label { display:grid; gap:var(--space-1); } label.choice { display:flex; gap:var(--space-2); align-items:center; } [role=alert] { color:var(--danger); }</style>
