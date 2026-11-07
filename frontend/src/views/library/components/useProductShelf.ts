import { computed, ref, type Ref } from 'vue'
import { getEntity, listEntities, type Entity, type EntityEntry } from '@/api/entities'
import { createManagedRelation, listEntityRelations, type EntityRelation, type ManagedRelationInput } from '@/api/relations'
import type { LibraryCatalogItem } from '@/api/library'
import { SHELF_LEVEL_KINDS, shelfLevel, shelfSection, type ShelfSection } from '@/utils/productShelf'

export interface ShelfRecord { entity: Entity; owner: Entity; relation: EntityRelation; section: ShelfSection; ancestor: boolean }
export function useProductShelf(library: Ref<LibraryCatalogItem>) {
  const entries = ref<EntityEntry[]>([])
  const nodes = ref<Entity[]>([]), selected = ref<Entity>(), records = ref<ShelfRecord[]>([])
  const links = ref<Record<string, EntityRelation[]>>({})
  const loading = ref(false), detailLoading = ref(false), error = ref(''), detailError = ref(''), linking = ref(false)
  const pending = ref<{ entity: Entity; input: ManagedRelationInput }>()
  let generation = 0, detailGeneration = 0
  const relations = computed(() => {
    const unique = new Map<string, { child: string; parent: string; relation: EntityRelation }>()
    for (const [id, rows] of Object.entries(links.value)) for (const relation of rows) {
      if (relation.relation_type !== 'belongs_to') continue
      const child = relation.direction === 'outgoing' ? id : relation.peer.id
      const parent = relation.direction === 'outgoing' ? relation.peer.id : id
      unique.set(relation.id, { child, parent, relation })
    }
    return [...unique.values()]
  })
  async function fetchRelations(id: string, fresh = false) {
    if (!fresh && links.value[id]) return links.value[id]!
    const epoch = generation
    const result = await listEntityRelations(id)
    if (!result.success) throw new Error(result.message || 'Unable to load relationships')
    const rows = result.data.flatMap(group => group.relations).filter(row => !row.peer.archived_at)
    if (epoch === generation) links.value = { ...links.value, [id]: rows }
    return rows
  }
  async function load() {
    const epoch = ++generation; detailGeneration++; loading.value = true; error.value = ''; records.value = []; links.value = {}
    try {
      const groups = await Promise.all(SHELF_LEVEL_KINDS.map(async kind => {
        const items: Entity[] = []
        for (let offset = 0; ; offset += 100) {
          const result = await listEntities({ library_id: library.value.id, kind, limit: 100, offset })
          if (!result.success) throw new Error(result.error)
          items.push(...result.data)
          if (result.data.length < 100) return items
        }
      }))
      if (epoch !== generation) return
      nodes.value = groups.flat()
      // Parent endpoints supply all shelf edges, including links to models and missing-level branches.
      const parents = nodes.value.filter(node => shelfLevel(node.kind || '') !== 3)
      let index = 0
      await Promise.all(Array.from({ length: Math.min(4, parents.length) }, async () => {
        while (index < parents.length && epoch === generation) await fetchRelations(parents[index++]!.id)
      }))
      if (epoch === generation) {
        const next = nodes.value.find(node => node.id === selected.value?.id) || nodes.value[0]
        if (next) await select(next)
        else selected.value = undefined
      }
    } catch (cause) { if (epoch === generation) error.value = cause instanceof Error ? cause.message : String(cause) }
    finally { if (epoch === generation) loading.value = false }
  }
  async function select(node: Entity) {
    selected.value = node; records.value = []; entries.value = []; detailError.value = ''; detailLoading.value = true
    const epoch = ++detailGeneration
    try {
      const [direct, detail] = await Promise.all([fetchRelations(node.id), getEntity(node.id)])
      if (!detail.success) throw new Error(detail.error)
      if (epoch === detailGeneration) entries.value = detail.data.entries || []
      const fullRecords = new Map<string, Entity>()
      const found = new Map<string, ShelfRecord>(), visited = new Set<string>([node.id])
      async function walk(owner: Entity, rows: EntityRelation[], ancestor: boolean, depth: number) {
        for (const relation of rows) {
          const peer = relation.peer
          if (shelfLevel(peer.kind || '') !== null || peer.archived_at) continue
          const incoming = relation.direction === 'incoming'
          const permitted = incoming ? ['applies_to'] : ['composed_of', 'has_certification', 'has_document', 'fulfills']
          if (!permitted.includes(relation.relation_type)) continue
          const section = shelfSection(peer.kind || '')
          let full = fullRecords.get(peer.id)
          if (!full) {
            const response = await getEntity(peer.id)
            if (!response.success) throw new Error(response.error)
            full = response.data.entity
            fullRecords.set(peer.id, full)
          }
          const existing = found.get(relation.id)
          if (!existing || (existing.ancestor && !ancestor)) found.set(relation.id, { entity: full, owner, relation, section, ancestor })
          if (depth < 3 && !visited.has(peer.id)) {
            visited.add(peer.id)
            await walk(peer, await fetchRelations(peer.id), ancestor, depth + 1)
          }
        }
      }
      await walk(node, direct, false, 0)
      const parentQueue = relations.value.filter(edge => edge.child === node.id).map(edge => edge.parent), seenParents = new Set<string>()
      while (parentQueue.length) {
        const id = parentQueue.shift()!
        if (seenParents.has(id)) continue
        seenParents.add(id)
        const parent = nodes.value.find(item => item.id === id)
        if (parent) await walk(parent, await fetchRelations(id), true, 0)
        parentQueue.push(...relations.value.filter(edge => edge.child === id).map(edge => edge.parent))
      }
      if (epoch === detailGeneration) records.value = [...found.values()]
    } catch (cause) { if (epoch === detailGeneration) detailError.value = cause instanceof Error ? cause.message : String(cause) }
    finally { if (epoch === detailGeneration) detailLoading.value = false }
  }
  async function refresh(node = selected.value) {
    links.value = {}
    await load()
    if (node) {
      const result = await getEntity(node.id)
      if (result.success) { const i = nodes.value.findIndex(item => item.id === node.id); if (i >= 0) nodes.value[i] = result.data.entity; await select(result.data.entity) }
    }
  }
  async function attach(entity: Entity, input?: ManagedRelationInput) {
    if (!input) { nodes.value.push(entity); await refresh(entity); return }
    pending.value = { entity, input }
    await retryLink()
  }
  async function retryLink() {
    if (!pending.value || linking.value) return
    linking.value = true; detailError.value = ''
    try {
      const { entity, input } = pending.value
      const result = await createManagedRelation(library.value.id, input)
      if (!result.success) throw new Error(result.message)
      pending.value = undefined
      const owner = selected.value
      await refresh(shelfLevel(entity.kind || '') !== null ? entity : owner)
    } catch (cause) { detailError.value = cause instanceof Error ? cause.message : String(cause) }
    finally { linking.value = false }
  }
  function dispose() { generation++; detailGeneration++ }
  return { nodes, selected, records, entries, links, relations, loading, detailLoading, error, detailError, pending, linking, load, select, refresh, attach, retryLink, dispose }
}
