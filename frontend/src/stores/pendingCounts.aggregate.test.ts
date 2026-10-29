import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { createPinia, defineStore, setActivePinia } from 'pinia'
import ts from 'typescript'

const require = createRequire(import.meta.url)
function fixture(overrides: Record<string, unknown> = {}) {
  const calls: unknown[] = []
  const modules: Record<string, unknown> = {
    pinia: { defineStore },
    '@/api/entry-review': { listPendingEntry: async () => ({ success: true, total: 4 }) },
    '@/api/library': { listLibraries: async () => ({ success: true, data: [{ id: 'a' }, { id: 'b' }] }) },
    '@/api/entities': { listEntityMergeSuggestions: async (query: { library_id: string; offset: number }) => {
      calls.push(query)
      const length = query.library_id === 'a' ? (query.offset === 0 ? 100 : 1) : 2
      return { success: true, data: Array.from({ length }, (_, i) => ({ id: `${query.library_id}-${query.offset + i}` })) }
    } },
    ...overrides,
  }
  const source = readFileSync(new URL('./pendingCounts.ts', import.meta.url), 'utf8')
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', code)((name: string) => modules[name] ?? require(name), module, module.exports)
  setActivePinia(createPinia())
  return { store: module.exports.usePendingCountsStore(), calls }
}

test('pending total adds intake and every page of merge suggestions across libraries', async () => {
  const { store, calls } = fixture()
  await store.refresh()
  assert.equal(store.total, 107)
  assert.deepEqual(calls, [
    { library_id: 'a', status: 'pending', limit: 100, offset: 0 },
    { library_id: 'b', status: 'pending', limit: 100, offset: 0 },
    { library_id: 'a', status: 'pending', limit: 100, offset: 100 },
  ])
  store.$dispose()
})

test('partial or malformed merge counts hide the whole total and permit retry', async () => {
  let response: unknown = { success: false, data: [] }
  const { store } = fixture({ '@/api/entities': { listEntityMergeSuggestions: async () => response } })
  for (const invalid of [{ success: false, data: [] }, { success: true }, { success: true, data: [{}] }]) {
    response = invalid
    await store.refresh()
    assert.equal(store.total, null)
    assert.equal(store.loading, false)
  }
  response = { success: true, data: [] }
  await store.refresh()
  assert.equal(store.total, 4)
  store.$dispose()
})

test('a failed library catalogue cannot be mistaken for no merge suggestions', async () => {
  const { store } = fixture({ '@/api/library': { listLibraries: async () => ({ success: false, data: [] }) } })
  await store.refresh()
  assert.equal(store.total, null)
  store.$dispose()
})

test('a later merge page failure never exposes the successfully loaded partial count', async () => {
  const { store } = fixture({
    '@/api/library': { listLibraries: async () => ({ success: true, data: [{ id: 'a' }] }) },
    '@/api/entities': { listEntityMergeSuggestions: async ({ offset }: { offset: number }) => {
      if (offset > 0) throw new Error('Second page unavailable')
      return { success: true, data: Array.from({ length: 100 }, (_, index) => ({ id: String(index) })) }
    } },
  })
  await store.refresh()
  assert.equal(store.total, null)
  store.$dispose()
})

test('an obsolete refresh cannot restore a partial total after a newer refresh', async () => {
  let resolveOld!: (value: unknown) => void
  let requests = 0
  const { store } = fixture({
    '@/api/library': { listLibraries: async () => ({ success: true, data: [{ id: 'a' }] }) },
    '@/api/entities': { listEntityMergeSuggestions: () => ++requests === 1
      ? new Promise(resolve => { resolveOld = resolve })
      : Promise.resolve({ success: true, data: [{ id: 'new' }] }) },
  })
  const old = store.refresh()
  await new Promise(resolve => setImmediate(resolve))
  await store.refresh()
  resolveOld({ success: true, data: [] })
  await old
  assert.equal(store.total, 5)
  store.$dispose()
})
