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

test('same-route entity bookmarks redirect while legacy merge review stays reachable', async () => {
  const start = source.indexOf('watch(() => [route.query.tab, route.query.section],')
  const end = source.indexOf('watch(() => libraryId.value,', start)
  const route = reactive({ query: {} as Record<string, string>, hash: '#profile' })
  const activeLibraryTab = ref('documents')
  const replacements: any[] = []
  new Function('watch', 'route', 'activeLibraryTab', 'router', source.slice(start, end))(
    watch, route, activeLibraryTab, { replace: (target: any) => { replacements.push(target); if (!target.path) route.query = target.query } },
  )
  route.query = { tab: 'entities', entity_id: 'company', libraries: 'lib-a' }
  await nextTick()
  assert.deepEqual(replacements.at(-1), { path: '/platform/entities', query: { entity_id: 'company', libraries: 'lib-a' }, hash: '#profile' })
  route.query = { tab: 'entities', section: 'merges', libraries: 'lib-a' }
  await nextTick()
  assert.equal(activeLibraryTab.value, 'entities')
  route.query = { tab: 'reviews' }
  await nextTick()
  assert.equal(activeLibraryTab.value, 'reviews')
  activeLibraryTab.value = 'documents'
  await nextTick()
  assert.deepEqual(route.query, {})
})
