import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { nextTick, reactive, ref, watch } from 'vue'

const source = readFileSync(new URL('./Library.vue', import.meta.url), 'utf8')

test('legacy material references navigate to the independent detail route', async () => {
  const start = source.indexOf('const tryAutoOpenDocument =')
  const end = source.indexOf('// React to later', start)
  const pendingEntryId = ref<string | null>('article-a')
  const route = { query: { reference: 'valid' } }
  const pushes: unknown[] = []
  const run = new Function('pendingEntryId', 'route', 'router', 'entryDetailLocation', 'parseReference', source.slice(start, end) + '; return tryAutoOpenDocument')(
    pendingEntryId, route, { replace: (value: unknown) => pushes.push(value) },
    (id: string, location: unknown) => ({ id, location }), () => ({ paragraph: 3 }),
  )
  await run()
  assert.deepEqual(pushes, [{ id: 'article-a', location: { paragraph: 3 } }])
  assert.equal(pendingEntryId.value, null)
})

test('sidebar navigation and article tabs remain synchronized without remounting the page', async () => {
  const start = source.indexOf('watch(() => route.query.tab,')
  const end = source.indexOf('watch(() => libraryId.value,', start)
  assert.ok(start >= 0 && end > start)
  const route = reactive({ query: { entry_id: 'article-a' } as Record<string, string> })
  const activeLibraryTab = ref('documents')
  new Function('watch', 'route', 'activeLibraryTab', 'router', source.slice(start, end))(
    watch, route, activeLibraryTab, { replace: ({ query }: { query: Record<string, string> }) => { route.query = query } },
  )
  route.query.tab = 'entities'
  await nextTick()
  assert.equal(activeLibraryTab.value, 'entities')
  delete route.query.tab
  await nextTick()
  assert.equal(activeLibraryTab.value, 'documents')
  activeLibraryTab.value = 'entities'
  await nextTick()
  assert.deepEqual(route.query, { tab: 'entities', entry_id: 'article-a' })
  activeLibraryTab.value = 'documents'
  await nextTick()
  assert.deepEqual(route.query, { entry_id: 'article-a' })
  activeLibraryTab.value = 'reviews'
  await nextTick()
  assert.deepEqual(route.query, { tab: 'reviews', entry_id: 'article-a' })
  route.query.tab = 'entities'
  await nextTick()
  assert.equal(activeLibraryTab.value, 'entities')
})
