import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { ref } from 'vue'
import ts from 'typescript'
import type { Entity, EntityEntry } from '../../../api/entities'
import type { LibraryCatalogItem } from '../../../api/library'
import type { EntityRelation, ManagedRelationInput } from '../../../api/relations'
import * as productShelf from '../../../utils/productShelf'

const require = createRequire(import.meta.url)
function fixture() {
  const entity = (id: string, kind: string): Entity => ({ id, kind, canonical_name: id, entity_type: 'product', space_id: 1, library_id: 'products', aliases: [], created_at: '', updated_at: '' })
  const nodes = [entity('line', 'product_line'), entity('platform', 'platform'), entity('product', 'product'), entity('model', 'model')]
  const extras = [entity('configuration', 'configuration'), entity('market', 'market_scope'), entity('scope', 'verification_scope'), entity('task', 'certification_task'), entity('criterion', 'criterion'), entity('document', 'credential'), entity('source', 'source_record'), entity('ancestor-scope', 'verification_scope'), entity('ancestor-report', 'credential'), entity('second-scope', 'verification_scope')]
  const entities = new Map([...nodes, ...extras].map(item => [item.id, item]))
  entities.get('document')!.aliases = ['Original document alias']
  const links = new Map<string, EntityRelation[]>()
  const addLink = (subject: string, object: string, relationType: string) => {
    const relation = { id: `${subject}-${relationType}-${object}`, relation_type: relationType, library_id: 'products', library_name: 'Products', source_count: 0, sources: [] }
    links.set(subject, [...(links.get(subject) || []), { ...relation, direction: 'outgoing', peer: { ...entities.get(object)!, aliases: undefined } }])
    links.set(object, [...(links.get(object) || []), { ...relation, direction: 'incoming', peer: { ...entities.get(subject)!, aliases: undefined } }])
  }
  addLink('platform', 'line', 'belongs_to'); addLink('product', 'platform', 'belongs_to'); addLink('model', 'product', 'belongs_to')
  addLink('configuration', 'model', 'configuration_of'); addLink('configuration', 'market', 'sale_status')
  addLink('scope', 'model', 'covers'); addLink('task', 'scope', 'evidence_for'); addLink('task', 'criterion', 'verification_item')
  addLink('second-scope', 'model', 'covers'); addLink('task', 'second-scope', 'evidence_for')
  addLink('model', 'document', 'has_document'); addLink('source', 'task', 'source_for')
  addLink('ancestor-scope', 'platform', 'covers'); addLink('ancestor-report', 'ancestor-scope', 'evidence_for')
  const entries: EntityEntry[] = [{ id: 'ordinary-entry', library_id: 'products', title: 'Ordinary material', created_at: '2026-01-01T00:00:00Z' }]
  let rejectLink = false, createCount = 0
  const writes: ManagedRelationInput[] = []
  // Synthetic API fixtures replace transport; the real composable and shelf classification run unchanged.
  const entityApi = {
    listEntities: async ({ kind }: { kind: string }) => ({ success: true, data: nodes.filter(item => item.kind === kind) }),
    getEntity: async (id: string) => ({ success: true, data: { entity: entities.get(id), entries: id === 'model' ? entries : [] } }),
    createManagedEntity: async () => { createCount++; const saved = entity('new-configuration', 'configuration'); entities.set(saved.id, saved); return { success: true, data: saved } },
  }
  const relationApi = {
    listEntityRelations: async (id: string) => ({ success: true, data: [{ relation_type: 'fixture', relations: links.get(id) || [] }] }),
    createManagedRelation: async (_library: string, input: ManagedRelationInput) => {
      writes.push(input)
      if (rejectLink) return { success: false, message: 'Relationship save failed' }
      addLink(input.subject_id, input.object_id, input.relation_type)
      return { success: true }
    },
  }
  const modules: Record<string, unknown> = { '@/api/entities': entityApi, '@/api/relations': relationApi, '@/utils/productShelf': productShelf }
  const code = ts.transpileModule(readFileSync(new URL('./useProductShelf.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as typeof import('./useProductShelf') }
  new Function('require', 'module', 'exports', code)((name: string) => modules[name] ?? require(name), module, module.exports)
  const shelf = module.exports.useProductShelf(ref({ id: 'products' } as LibraryCatalogItem))
  return { shelf, nodes, entries, writes, entityApi, createCount: () => createCount, failLink: (value: boolean) => { rejectLink = value } }
}

// Four-level shelf contract: attached evidence retains its owner and ordinary Entry materials remain separate.
test('selecting a shelf model groups related records with ancestor provenance', async t => {
  const { shelf, nodes, entries } = fixture(); t.after(() => shelf.dispose())
  await shelf.load()
  assert.deepEqual(shelf.nodes.value.map(item => item.id), ['line', 'platform', 'product', 'model'])
  assert.deepEqual(shelf.relations.value.map(edge => [edge.child, edge.parent]).sort(), [['model', 'product'], ['platform', 'line'], ['product', 'platform']])
  await shelf.select(nodes[3]!)
  const sections = Object.fromEntries(['attrs', 'market', 'certs', 'docs'].map(section => [section, shelf.records.value.filter(row => row.section === section).map(row => row.entity.id).sort()]))
  assert.deepEqual(sections, {
    attrs: ['configuration'], market: ['market'],
    certs: ['ancestor-report', 'ancestor-scope', 'criterion', 'scope', 'second-scope', 'task', 'task'], docs: ['document', 'source'],
  })
  assert.deepEqual(shelf.records.value.filter(row => row.ancestor).map(row => row.entity.id).sort(), ['ancestor-report', 'ancestor-scope'])
  assert.equal(shelf.records.value.find(row => row.entity.id === 'ancestor-scope')!.owner.id, 'platform')
  assert.equal(shelf.records.value.filter(row => row.entity.id === 'task').length, 2)
  assert.deepEqual(shelf.records.value.find(row => row.entity.id === 'document')!.entity.aliases, ['Original document alias'])
  assert.deepEqual(shelf.entries.value, entries)
  assert.equal(shelf.detailError.value, '')
})

// A saved entity survives a failed link; retry must not create the entity again.
test('retrying a failed shelf attachment only repeats the relationship write', async t => {
  const context = fixture(), { shelf, nodes, entityApi } = context; t.after(() => shelf.dispose())
  await shelf.load(); await shelf.select(nodes[3]!)
  const created = await entityApi.createManagedEntity()
  const input = { subject_id: created.data.id, object_id: 'model', relation_type: 'configuration_of' }
  context.failLink(true)
  await shelf.attach(created.data, input)
  assert.equal(shelf.pending.value!.entity.id, created.data.id)
  assert.deepEqual(shelf.pending.value!.input, input)
  assert.equal(shelf.detailError.value, 'Relationship save failed')
  assert.equal(context.createCount(), 1)
  context.failLink(false)
  await shelf.retryLink()
  assert.deepEqual(context.writes, [input, input])
  assert.equal(context.createCount(), 1)
  assert.equal(shelf.pending.value, undefined)
  assert.equal(shelf.detailError.value, '')
  assert.equal(shelf.records.value.filter(row => row.entity.id === created.data.id).length, 1)
})
