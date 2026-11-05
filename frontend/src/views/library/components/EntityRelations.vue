<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { deleteEntityRelation, type RelationGroup, type EntityRelation, type RelationSource } from '@/api/relations'
import ManagedAttributes from './ManagedAttributes.vue'
import ManagedRelationEditor from './ManagedRelationEditor.vue'
import type { LibraryCatalogItem } from '@/api/library'
import type { Entity } from '@/api/entities'
import type { SourceLocation } from '@/types/search'
import { Button, EmptyState, ErrorState } from '@/components/ui'
import { confirm, toast, type ConfirmHandle } from '@/components/ui/feedback'
const props = defineProps<{ entityId: string; entityName: string; groups: RelationGroup[]; managedLibrary?: LibraryCatalogItem; entity?: Entity; loading?: boolean; error?: string }>()
const emit = defineEmits<{ 'open-source': [id: string, location?: SourceLocation]; 'open-entity': [id: string]; changed: []; retry: [] }>()
const { t } = useI18n()
const busy = ref(false)
const relations = computed(() => props.groups.flatMap(group => group.relations))
let confirmation: ConfirmHandle | undefined
function locationFor(source: RelationSource, location?: SourceLocation) { return location ? { ...location, body_hash: location.body_hash || source.body_hash } : undefined }
function remove(relation: EntityRelation) {
  if (busy.value) return
  const entity = props.entityId
  confirmation?.destroy()
  confirmation = confirm({
    header: t('ikb18.deleteRelation'), body: `${relation.relation_type} · ${relation.peer.canonical_name}\n${props.managedLibrary ? '' : t('ikb18.suppressHint')}`,
    confirmBtn: t('common.confirm'), cancelBtn: t('common.cancel'), theme: 'danger',
    onConfirm: async () => {
      busy.value = true
      try {
        const response = await deleteEntityRelation(relation.id, props.managedLibrary ? relation.version : undefined)
        if (!response.success) throw new Error(response.message || t('common.operationFailed'))
        toast.success(t('entityPage.relationDeleted'))
        if (entity === props.entityId) emit('changed')
      } finally { busy.value = false }
    },
  })
}
onBeforeUnmount(() => confirmation?.destroy())
</script>
<template>
  <section class="entity-relations" :aria-label="t('ikb18.relations')" :aria-busy="loading">
    <ErrorState v-if="error" :title="error"><Button @click="emit('retry')">{{ t('common.retry') }}</Button></ErrorState>
    <p v-else-if="loading" role="status">{{ t('common.loading') }}</p>
    <EmptyState v-else-if="!relations.length" :title="t('ikb18.noRelations')" />
    <div v-else class="relation-scroll">
      <table><thead><tr><th>{{ t('entityPage.subject') }}</th><th>{{ t('ikb18.relations') }}</th><th>{{ t('entityPage.object') }}</th><th>{{ t('ikb18.relationSources') }}</th><th>{{ t('ikb18.speaker') }}</th><th>{{ t('entityPage.actions') }}</th></tr></thead>
        <tbody><tr v-for="relation in relations" :key="relation.id">
          <td><span v-if="relation.direction === 'outgoing'">{{ entityName }}</span><button v-else type="button" class="entity-link" @click="emit('open-entity', relation.peer.id)">{{ relation.peer.canonical_name }}</button></td>
          <td><strong>{{ relation.relation_type }}</strong><ManagedAttributes v-if="managedLibrary && relation.attributes" :model-value="relation.attributes" :fields="managedLibrary.relation_types.find(r => r.name === relation.relation_type)?.fields || []" :library-id="managedLibrary.id" readonly /></td>
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
          <td><ManagedRelationEditor v-if="managedLibrary && entity" :library="managedLibrary" :entity="entity" :relation="relation" @saved="emit('changed')" /><Button variant="danger" :disabled="busy" @click="remove(relation)">{{ t('ikb18.deleteRelation') }}</Button></td>
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
</style>
