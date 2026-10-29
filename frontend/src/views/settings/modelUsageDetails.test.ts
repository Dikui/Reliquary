import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./ModelSettings.vue', import.meta.url), 'utf8')
const libraryEditorSource = readFileSync(
  new URL('./LibrarySettings.vue', import.meta.url),
  'utf8',
)

test('model deletion renders structured usage groups and localized fallback errors', () => {
  assert.match(source, /error instanceof ModelInUseError/)
  assert.match(source, /usageConflict\.libraries/)
  assert.match(source, /MessagePlugin\.error\(error\.message \|\| t\('modelSettings\.toasts\.deleteFailed'\)\)/)
})

test('referenced knowledge configuration opens in the single settings center', () => {
  assert.match(source, /openUsageResource\('library', resource\.id, resource\.bindings\)/)
  assert.match(source, /uiStore\.openSettings\('entry', librarySection\)/)
  assert.match(libraryEditorSource, /getLibrary\(\)/)
  assert.match(libraryEditorSource, /await updateLibrary\(/)
  assert.match(libraryEditorSource, /if \(request !== generation\) return/)
})
