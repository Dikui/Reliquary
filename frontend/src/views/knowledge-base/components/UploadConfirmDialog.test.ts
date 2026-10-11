import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'

const dialog = readFileSync(new URL('./UploadConfirmDialog.vue', import.meta.url), 'utf8')
const host = readFileSync(new URL('../../../components/UploadConfirmHost.vue', import.meta.url), 'utf8')
const knowledgeBase = readFileSync(new URL('../KnowledgeBase.vue', import.meta.url), 'utf8')
const platform = readFileSync(new URL('../../platform/index.vue', import.meta.url), 'utf8')

test('calls out directory uploads via relative paths in the file list', () => {
  assert.match(dialog, /fileRelativeDir/)
  assert.doesNotMatch(dialog, /folder-upload-banner/)
})

test('routes global knowledge file drops through the upload confirmation flow', () => {
  assert.match(platform, /reliquary:entry-file-drop/)
  assert.match(knowledgeBase, /handleEntryFileDrop/)
  assert.match(knowledgeBase, /handleUploadSourceFiles\(files\)/)
})

test('uses section navigation with inline chunking controls and advanced options grouped', () => {
  assert.match(dialog, /v-model="uiState\.chunkingConfig\.strategy"/)
  assert.match(dialog, /v-model="uiState\.chunkingConfig\.chunkSize"/)
  assert.match(dialog, /v-model="uiState\.chunkingConfig\.chunkOverlap"/)
  assert.match(dialog, /chunkingMoreOpen/)
  assert.match(dialog, /class="files-panel"/)
  assert.match(dialog, /statusFull/)
  assert.match(dialog, /data-section="parser"/)
  assert.doesNotMatch(dialog, /<KBChunkingSettings/)
})
