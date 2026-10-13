import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'

const selector = readFileSync(new URL('./ModelSelector.vue', import.meta.url), 'utf8')
const libraryEditor = readFileSync(new URL('../views/settings/LibrarySettings.vue', import.meta.url), 'utf8')

function modelSelectorTag(source: string, selectedModelBinding: string): string {
  const tag = source
    .match(/<ModelSelector\b[\s\S]*?\/>/g)
    ?.find((candidate) => candidate.includes(selectedModelBinding))
  assert.ok(tag, `找不到模型选择器：${selectedModelBinding}`)
  return tag
}

function assertClearable(tag: string): void {
  assert.match(tag, /(?:\s|:)clearable(?:\s|=|\/>)/)
}

function assertNotClearable(tag: string): void {
  assert.doesNotMatch(tag, /(?:\s|:)clearable(?:\s|=|\/>)/)
}

test('ModelSelector 清空时向父组件回传空字符串，默认仍不可清空', () => {
  assert.match(selector, /:clearable="clearable"/)
  assert.match(selector, /clearable:\s*false/)
  assert.match(selector, /emit\('update:selectedModelId', value \|\| ''\)/)
})
