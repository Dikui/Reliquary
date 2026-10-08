import assert from 'node:assert/strict'
import test from 'node:test'

import { shouldRejectKnowledgeFileType } from './fileTypeVerification.ts'

test('shouldRejectKnowledgeFileType rejects formats removed with the service parser', () => {
  assert.equal(shouldRejectKnowledgeFileType('page.html'), true)
  assert.equal(shouldRejectKnowledgeFileType('legacy.HTM'), true)
  assert.equal(shouldRejectKnowledgeFileType('payload.exe'), true)
  assert.equal(shouldRejectKnowledgeFileType('archive.mhtml'), true)
  assert.equal(shouldRejectKnowledgeFileType('outline.xmind'), true)
  assert.equal(shouldRejectKnowledgeFileType('report.pdf'), false)
})

test('shouldRejectKnowledgeFileType preserves dynamic whitelist behavior', () => {
  assert.equal(shouldRejectKnowledgeFileType('custom.xyz', ['xyz']), false)
  assert.equal(shouldRejectKnowledgeFileType('page.html', ['pdf']), true)
  assert.equal(shouldRejectKnowledgeFileType('page.html', []), true)
})
