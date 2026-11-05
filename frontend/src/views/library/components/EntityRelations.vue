<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { deleteEntityRelation, type RelationGroup, type EntityRelation, type RelationSource } from '@/api/relations'
import { formatDateTime, localDateTimeISO } from '@/utils/dateTime'
import ManagedAttributes from './ManagedAttributes.vue'
import ManagedRelationEditor from './ManagedRelationEditor.vue'
import type { LibraryCatalogItem } from '@/api/library'
import type { Entity } from '@/api/entities'
import type { SourceLocation } from '@/types/search'
import { Button, Dialog, EmptyState, ErrorState } from '@/components/ui'
import { confirm, toast, type ConfirmHandle } from '@/components/ui/feedback'
const props = defineProps<{ entityId: string; entityName: string; groups: RelationGroup[]; managedLibrary?: LibraryCatalogItem; entity?: Entity; loading?: boolean; error?: string; asOf?: string }>()
const emit = defineEmits<{ 'open-source': [id: string, location?: SourceLocation]; 'open-entity': [id: string]; changed: []; retry: []; 'query-time': [value: string] }>()
const { t, locale } = useI18n()
const at = ref('')
watch(() => props.entityId, () => { at.value = '' })
watch(() => props.asOf, value => { if (!value) at.value = '' })
const busy = ref(false)
const ending = ref<EntityRelation>(), endAt = ref(''), endError = ref('')
watch(() => props.entityId, () => { ending.value = undefined })
const relations = computed(() => props.groups.flatMap(group => group.relations))
let confirmation: ConfirmHandle | undefined
let endOpener: HTMLElement | null = null
function focusCancel(event: Event) {
  const button = (event.target as HTMLElement)?.querySelector<HTMLElement>('[data-cancel]')
  if (button) { event.preventDefault(); button.focus() }
}
function restoreEndFocus(event: Event) {
  event.preventDefault()
  const opener = endOpener
  void nextTick(() => { if (opener?.isConnected) opener.focus() })
}
function locationFor(source: RelationSource, location?: SourceLocation) { return location ? { ...location, body_hash: location.body_hash || source.body_hash } : undefined }
function remove(relation: EntityRelation) {
  if (busy.value) return
  if (props.managedLibrary) {
    endOpener = document.activeElement as HTMLElement | null
    ending.value = relation; endAt.value = ''; endError.value = ''
    return
  }
  const entity = props.entityId
  confirmation?.destroy()
  confirmation = confirm({
    header: t('ikb18.deleteRelation'), body: `${relation.relation_type} · ${relation.peer.canonical_name}\n${t('ikb18.suppressHint')}`,
    confirmBtn: t('common.confirm'), cancelBtn: t('common.cancel'), theme: 'danger',
    onConfirm: async () => {
      busy.value = true
      try {
        const response = await deleteEntityRelation(relation.id)
        if (!response.success) throw new Error(response.message || t('common.operationFailed'))
        toast.success(t('entityPage.relationDeleted'))
        if (entity === props.entityId) emit('changed')
      } finally { busy.value = false }
    },
  })
}
async function endRelation() {
  if (busy.value || !ending.value) return
  const entity = props.entityId, relation = ending.value
  busy.value = true; endError.value = ''
  try {
    const response = await deleteEntityRelation(relation.id, relation.version, endAt.value ? localDateTimeISO(endAt.value) : undefined)
    if (!response.success) throw new Error(response.message || t('common.operationFailed'))
    toast.success(t('managed.relationEnded'))
    ending.value = undefined
    if (entity === props.entityId) emit('changed')
  } catch (cause: any) { endError.value = cause.message || t('common.operationFailed') }
  finally { busy.value = false }
}
onBeforeUnmount(() => confirmation?.destroy())
</script>
<template>
  <section class="entity-relations" :aria-label="t('ikb18.relations')" :aria-busy="loading">
    <Dialog v-if="managedLibrary" :model-value="!!ending" :title="t('managed.endRelation')" :closable="!busy" :close-on-esc-keydown="false" :close-on-overlay-click="false" @update:model-value="!$event && (ending = undefined)" @open-auto-focus="focusCancel" @close-auto-focus="restoreEndFocus">
      <form class="relation-end" novalidate @submit.prevent="endRelation"><fieldset :disabled="busy">
        <p>{{ ending?.relation_type }} · {{ ending?.peer.canonical_name }}</p>
        <p>{{ t('managed.endRelationHint') }}</p>
        <label>{{ t('managed.relationEndAt') }}<input v-model="endAt" class="rq-control" type="datetime-local"></label>
        <p>{{ t('managed.relationTimeHint') }}</p>
        <p v-if="endError" role="alert">{{ endError }}</p>
        <Button data-cancel type="button" :disabled="busy" @click="ending = undefined">{{ t('common.cancel') }}</Button>
        <Button type="submit" variant="danger" :loading="busy">{{ t('common.confirm') }}</Button>
      </fieldset></form>
    </Dialog>
    <form v-if="managedLibrary" class="relation-time" novalidate @submit.prevent="emit('query-time', at ? localDateTimeISO(at) : '')"><label>{{ t('managed.asOf') }}<input v-model="at" class="rq-control" type="datetime-local"></label><Button type="submit" :loading="loading">{{ t('managed.queryRelations') }}</Button></form>
    <ErrorState v-if="error" :title="error"><Button @click="emit('retry')">{{ t('common.retry') }}</Button></ErrorState>
    <p v-else-if="loading" role="status">{{ t('common.loading') }}</p>
    <EmptyState v-else-if="!relations.length" :title="t('ikb18.noRelations')" />
    <div v-else class="relation-scroll">
      <table><thead><tr><th>{{ t('entityPage.subject') }}</th><th>{{ t('ikb18.relations') }}</th><th>{{ t('entityPage.object') }}</th><th>{{ t('ikb18.relationSources') }}</th><th>{{ t('ikb18.speaker') }}</th><th>{{ t('entityPage.actions') }}</th></tr></thead>
        <tbody><tr v-for="relation in relations" :key="relation.id">
          <td><span v-if="relation.direction === 'outgoing'">{{ entityName }}</span><button v-else type="button" class="entity-link" @click="emit('open-entity', relation.peer.id)">{{ relation.peer.canonical_name }}</button></td>
          <td><strong>{{ relation.relation_type }}</strong><p v-if="managedLibrary && relation.valid_from">{{ formatDateTime(relation.valid_from, locale, { dateStyle: 'medium', timeStyle: 'short' }) }} → {{ relation.valid_to ? formatDateTime(relation.valid_to, locale, { dateStyle: 'medium', timeStyle: 'short' }) : t('managed.ongoing') }}</p><ManagedAttributes v-if="managedLibrary && relation.attributes" :model-value="relation.attributes" :fields="managedLibrary.relation_types.find(r => r.name === relation.relation_type)?.fields || []" :library-id="managedLibrary.id" readonly /></td>
          <td><span v-if="relation.direction === 'incoming'">{{ entityName }}</span><button v-else type="button" class="entity-link" @click="emit('open-entity', relation.peer.id)">{{ relation.peer.canonical_name }}</button></td>
          <td><span>{{ t('ikb18.sources', { count: relation.source_count }) }}</span>
            <details><summary>{{ t('ikb18.relationSources') }}</summary>
              <div v-for="source in relation.sources" :key="source.entry_id" class="relation-source">
                <button type="button" class="entity-link" @click="emit('open-source', source.entry_id, locationFor(source, source.evidence?.[0]?.locations?.[0]))">{{ source.title || source.entry_id }}</button>
                <div v-for="(evidence, index) in source.evidence || []" :key="index">
                  <p v-if="evidence.speaker">{{ t('ikb18.speaker') }}: <button v-if="evidence.speaker.entity_id" type="button" class="entity-link" @click="emit('open-entity', evidence.speaker.entity_id)">{{ evidence.speaker.name }}</button><span v-else>{{ evidence.speaker.name }}</span></p>
                  <button v-for="(location, number) in evidence.locations || []" :key="number" type="button" class="entity-link" @click="emit('open-source', source.entry_id, locationFor(source, location))">{{ location.heading_path?.join(' / ') }} · {{ t('ikb18.paragraph', { number: location.paragraph }) }}</button>
                </div>
              </div>
            </details>
          </td>
          <td>{{ [...new Set(relation.sources.flatMap(source => (source.evidence || []).flatMap(evidence => evidence.speaker?.name ? [evidence.speaker.name] : [])))].join(' · ') }}</td>
          <td><template v-if="!asOf"><ManagedRelationEditor v-if="managedLibrary && entity" :library="managedLibrary" :entity="entity" :relation="relation" @saved="emit('changed')" /><Button variant="danger" :disabled="busy" @click="remove(relation)">{{ t(managedLibrary ? 'managed.endRelation' : 'ikb18.deleteRelation') }}</Button></template></td>
        </tr></tbody>
      </table>
    </div>
    <p v-if="relations.length && !managedLibrary" class="relation-hint">{{ t('ikb18.suppressHint') }}</p>
  </section>
</template>
<style scoped>
.entity-relations { color: var(--text); overflow-wrap: anywhere; }
.relation-scroll { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; font-size: var(--text-sm); }
th, td { padding: var(--space-3); text-align: left; vertical-align: top; border-bottom: 1px solid var(--border); min-width: 100px; }
th { border-bottom: 2px solid var(--rule); font-size: var(--text-xs); }
tbody tr:hover, tbody tr:focus-within { background: var(--select); color: var(--on-select); }
.relation-hint { color: var(--muted); font-size: var(--text-xs); }
.relation-source { padding-block: var(--space-2); }
.entity-link { padding: 0; border: 0; color: inherit; background: none; font: inherit; text-align: left; text-decoration: underline; cursor: pointer; overflow-wrap: anywhere; }
.entity-link:hover { color: var(--accent); }
tbody tr:hover .entity-link, tbody tr:focus-within .entity-link { color: var(--on-select); }
tbody tr:hover :deep(.rq-button), tbody tr:focus-within :deep(.rq-button) { color: var(--on-select); }
summary { cursor: pointer; margin-block: var(--space-2); }
.entity-link:focus-visible, summary:focus-visible { outline: 2px solid currentColor; outline-offset: 3px; }
.relation-time { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: end; margin-block: var(--space-3); } .relation-time label { display: grid; gap: var(--space-1); }
.relation-end fieldset { display: grid; gap: var(--space-3); border: 0; padding: 0; } .relation-end label { display: grid; gap: var(--space-1); } .relation-end [role='alert'] { color: var(--danger); }
</style>
