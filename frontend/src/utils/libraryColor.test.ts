import assert from 'node:assert/strict'
import test from 'node:test'
import type { CreateLibraryRequest, LibraryCatalogItem, LibraryColor, LibraryPatch } from '../api/library'
import { libraryColorVar } from './libraryColor'

test('catalog library colors resolve to the shared palette variables', () => {
  const colors: LibraryColor[] = [1, 2, 3, 4, 5, 6, 7, 8]
  assert.deepEqual(colors.map(libraryColorVar), [
    'var(--lib-1)', 'var(--lib-2)', 'var(--lib-3)', 'var(--lib-4)',
    'var(--lib-5)', 'var(--lib-6)', 'var(--lib-7)', 'var(--lib-8)',
  ])
  const library: Pick<LibraryCatalogItem, 'color'> = { color: 6 }
  assert.equal(libraryColorVar(library.color), 'var(--lib-6)')
})

test('library creation can omit the color or choose a palette color', () => {
  const automatic: CreateLibraryRequest = { name: 'Notes', directory: 'notes', description: '' }
  const chosen: CreateLibraryRequest = { ...automatic, color: 8 }
  assert.equal(automatic.color, undefined)
  assert.equal(libraryColorVar(chosen.color!), 'var(--lib-8)')
  const patch: LibraryPatch = { color: 3 }
  assert.equal(libraryColorVar(patch.color!), 'var(--lib-3)')
  // @ts-expect-error The library palette excludes zero.
  const zero: LibraryColor = 0
  // @ts-expect-error The library palette has only eight colors.
  const overflow: LibraryCatalogItem['color'] = 9
  void zero
  void overflow
  // @ts-expect-error Library updates also reject values outside the palette.
  const invalidPatch: LibraryPatch = { color: -1 }
  void invalidPatch
})
