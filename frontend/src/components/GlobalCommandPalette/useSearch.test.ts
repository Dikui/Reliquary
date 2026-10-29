import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import ts from 'typescript'
import { ref } from 'vue'
import { mount, settle } from '../ui/__tests__/dom'

const require = createRequire(import.meta.url)
function searchHook(searchEntries: Function, listEntities: Function) {
  const module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL('./useSearch.ts', import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS },
  }).outputText
  const modules: Record<string, unknown> = {
    '@/api/search': { searchEntries }, '@/api/entities': { listEntities },
    '@/utils/search': { searchErrorMessage: () => 'Search failed' },
    '@/composables/useLibraryScope': { useLibraryScope: () => ref(['a', 'b']) },
  }
  new Function('require', 'module', 'exports', code)((name: string) => modules[name] ?? require(name), module, module.exports)
  return module.exports.useCmdkSearch
}

test('palette searches standalone entities in the selected libraries and preserves material metadata', async t => {
  const calls: any[] = []
  const useSearch = searchHook(async () => ({ success: true, data: { results: [{
    entry_id: 'entry', title: 'Material', library_id: 'a', content_type: 'research', published_at: '2026-10-11',
    source: 'article', snippets: [], score: 0.8,
  }] } }), async (params: any) => {
    calls.push(params)
    return { success: true, data: [{ id: params.library_id, library_id: params.library_id, canonical_name: 'Orion', entity_type: 'company' }] }
  })
  let state: any
  const mounted = mount({ setup() { state = useSearch({ debounceMs: 0, libraries: ref([{ id: 'a' }, { id: 'b' }, { id: 'c' }]) }); return () => null } })
  t.after(() => mounted.unmount())
  state.query.value = ' Orion '
  await state.retry()
  assert.deepEqual(calls, [
    { library_id: 'a', entity: 'Orion', limit: 5 }, { library_id: 'b', entity: 'Orion', limit: 5 },
  ])
  assert.deepEqual(state.entities.value.map((item: any) => item.id), ['a', 'b'])
  assert.equal(state.fileGroups.value[0].contentType, 'research')
  assert.equal(state.fileGroups.value[0].publishedAt, '2026-10-11')
  assert.deepEqual(state.fileGroups.value[0].chunks, [], 'a summary fallback is not a located body snippet')
})

test('a new query invalidates old palette results before debounce and ignores late responses', async t => {
  let resolveOld!: (value: any) => void
  const useSearch = searchHook(() => new Promise(resolve => { resolveOld = resolve }), async () => ({ success: true, data: [] }))
  let state: any
  const mounted = mount({ setup() { state = useSearch({ debounceMs: 1000 }); return () => null } })
  t.after(() => mounted.unmount())
  state.query.value = 'old'
  await settle()
  const pending = state.retry()
  state.query.value = 'new'
  await settle()
  resolveOld({ success: true, data: { results: [{ entry_id: 'old', title: 'Old', snippets: [], score: 1 }] } })
  await pending
  assert.deepEqual(state.fileGroups.value, [])
  assert.equal(state.hasSearched.value, false)
  state.clearResults()
  state.query.value = ''
})

test('entity failures retain material results and successful libraries, and retry clears the error', async t => {
  let failed = true, state: any
  const useSearch = searchHook(async () => ({ success: true, data: { results: [] } }), async ({ library_id }: any) => {
    if (library_id === 'a' && failed) throw new Error('private upstream details')
    return { success: true, data: [{ id: library_id, library_id, canonical_name: 'Orion', entity_type: 'company' }] }
  })
  const mounted = mount({ setup() { state = useSearch({ debounceMs: 1000, libraries: ref([{ id: 'a' }, { id: 'b' }]) }); return () => null } })
  t.after(() => mounted.unmount())
  state.query.value = 'Orion'
  await settle()
  await state.retry()
  assert.equal(state.entityError.value, 'Some entities could not be searched. Retry to load them.')
  assert.deepEqual(state.entities.value.map((item: any) => item.id), ['b'])
  failed = false
  await state.retry()
  assert.equal(state.entityError.value, '')
  assert.deepEqual(state.entities.value.map((item: any) => item.id), ['a', 'b'])
})
