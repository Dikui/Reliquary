import { onScopeDispose, ref, watch, type Ref } from 'vue'
import type { ClassifiedEntry } from '@/types/entryClassification'
import type { Entity } from '@/api/entities'
import type { EntityRelation } from '@/api/relations'
import type { EntryRepostSource, LibraryCatalogItem } from '@/api/library'
import { entrySpansPayloadHasTrace, type EntryTraceNode } from '@/utils/entryTrace'
import { isEntryProcessingActive } from '@/utils/entryProcessingStatus'

export interface DetailEntry extends ClassifiedEntry {
  id: string
  title?: string
  file_name?: string
  type?: string
  channel?: string
  file_type?: string
  created_at?: string
  parse_status?: string
  collect_reason?: string
  custom_metadata?: Record<string, unknown>
  repost_sources?: EntryRepostSource[]
  metadata?: NonNullable<ClassifiedEntry['metadata']> & { related_entry_ids?: string[]; related_entities?: Array<{ id: string; name?: string; canonical_name?: string }> }
}
type Envelope = { success: boolean; data?: any; message?: string }
export interface DetailPorts {
  details: (id: string) => Promise<Envelope>
  body: (id: string, signal: AbortSignal) => Promise<{ content: string; bodyHash: string }>
  spans: (id: string) => Promise<Envelope>
  library: (id: string) => Promise<Envelope>
  entity: (id: string) => Promise<Envelope>
  relations: (id: string) => Promise<Envelope>
}
export interface DetailRelation {
  id: string
  type: string
  subject: Pick<Entity, 'id' | 'canonical_name'>
  object: Pick<Entity, 'id' | 'canonical_name'>
  evidence: EntityRelation['sources'][number]['evidence']
}

// Owns request lifetimes for the route. Every response is tied to one entry ID.
export function useEntryDetail(id: Ref<string>, api: DetailPorts) {
  const entry = ref<DetailEntry>(), library = ref<LibraryCatalogItem>()
  const body = ref(''), bodyHash = ref(''), bodyReady = ref(false)
  const loading = ref(false), error = ref(false), bodyError = ref(false)
  const trace = ref<EntryTraceNode>(), traceError = ref(false)
  const entities = ref<Entity[]>([]), relations = ref<DetailRelation[]>([])
  const relationsLoading = ref(false), relationsError = ref(false)
  let generation = 0, relatedGeneration = 0, traceGeneration = 0, bodyGeneration = 0, refreshGeneration = 0
  let controller: AbortController | undefined, timer: ReturnType<typeof setTimeout> | undefined
  let disposed = false
  const current = (version: number) => !disposed && version === generation

  async function loadBody() {
    const version = generation, request = ++bodyGeneration, target = id.value
    controller?.abort(); controller = new AbortController()
    bodyError.value = false
    try {
      const result = await api.body(target, controller.signal)
      if (!current(version) || request !== bodyGeneration) return
      body.value = result.content; bodyHash.value = result.bodyHash; bodyReady.value = true
    } catch {
      if (current(version) && request === bodyGeneration) bodyError.value = true
    }
  }
  async function loadTrace() {
    const version = generation, request = ++traceGeneration
    try {
      const result = await api.spans(id.value)
      if (!current(version) || request !== traceGeneration) return
      if (!result.success) throw new Error()
      trace.value = entrySpansPayloadHasTrace(result.data) ? result.data.trace : undefined
      traceError.value = false
    } catch { if (current(version) && request === traceGeneration) traceError.value = true }
  }
  async function loadRelations() {
    const version = generation, request = ++relatedGeneration, target = id.value
    const linked = entry.value?.entities || []
    relationsLoading.value = true; relationsError.value = false
    const results = await Promise.all(linked.map(async entity => {
      const [profile, links] = await Promise.allSettled([api.entity(entity.id), api.relations(entity.id)])
      return { entity, profile, links }
    }))
    if (!current(version) || request !== relatedGeneration) return
    const found = new Map<string, DetailRelation>()
    entities.value = results.map(({ entity, profile, links }) => {
      if (profile.status !== 'fulfilled' || !profile.value.success || links.status !== 'fulfilled' || !links.value.success) relationsError.value = true
      const value = profile.status === 'fulfilled' && profile.value.success ? profile.value.data?.entity || entity : entity
      if (links.status === 'fulfilled' && links.value.success) {
        for (const group of links.value.data || []) for (const relation of group.relations as EntityRelation[]) {
          const source = (relation.sources || []).find(row => row.entry_id === target)
          if (!source || found.has(relation.id)) continue
          found.set(relation.id, {
            id: relation.id, type: relation.relation_type,
            subject: relation.direction === 'outgoing' ? entity : relation.peer,
            object: relation.direction === 'outgoing' ? relation.peer : entity,
            evidence: (source.evidence || []).map(item => ({ ...item, locations: (item.locations || []).map(location => ({ ...location, body_hash: location.body_hash || source.body_hash })) })),
          })
        }
      }
      return value
    })
    relations.value = [...found.values()]; relationsLoading.value = false
  }
  function schedule() {
    clearTimeout(timer)
    if (entry.value && isEntryProcessingActive(entry.value)) timer = setTimeout(() => { void refresh() }, 2000)
  }
  async function refresh() {
    clearTimeout(timer)
    const version = generation, target = id.value, request = ++refreshGeneration
    try {
      const result = await api.details(target)
      if (!current(version) || request !== refreshGeneration) return
      if (!result.success || !result.data) throw new Error()
      const wasActive = entry.value && isEntryProcessingActive(entry.value)
      entry.value = result.data; error.value = false
      if (wasActive && !isEntryProcessingActive(result.data)) { void loadBody(); void loadRelations() }
      void loadTrace()
    } catch { if (current(version) && request === refreshGeneration) error.value = true }
    finally { if (current(version) && request === refreshGeneration) schedule() }
  }
  async function load() {
    const version = ++generation, target = id.value
    clearTimeout(timer); controller?.abort()
    entry.value = undefined; library.value = undefined; body.value = ''; bodyHash.value = ''; bodyReady.value = false
    entities.value = []; relations.value = []; trace.value = undefined
    error.value = false; bodyError.value = false; traceError.value = false; relationsError.value = false; loading.value = true
    void loadBody(); void loadTrace()
    try {
      const result = await api.details(target)
      if (!current(version)) return
      if (!result.success || !result.data) throw new Error()
      entry.value = result.data
      void loadRelations()
      if (entry.value?.library_id) {
        void api.library(entry.value.library_id).then(result => {
          if (current(version) && result.success) library.value = result.data
        }).catch(() => { /* The entry's library name remains available. */ })
      }
    } catch { if (current(version)) error.value = true }
    finally { if (current(version)) { loading.value = false; schedule() } }
  }
  watch(id, () => { void load() }, { immediate: true })
  onScopeDispose(() => { disposed = true; generation++; controller?.abort(); clearTimeout(timer) })
  return { entry, library, body, bodyHash, bodyReady, loading, error, bodyError, trace, traceError, entities, relations, relationsLoading, relationsError, load, refresh, loadBody, loadTrace, loadRelations }
}
