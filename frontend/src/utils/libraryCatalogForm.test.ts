import assert from 'node:assert/strict'
import test from 'node:test'
import type { LibraryCatalogItem } from '../api/library'
import { librarySettingsPayload, libraryNumberErrors, fillParentChunkDefaults } from './libraryCatalogForm'

test('library edits include persisted color and preserve ordered type and chunking configuration', () => {
  const draft = { name: ' Notes ', description: 'Source notes', color: 6, feed_enabled: false,
    model_tiers: { generation: 'writer', entity_resolution: '', rerank: 'ranker' },
    chunking_config: { chunk_size: 800, chunk_overlap: 0, enable_parent_child: true, parent_chunk_size: 3000, child_chunk_size: 240, separators: ['\n'], strategy: 'heading', token_limit: 0, languages: ['zh'] },
    content_types: [{ name: ' Custom ', enabled: false, extract_graph: true, position: 9 }], relation_types: [],
  } as unknown as LibraryCatalogItem
  const body = librarySettingsPayload(draft)
  assert.equal(body.color, 6)
  assert.equal(body.name, 'Notes')
  assert.deepEqual(body.content_types, [{ name: 'Custom', enabled: false, extract_graph: true, position: 0 }])
  assert.equal(body.chunking_config?.chunk_overlap, 0)
  assert.deepEqual(body.model_tiers, { generation: 'writer', entity_resolution: '', rerank: 'ranker' })
  assert.equal(draft.content_types[0]?.name, ' Custom ')
})


test('numeric validation rejects blanks, non-integers and values outside each field range', () => {
  const valid = { chunk_size: 512, chunk_overlap: 0, parent_chunk_size: 4096, child_chunk_size: 384, token_limit: 0, enable_parent_child: true }
  const ranges = [
    ['chunk_size', 100, 4000], ['chunk_overlap', 0, 500],
    ['parent_chunk_size', 512, 8192], ['child_chunk_size', 64, 2048], ['token_limit', 0, 8192],
  ] as const
  for (const [field, min, max] of ranges) {
    for (const value of [NaN, Infinity, 12.5, min - 1, max + 1, 'bad']) {
      const errors = libraryNumberErrors({ ...valid, [field]: value } as any)
      assert.equal(errors[field]?.reason, 'range', `${field}=${value}`)
    }
    for (const value of [null, undefined, '']) {
      const errors = libraryNumberErrors({ ...valid, [field]: value } as any)
      assert.equal(errors[field]?.reason, field === 'token_limit' ? undefined : 'required')
    }
    for (const value of [min, max]) assert.equal(libraryNumberErrors({ ...valid, [field]: value } as any)[field], undefined)
  }
  assert.deepEqual(libraryNumberErrors({ ...valid, enable_parent_child: false, parent_chunk_size: '', child_chunk_size: null } as any), {})
})

test('main-compatible parent defaults do not replace configured sizes', () => {
  const config = { parent_chunk_size: 0, child_chunk_size: undefined } as any
  fillParentChunkDefaults(config)
  assert.deepEqual(config, { parent_chunk_size: 4096, child_chunk_size: 384 })
  config.parent_chunk_size = 1536; config.child_chunk_size = 256
  fillParentChunkDefaults(config)
  assert.deepEqual(config, { parent_chunk_size: 1536, child_chunk_size: 256 })
})


test('cleared inactive parent sizes produce numeric defaults without mutating the draft', () => {
  const draft = {
    name: 'Notes', content_types: [], relation_types: [],
    chunking_config: { chunk_size: 512, chunk_overlap: 0, enable_parent_child: false, parent_chunk_size: '', child_chunk_size: null, token_limit: '' },
  } as unknown as LibraryCatalogItem
  const body = librarySettingsPayload(draft)
  assert.equal(body.chunking_config?.parent_chunk_size, 4096)
  assert.equal(body.chunking_config?.child_chunk_size, 384)
  assert.equal(body.chunking_config?.token_limit, 0)
  assert.equal(draft.chunking_config.parent_chunk_size, '')
})
