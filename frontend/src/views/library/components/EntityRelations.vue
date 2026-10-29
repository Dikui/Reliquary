<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { listEntityRelations, deleteEntityRelation, type RelationGroup, type EntityRelation, type RelationSource } from '@/api/relations'
import type { SourceLocation } from '@/types/search'
import LibraryScopeSelect from '@/components/LibraryScopeSelect.vue'
import { useLibraryScope } from '@/composables/useLibraryScope'
const props = defineProps<{ entityId: string }>()
const emit = defineEmits<{ 'open-source': [id: string, location?: SourceLocation]; 'open-entity': [id: string] }>()
const { t } = useI18n(), scope = useLibraryScope()
const groups = ref<RelationGroup[]>([]), loading = ref(false), error = ref(''), deleting = ref<EntityRelation | null>(null), busy = ref(false), deleteError = ref('')
let sequence = 0
const filtered = computed(() => groups.value.map(group => ({ ...group, relations: group.relations.filter(relation => !scope.value.length || scope.value.includes(relation.library_id)) })).filter(group => group.relations.length))
function locationFor(source: RelationSource, location?: SourceLocation) { return location ? { ...location, body_hash: location.body_hash || source.body_hash } : undefined }
async function load() {
  const current = ++sequence; groups.value = []; loading.value = true; error.value = ''
  try { const response = await listEntityRelations(props.entityId); if (!response.success) throw new Error(response.message); if (current === sequence) groups.value = response.data || [] }
  catch (cause: any) { if (current === sequence) error.value = cause.message || t('common.operationFailed') }
  finally { if (current === sequence) loading.value = false }
}
async function remove() {
  if (!deleting.value || busy.value) return
  busy.value = true; deleteError.value = ''
  const id = deleting.value.id, entity = props.entityId
  try { const response = await deleteEntityRelation(id); if (!response.success) throw new Error(response.message); deleting.value = null; if (entity === props.entityId) await load() }
  catch (cause: any) { deleteError.value = cause.message || t('common.operationFailed') }
  finally { busy.value = false }
}
watch(() => props.entityId, () => { deleting.value = null; void load() }, { immediate: true })
onBeforeUnmount(() => { sequence++ })
</script>
<template>
  <section class="entity-relations"><h4>{{ t('ikb18.relations') }}</h4><LibraryScopeSelect v-model="scope" :disabled="busy" />
    <t-alert v-if="error" theme="error" :message="error"><template #operation><t-button variant="text" @click="load">{{ t('common.retry') }}</t-button></template></t-alert>
    <t-loading :loading="loading"><p v-if="!loading && !error && !filtered.length">{{ t('ikb18.noRelations') }}</p>
      <section v-for="group in filtered" :key="group.relation_type" class="relation-group"><h5>{{ group.relation_type }}</h5>
        <article v-for="relation in group.relations" :key="relation.id" class="relation-row">
          <div class="relation-heading"><span>{{ t(`ikb18.${relation.direction}`) }}</span><t-button variant="text" @click="emit('open-entity', relation.peer.id)">{{ relation.peer.canonical_name }}</t-button><span>{{ relation.library_name }}</span><span>{{ t('ikb18.sources', { count: relation.source_count }) }}</span><t-button variant="text" theme="danger" :disabled="busy" @click="deleting = relation; deleteError = ''">{{ t('ikb18.deleteRelation') }}</t-button></div>
          <details><summary>{{ t('ikb18.relationSources') }}</summary><div v-for="source in relation.sources" :key="source.entry_id" class="relation-source">
            <t-button variant="text" @click="emit('open-source', source.entry_id, locationFor(source, source.evidence?.[0]?.locations?.[0]))">{{ source.title || source.entry_id }}</t-button>
            <div v-for="(evidence, index) in source.evidence" :key="index"><p v-if="evidence.speaker">{{ t('ikb18.speaker') }}: <t-button v-if="evidence.speaker.entity_id" variant="text" @click="emit('open-entity', evidence.speaker.entity_id)">{{ evidence.speaker.name }}</t-button><span v-else>{{ evidence.speaker.name }}</span></p><t-button v-for="(location, number) in evidence.locations" :key="number" variant="text" @click="emit('open-source', source.entry_id, locationFor(source, location))">{{ location.heading_path?.join(' / ') }} · {{ t('ikb18.paragraph', { number: location.paragraph }) }} [{{ location.start }}, {{ location.end }})</t-button></div>
          </div></details>
        </article>
      </section>
    </t-loading>
    <t-dialog :visible="!!deleting" :header="t('ikb18.deleteRelation')" :footer="false" :close-btn="!busy" :close-on-esc-keydown="!busy" :close-on-overlay-click="false" @close="!busy && (deleting = null)"><p>{{ deleting?.relation_type }} · {{ deleting?.peer.canonical_name }}</p><p>{{ t('ikb18.suppressHint') }}</p><p v-if="deleteError" role="alert">{{ deleteError }}</p><div class="relation-heading"><t-button theme="danger" :loading="busy" @click="remove">{{ t('common.confirm') }}</t-button><t-button variant="outline" :disabled="busy" @click="deleting = null">{{ t('common.cancel') }}</t-button></div></t-dialog>
  </section>
</template>
<style scoped>
.entity-relations { margin-block: 24px; overflow-wrap: anywhere; }
.relation-row { border-bottom: 1px solid var(--td-component-stroke); padding-block: 12px; }
.relation-heading { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; font-size: 13px; }
.relation-source { padding: 12px; background: var(--td-bg-color-secondarycontainer); margin-block: 8px; }
summary { cursor: pointer; color: var(--td-text-color-secondary); margin-block: 12px; }
:deep(.t-button) { height: auto; white-space: normal; max-width: 100%; }
:deep(.t-button__text) { white-space: normal; overflow-wrap: anywhere; }
</style>
