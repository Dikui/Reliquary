import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'

const source = readFileSync(new URL('./Library.vue', import.meta.url), 'utf8')

test('material views have no duplicate header navigation or library settings button', () => {
  const { descriptor } = parse(source)
  assert.doesNotMatch(descriptor.template!.content, /document-header|document-breadcrumb|library-title-actions|library-settings-button|document-subtitle/)
})
