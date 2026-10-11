import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./ModelSettings.vue', import.meta.url), 'utf8')
const knowledgeBaseEditorSource = readFileSync(
  new URL('./KnowledgeBaseSettings.vue', import.meta.url),
  'utf8',
)

test('model deletion renders structured usage groups and localized fallback errors', () => {
  assert.match(source, /error instanceof ModelInUseError/)
  assert.match(source, /usageConflict\.knowledge_bases/)
  assert.match(source, /MessagePlugin\.error\(error\.message \|\| t\('modelSettings\.toasts\.deleteFailed'\)\)/)
})

test('referenced knowledge configuration opens in the single settings center', () => {
  assert.match(source, /openUsageResource\('knowledge_base', resource\.id, resource\.bindings\)/)
  assert.match(source, /uiStore\.openSettings\('entry', knowledgeBaseSection\)/)
  assert.match(knowledgeBaseEditorSource, /getKnowledgeBase\(\)/)
  assert.match(knowledgeBaseEditorSource, /await updateKnowledgeBase\(/)
  assert.match(knowledgeBaseEditorSource, /if \(request !== generation\) return/)
})
