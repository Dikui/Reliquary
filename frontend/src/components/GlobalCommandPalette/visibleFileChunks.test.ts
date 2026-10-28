import assert from 'node:assert/strict'
import test from 'node:test'
import { visibleFileChunks } from './visibleFileChunks.ts'
import type { CmdkFileGroup, CmdkChunk } from './useSearch'

test('search displays one result per message, preserving the best matching chunk', () => {
  const chunk = (id: string): CmdkChunk => ({
    id, chunk_index: 0, entry_id: id, library_id: 'library', entry_title: id,
    content: id, match_type: 'keyword', score: 1,
    locations: [{ heading_path: [], paragraph: 1, start: 0, end: 10, body_hash: 'hash', mapping: 'exact' }],
  })
  const files: CmdkFileGroup[] = [
    { entryId: 'a', libraryId: 'library', title: 'A', chunks: [chunk('a1'), chunk('a2')] },
    { entryId: 'b', libraryId: 'library', title: 'B', chunks: [chunk('b1')] },
  ]
  assert.deepEqual(visibleFileChunks(files, 2).map(item => item.chunk.id), ['a1', 'b1'])
})

test('body snippets exclude summary candidates and require a real chunk with a source location', () => {
  const located: CmdkChunk = {
    id: 'body-chunk', chunk_index: 0, entry_id: 'mixed', library_id: 'library', entry_title: 'Mixed',
    content: 'Saved body', match_type: 'keyword', score: 1,
    locations: [{ heading_path: [], paragraph: 2, start: 10, end: 20, body_hash: 'hash', mapping: 'exact' }],
  }
  const summary: CmdkChunk = { ...located, id: '', chunk_index: -1, content: 'Model summary', locations: [] }
  const files: CmdkFileGroup[] = [
    { entryId: 'summary', libraryId: 'library', title: 'Summary only', chunks: [summary] },
    { entryId: 'no-location', libraryId: 'library', title: 'No location', chunks: [{ ...located, locations: [] }] },
    { entryId: 'no-id', libraryId: 'library', title: 'No ID', chunks: [{ ...located, id: '' }] },
    { entryId: 'negative-index', libraryId: 'library', title: 'Not a body chunk', chunks: [{ ...located, chunk_index: -1 }] },
    { entryId: 'mixed', libraryId: 'library', title: 'Mixed', chunks: [summary, located] },
  ]
  const visible = visibleFileChunks(files, 1)
  assert.deepEqual(visible.map(item => [item.file.entryId, item.chunk.id]), [['mixed', 'body-chunk']])
  assert.equal(visible[0]!.chunk.locations?.[0]?.paragraph, 2)
})
