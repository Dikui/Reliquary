import assert from 'node:assert/strict'
import test from 'node:test'
import { visibleFileChunks } from './visibleFileChunks.ts'
import type { CmdkFileGroup, CmdkChunk } from './useSearch'

test('search displays one result per message, preserving the best matching chunk', () => {
  const chunk = (id: string): CmdkChunk => ({
    id, chunk_index: 0, entry_id: id, library_id: 'library', entry_title: id,
    content: id, match_type: 'keyword', score: 1,
  })
  const files: CmdkFileGroup[] = [
    { entryId: 'a', libraryId: 'library', title: 'A', chunks: [chunk('a1'), chunk('a2')] },
    { entryId: 'b', libraryId: 'library', title: 'B', chunks: [chunk('b1')] },
  ]
  assert.deepEqual(visibleFileChunks(files, 2).map(item => item.chunk.id), ['a1', 'b1'])
})
