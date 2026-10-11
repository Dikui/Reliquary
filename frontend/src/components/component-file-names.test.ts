import assert from 'node:assert/strict'
import { readdirSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

function vueFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name)
    return entry.isDirectory() ? vueFiles(path) : entry.name.endsWith('.vue') ? [path] : []
  })
}

test('component and view Vue filenames use PascalCase except route index files', () => {
  const src = fileURLToPath(new URL('../', import.meta.url))
  const files = ['components', 'views'].flatMap(directory => vueFiles(join(src, directory)))
  assert.ok(files.length > 0, 'the naming check must scan existing Vue files')
  const invalid = files.filter(path => !/^(index|[A-Z][A-Za-z0-9]*)\.vue$/.test(path.split('/').pop()!))
  assert.deepEqual(invalid.map(path => relative(src, path)), [])
})
