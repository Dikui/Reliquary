import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { nextTick, reactive, ref, watch } from 'vue'

const source = readFileSync(new URL('./KnowledgeBase.vue', import.meta.url), 'utf8')

test('referenced articles fetch details before opening', () => {
  const fetch = source.indexOf('await getEntryDetails(targetId)')
  assert.ok(fetch >= 0)
  assert.ok(source.indexOf('openCardDetails(target)', fetch) > fetch)
})

test('a newer referenced-document navigation supersedes an older request', () => {
  assert.match(source, /const request = \+\+autoOpenRequest/)
  assert.match(source, /if \(request !== autoOpenRequest\) return;/)
})

test('sidebar navigation and article tabs remain synchronized without remounting the page', async () => {
  const start = source.indexOf('watch(() => route.query.tab,')
  const end = source.indexOf('watch(() => kbId.value,', start)
  assert.ok(start >= 0 && end > start)
  const route = reactive({ query: { entry_id: 'article-a' } as Record<string, string> })
  const activeKbTab = ref('documents')
  new Function('watch', 'route', 'activeKbTab', 'router', source.slice(start, end))(
    watch, route, activeKbTab, { replace: ({ query }: { query: Record<string, string> }) => { route.query = query } },
  )
  route.query.tab = 'entities'
  await nextTick()
  assert.equal(activeKbTab.value, 'entities')
  delete route.query.tab
  await nextTick()
  assert.equal(activeKbTab.value, 'documents')
  activeKbTab.value = 'entities'
  await nextTick()
  assert.deepEqual(route.query, { tab: 'entities', entry_id: 'article-a' })
  activeKbTab.value = 'documents'
  await nextTick()
  assert.deepEqual(route.query, { entry_id: 'article-a' })
  activeKbTab.value = 'reviews'
  await nextTick()
  assert.deepEqual(route.query, { tab: 'reviews', entry_id: 'article-a' })
  route.query.tab = 'entities'
  await nextTick()
  assert.equal(activeKbTab.value, 'entities')
})
