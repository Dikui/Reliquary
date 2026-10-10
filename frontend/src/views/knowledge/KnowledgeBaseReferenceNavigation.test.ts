import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./KnowledgeBase.vue', import.meta.url), 'utf8')

test('referenced articles fetch details before opening', () => {
  const fetch = source.indexOf('await getKnowledgeDetails(targetId)')
  assert.ok(fetch >= 0)
  assert.ok(source.indexOf('openCardDetails(target)', fetch) > fetch)
})

test('a newer referenced-document navigation supersedes an older request', () => {
  assert.match(source, /const request = \+\+autoOpenRequest/)
  assert.match(source, /if \(request !== autoOpenRequest\) return;/)
})
