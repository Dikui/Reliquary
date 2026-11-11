<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Input, Tabs, EmptyState, ErrorState } from '@/components/ui'
import type { Entity } from '@/api/entities'
import type { LibraryCatalogItem } from '@/api/library'
import { SHELF_LEVEL_KINDS, shelfLevel, shelfRelationLabel, type ShelfSection } from '@/utils/productShelf'
import { useProductShelf, type ShelfRecord } from './useProductShelf'
import ManagedEntityEditor from './ManagedEntityEditor.vue'
import ManagedRelationEditor from './ManagedRelationEditor.vue'
import ManagedAttributes from './ManagedAttributes.vue'

const props = defineProps<{ library: LibraryCatalogItem; initialEntityId?: string }>()
const emit = defineEmits<{ 'open-detail': [id: string, library: string]; 'open-source': [id: string] }>()
const { t } = useI18n()
const shelf = useProductShelf(toRef(props, 'library'))
const { nodes, selected, records, entries, links, relations, loading, detailLoading, error, detailError, pending, linking } = shelf
const query = ref(''), tab = ref<ShelfSection>('attrs'), path = ref<string[]>([])
const composition = ref(false)
const matches = computed(() => query.value.trim() ? nodes.value.filter(node => `${node.canonical_name} ${node.code || ''}`.toLowerCase().includes(query.value.trim().toLowerCase())) : [])
const tabs = computed(() => (['attrs', 'market', 'certs', 'docs'] as const).map(value => ({ value, label: t(`productShelf.${value}`) })))
const currentLevel = computed(() => shelfLevel(selected.value?.kind || ''))
const nextKind = computed(() => currentLevel.value !== null && currentLevel.value < 3 ? SHELF_LEVEL_KINDS[currentLevel.value + 1] : undefined)
const parentRelations = computed(() => (links.value[selected.value?.id || ''] || []).filter(r => r.direction === 'outgoing' && r.relation_type === 'belongs_to'))
const currentRecords = computed(() => records.value.filter(record => record.section === tab.value))
const presentFields = (entity: Entity) => (props.library.entity_schema?.fields || []).filter(field => Object.hasOwn(entity.attributes || {}, field.key))
const relationFields = (record: ShelfRecord) => (props.library.relation_types.find(r => r.name === record.relation.relation_type)?.fields || []).filter(field => Object.hasOwn(record.relation.attributes || {}, field.key))
const kindName = (kind: string) => props.library.entity_schema?.kinds.find(k => k.key === kind)?.name || kind
const recordKinds = computed(() => tab.value === 'certs' ? ['certification'] : tab.value === 'docs' ? ['document_file'] : [])
function isUnassigned(node: Entity) { return !relations.value.some(edge => edge.child === node.id && nodes.value.some(parent => parent.id === edge.parent && shelfLevel(parent.kind || '') === shelfLevel(node.kind || '')! - 1)) }
const columns = computed(() => SHELF_LEVEL_KINDS.map((kind, level) => {
  const candidates = nodes.value.filter(node => node.kind === kind)
  if (!level) return candidates.map(node => ({ id: node.id, name: node.canonical_name, node }))
  const parent = path.value[level - 1]
  const children = candidates.filter(node => parent?.startsWith('missing:') ? isUnassigned(node) : relations.value.some(edge => edge.child === node.id && edge.parent === parent))
  const items: Array<{ id: string; name: string; node?: Entity }> = children.map(node => ({ id: node.id, name: node.canonical_name, node }))
  const unassigned = candidates.filter(isUnassigned)
  if (unassigned.length && !parent?.startsWith('missing:')) items.push({ id: `missing:${level}`, name: `${t('productShelf.unassigned')} (${unassigned.length})` })
  return items
}))
function setPath(node: Entity) {
  const next: string[] = [], seen = new Set<string>()
  let current: Entity | undefined = node
  while (current && !seen.has(current.id)) {
    seen.add(current.id)
    const level = shelfLevel(current.kind || '')
    if (level === null) break
    next[level] = current.id
    const parents = relations.value.filter(edge => edge.child === current!.id)
    current = nodes.value.find(item => item.id === parents[0]?.parent)
  }
  for (let i = 0; i < (shelfLevel(node.kind || '') || 0); i++) if (!next[i]) next[i] = `missing:${i}`
  path.value = next
  for (let i = next.length; i < 4; i++) { const first = columns.value[i]?.[0]; if (first) path.value[i] = first.id }
}
async function choose(id: string) {
  if (id.startsWith('missing:')) { const level = Number(id.split(':')[1]); path.value = [...path.value.slice(0, Math.max(0, level - 1)), `missing:${level - 1}`]; return }
  const node = nodes.value.find(item => item.id === id)
  if (!node) return
  setPath(node)
  query.value = ''
  await shelf.select(node)
}
function openDetail(id: string) { if (!id.startsWith('missing:')) emit('open-detail', id, props.library.id) }
async function load() {
  await shelf.load()
  const node = nodes.value.find(item => item.id === props.initialEntityId) || selected.value
  if (node) { setPath(node); await shelf.select(node) }
}
async function changed(entity?: Entity) { await shelf.refresh(entity || selected.value); if (selected.value && shelfLevel(selected.value.kind || '') !== null) setPath(selected.value) }
async function addChild(entity: Entity, parent: Entity | null = selected.value || null) {
  await shelf.attach(entity, parent ? { subject_id: entity.id, object_id: parent.id, relation_type: 'belongs_to', attributes: {} } : undefined)
  if (selected.value) setPath(selected.value)
}
async function addRecord(entity: Entity, owner = selected.value) {
  if (!owner) return
  const relation_type = entity.kind === 'certification' ? 'has_certification' : 'has_document'
  await shelf.attach(entity, { subject_id: owner.id, object_id: entity.id, relation_type, attributes: {} })
}
watch(() => props.initialEntityId, id => { if (id && id !== selected.value?.id && nodes.value.some(node => node.id === id)) void choose(id) })
watch(() => props.library.id, () => { path.value = []; void load() }, { immediate: true })
onBeforeUnmount(() => shelf.dispose())
</script>

<template>
  <section class="product-shelf" :aria-label="t('productShelf.title')">
    <div class="shelf-toolbar">
      <h2>{{ t('productShelf.title') }}</h2>
      <div class="shelf-search"><Input v-model="query" type="search" :aria-label="t('productShelf.search')" :placeholder="t('productShelf.search')" @compositionstart="composition = true" @compositionend="composition = false" /><Button v-if="query" variant="ghost" :aria-label="t('common.clear')" @click="query = ''">×</Button></div>
      <ManagedEntityEditor :library="library" initial-kind="product_line" :allowed-kinds="['product_line']" :trigger-label="t('productShelf.addRoot')" @saved="entity => addChild(entity, null)" />
    </div>
    <div v-if="query && !composition" class="shelf-results"><Button v-for="node in matches" :key="node.id" @click="choose(node.id)">{{ node.canonical_name }} · {{ kindName(node.kind || '') }}</Button><p v-if="!matches.length">{{ t('workbench.entities.noResults') }}</p></div>
    <ErrorState v-if="error" :title="error"><Button @click="load">{{ t('common.retry') }}</Button></ErrorState>
    <p v-if="loading" role="status">{{ t('productShelf.loading') }}</p>
    <div class="shelf-layout">
      <section class="shelf-main">
        <div class="shelf-levels"><strong v-for="kind in SHELF_LEVEL_KINDS" :key="kind">{{ kindName(kind) }}</strong></div>
        <div class="shelf-accessible"><div v-for="(column, index) in columns" :key="index"><Button v-for="node in column" :key="node.id" :aria-pressed="path.includes(node.id)" @click="choose(node.id)" @dblclick="openDetail(node.id)">{{ node.name }}</Button></div></div>
        <EmptyState v-if="!loading && !nodes.length" :title="t('productShelf.noRecords')" />
      </section>
      <aside class="shelf-detail">
        <template v-if="selected">
          <small>{{ kindName(selected.kind || '') }} · {{ selected.code }}</small><h2>{{ selected.canonical_name }}</h2>
          <div class="shelf-actions"><Button @click="openDetail(selected.id)">{{ t('productShelf.viewDetail') }}</Button><ManagedEntityEditor :key="selected.id" :library="library" :entity="selected" compact :trigger-label="t('productShelf.details')" @saved="changed" @deleted="load" /><ManagedEntityEditor v-if="nextKind" :key="`${selected.id}-child`" :library="library" :initial-kind="nextKind" :allowed-kinds="[nextKind]" :trigger-label="t('productShelf.addChild')" @saved="addChild" /></div>
          <p v-if="selected.description">{{ selected.description }}</p>
          <details v-if="parentRelations.length"><summary>{{ t('productShelf.adjustParent') }}</summary><div v-for="relation in parentRelations" :key="relation.id" class="shelf-parent"><span>{{ relation.peer.canonical_name }}</span><ManagedRelationEditor :library="library" :entity="selected" :relation="relation" @saved="changed()" /></div></details>
          <Tabs v-model="tab" :items="tabs" :label="t('productShelf.details')">
            <template v-for="item in tabs" :key="item.value" #[item.value]>
              <p v-if="tab === 'certs'" class="shelf-note">{{ t('productShelf.recordHint') }}</p>
              <ManagedAttributes v-if="tab === 'attrs'" :model-value="selected.attributes || {}" :fields="presentFields(selected)" :library-id="library.id" :kind="selected.kind" readonly />
              <div class="shelf-actions"><ManagedEntityEditor v-if="currentLevel !== 0 && recordKinds.length" :key="`${selected.id}-${tab}-new`" :library="library" :initial-kind="recordKinds[0]" :allowed-kinds="recordKinds" :trigger-label="t('productShelf.addRecord')" @saved="addRecord" /><ManagedRelationEditor :library="library" :entity="selected" @saved="changed()" /></div>
              <p v-if="detailLoading" role="status">{{ t('common.loading') }}</p>
              <ErrorState v-if="detailError" :title="detailError"><Button @click="shelf.select(selected!)">{{ t('common.retry') }}</Button></ErrorState>
              <div v-if="pending" class="shelf-note" role="alert"><p>{{ t('productShelf.linkFailed') }}</p><strong>{{ pending.entity.canonical_name }}</strong><Button :loading="linking" @click="shelf.retryLink">{{ t('productShelf.retryLink') }}</Button></div>
              <div v-if="tab === 'docs'" class="shelf-entries"><Button v-for="entry in entries" :key="entry.id" @click="emit('open-source', entry.id)">{{ entry.title || entry.file_name || entry.id }}</Button></div>
              <p v-if="!detailLoading && !detailError && !currentRecords.length && !(tab === 'docs' && entries.length)">{{ t('productShelf.noRecords') }}</p>
              <article v-for="record in currentRecords" :key="record.relation.id" class="shelf-record">
                <div class="shelf-record-head"><h3>{{ record.entity.canonical_name }}</h3><span v-if="record.ancestor" class="shelf-badge">{{ t('productShelf.scope') }}</span></div>
                <p>{{ t('productShelf.appliesTo') }}：{{ record.owner.canonical_name }}</p>
                <small>{{ kindName(record.entity.kind || '') }} · {{ t(shelfRelationLabel(record.relation.relation_type) || 'productShelf.associated') }}</small>
                <p v-if="record.ancestor" class="shelf-note">{{ t('productShelf.scopeHint') }}</p>
                <p v-if="record.entity.description">{{ record.entity.description }}</p>
                <ManagedAttributes :model-value="record.entity.attributes || {}" :fields="presentFields(record.entity)" :library-id="library.id" :kind="record.entity.kind" readonly />
                <ManagedAttributes v-if="relationFields(record).length" :model-value="record.relation.attributes || {}" :fields="relationFields(record)" :library-id="library.id" readonly />
                <div class="shelf-actions"><ManagedEntityEditor :library="library" :entity="record.entity" compact :trigger-label="t('productShelf.details')" @saved="() => changed()" @deleted="() => changed()" /><ManagedRelationEditor :library="library" :entity="record.owner" :relation="record.relation" @saved="changed()" /></div>
              </article>
            </template>
          </Tabs>
        </template>
        <EmptyState v-else :title="t('productShelf.choose')" />
      </aside>
    </div>
  </section>
</template>

<style scoped>
.product-shelf { min-width: 0; color: var(--text); }
.shelf-toolbar, .shelf-actions, .shelf-record-head, .shelf-parent { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
.shelf-toolbar { padding-block: var(--space-3); border-bottom: 2px solid var(--rule); }
h2,h3,p { margin: 0; } h2 { font-size: var(--text-lg); overflow-wrap: anywhere; } h3 { font-size: var(--text-md); }
.shelf-search { display: flex; flex: 1; min-width: 200px; } .shelf-search :deep(input) { width: 100%; }
.shelf-results { display: flex; flex-wrap: wrap; max-height: 240px; overflow: auto; gap: var(--space-2); padding-block: var(--space-3); }
.shelf-layout { display: grid; grid-template-columns: minmax(0, 1fr) 380px; min-height: 620px; }
.shelf-main { min-width: 0; padding-right: var(--space-3); }
.shelf-levels { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--space-2); padding-block: var(--space-3); font-size: var(--text-xs); }
.shelf-detail { display: grid; align-content: start; gap: var(--space-3); border-left: 1px solid var(--border); padding: var(--space-4); min-width: 0; }
.shelf-detail :deep(.rq-tabs__list) { display: flex; flex-wrap: wrap; } .shelf-detail :deep(.rq-tabs__trigger) { flex: 1; padding-inline: var(--space-2); }
.shelf-record { display: grid; gap: var(--space-2); padding-block: var(--space-4); border-bottom: 1px solid var(--border); overflow-wrap: anywhere; }
.shelf-note { font-size: var(--text-xs); padding: var(--space-3); background: var(--surface-soft); border-left: 3px solid var(--warning); }
small { color: var(--muted); overflow-wrap: anywhere; }.shelf-badge { background: var(--surface-soft); padding: var(--space-1); font-size: var(--text-xs); }
.shelf-accessible { display: grid; grid-template-columns: repeat(4, minmax(0,1fr)); gap: var(--space-2); max-height: 480px; overflow: auto; }
.shelf-accessible > div { display: flex; flex-direction: column; gap: var(--space-1); }.shelf-accessible :deep(button) { display: block; width: 100%; min-width: 0; white-space: normal; text-align: left; overflow-wrap: anywhere; }.shelf-accessible :deep([aria-pressed=true]) { background: var(--select); color: var(--on-select); }
.shelf-accessible :deep(button span) { white-space: normal; overflow-wrap: anywhere; }
summary { cursor: pointer; } summary:focus-visible { outline: 2px solid var(--text); outline-offset: 3px; }
@media(max-width:1100px) { .shelf-layout { grid-template-columns:minmax(0,1fr); } .shelf-main { padding-right:0; }.shelf-detail { border-left:0; border-top:1px solid var(--border); padding-inline:0; } }
</style>
