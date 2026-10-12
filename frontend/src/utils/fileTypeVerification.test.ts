import assert from 'node:assert/strict'
import test from 'node:test'

import { shouldRejectEntryFileType } from './fileTypeVerification.ts'

test('shouldRejectKnowledgeFileType rejects formats removed with the service parser', () => {
  assert.equal(shouldRejectEntryFileType('page.html'), true)
  assert.equal(shouldRejectEntryFileType('legacy.HTM'), true)
  assert.equal(shouldRejectEntryFileType('payload.exe'), true)
  assert.equal(shouldRejectEntryFileType('archive.mhtml'), true)
  assert.equal(shouldRejectEntryFileType('outline.xmind'), true)
  assert.equal(shouldRejectEntryFileType('report.pdf'), false)
})

test('shouldRejectKnowledgeFileType preserves dynamic whitelist behavior', () => {
  assert.equal(shouldRejectEntryFileType('custom.xyz', ['xyz']), false)
  assert.equal(shouldRejectEntryFileType('page.html', ['pdf']), true)
  assert.equal(shouldRejectEntryFileType('page.html', []), true)
})
