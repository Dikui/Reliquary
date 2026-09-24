import assert from 'node:assert/strict'
import test from 'node:test'
import { formatTemporarySummary } from './temporarySummary'

test('temporary summary keeps the answer and deduplicated formal sources', () => {
  const text = formatTemporarySummary('Evidence-backed analysis', [
    { id: 'chunk-1', knowledge_id: 'message-1', knowledge_base_id: 'kb-1', knowledge_title: 'First message', content: 'A' },
    { id: 'chunk-2', knowledge_id: 'message-1', knowledge_title: 'First message', content: 'B' },
    { id: 'chunk-3', knowledge_id: 'message-2', knowledge_title: 'Second message', content: 'C' },
    { id: 'https://example.com/unreviewed', chunk_type: 'web_search', metadata: { url: 'https://example.com/unreviewed' } },
  ], 'https://qa.example')
  assert.match(text, /^Evidence-backed analysis/)
  assert.equal(text.match(/First message/g)?.length, 1)
  assert.match(text, /Second message/)
  assert.match(text, /\[First message\]\(https:\/\/qa\.example\/platform\/knowledge-bases\/kb-1\?knowledge_id=message-1\)/)
  assert.doesNotMatch(text, /unreviewed/)
})
