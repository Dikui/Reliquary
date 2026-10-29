import assert from 'node:assert/strict'
import test from 'node:test'
import { effectScope, nextTick, ref } from 'vue'
import { useEntryDetail } from './useEntryDetail'

const deferred = () => { let resolve!: (value: any) => void; const promise = new Promise<any>(done => { resolve = done }); return { promise, resolve } }
const settle = async () => { for (let i = 0; i < 10; i++) await nextTick() }

test('detail discards old responses and loads the complete body separately from enrichment', async () => {
  const stale = deferred(), id = ref('a')
  const scope = effectScope()
  const state = scope.run(() => useEntryDetail(id, {
    details: async (value) => value === 'a' ? stale.promise : { success: true, data: { id: value, title: 'Current', parse_status: 'completed', entities: [] } },
    body: async value => ({ content: `Full ${value}`, bodyHash: value }),
    spans: async () => ({ success: false }),
    library: async () => ({ success: true, data: {} }),
    entity: async () => ({ success: false }), relations: async () => ({ success: false }),
  }))!
  id.value = 'b'; await settle()
  stale.resolve({ success: true, data: { id: 'a', title: 'Stale' } }); await settle()
  assert.equal(state.entry.value?.title, 'Current')
  assert.equal(state.body.value, 'Full b')
  assert.equal(state.error.value, false)
  assert.equal(state.traceError.value, true)
  scope.stop()
})

test('relations include only this material and deduplicate both endpoints without inventing evidence', async () => {
  const scope = effectScope(), id = ref('doc')
  const relation = { id: 'edge', relation_type: 'builds', direction: 'outgoing', peer: { id: 'b', canonical_name: 'B' }, sources: [{ entry_id: 'doc', body_hash: 'hash', evidence: [{ locations: [{ paragraph: 2, start: 5, end: 10 }], speaker: { name: 'Speaker' } }] }] }
  const state = scope.run(() => useEntryDetail(id, {
    details: async () => ({ success: true, data: { id: 'doc', parse_status: 'completed', entities: [{ id: 'a', canonical_name: 'A' }, { id: 'b', canonical_name: 'B' }] } }),
    body: async () => ({ content: 'body', bodyHash: 'hash' }), spans: async () => ({ success: true, data: {} }), library: async () => ({ success: true, data: {} }),
    entity: async () => ({ success: true, data: {} }),
    relations: async value => ({ success: true, data: [{ relations: [value === 'a' ? relation : { ...relation, direction: 'incoming', peer: { id: 'a', canonical_name: 'A' } }, { ...relation, id: 'unrelated', sources: [{ entry_id: 'other', evidence: [] }] }] }] }),
  }))!
  await settle()
  assert.equal(state.relations.value.length, 1)
  assert.equal(state.relations.value[0]?.subject.canonical_name, 'A')
  assert.equal(state.relations.value[0]?.object.canonical_name, 'B')
  assert.equal(state.relations.value[0]?.evidence[0]?.locations[0]?.body_hash, 'hash')
  scope.stop()
})
