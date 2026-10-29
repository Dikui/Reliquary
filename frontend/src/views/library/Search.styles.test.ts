import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'
import postcss from 'postcss'

// Compile the same component sheets used by the page, then compare specificity
// and source order for representative elements. This is not a browser layout test.
const sources = ['../../components/EntryUpdateNotice.vue', '../../components/SearchImage.vue', '../../components/SearchFilters.vue', './SearchResult.vue', './GraphSearchPanel.vue', './SearchView.vue']
const sheets = [readFileSync(new URL('../../components/ui/ui.css', import.meta.url), 'utf8')]
for (const source of sources) {
  const filename = new URL(source, import.meta.url).pathname
  for (const style of parse(readFileSync(filename, 'utf8')).descriptor.styles) {
    const result = await compileStyleAsync({ filename, source: style.content, id: 'data-v-search-test', scoped: style.scoped, preprocessLang: style.lang as 'less' | undefined })
    assert.deepEqual(result.errors, [])
    sheets.push(result.code)
  }
}
const css = postcss.parse(sheets.join('\n'))
function computed(path: string[], property: string) {
  let winner = { priority: -1, value: '' }
  css.walkRules(rule => {
    if (rule.parent?.type === 'atrule') return
    for (const original of rule.selectors) {
      const selector = original.replaceAll('[data-v-search-test]', '').replaceAll(':not(:has(.search-result:focus-within))', '')
      if (/[>+~*\[]|:(?!hover|focus-within)/.test(selector)) continue
      const parts = selector.trim().split(/\s+/)
      let cursor = path.length - 1
      let matches = true
      for (let i = parts.length - 1; i >= 0; i--) {
        const part = parts[i]!
        const tag = part.match(/^[\w-]+/)?.[0]
        const classes = [...part.matchAll(/\.([\w-]+)/g)].map(match => match[1]!)
        const pseudo = part.match(/:(hover|focus-within)/)?.[0]
        const match = (node: string) => (!tag || node.split(/[.:]/)[0] === tag) && classes.every(name => node.split(/[.:]/).includes(name)) && (!pseudo || node.includes(pseudo))
        if (i === parts.length - 1 && !match(path[cursor]!)) { matches = false; break }
        while (cursor >= 0 && !match(path[cursor]!)) cursor--
        if (cursor < 0) { matches = false; break }
        cursor--
      }
      if (!matches) continue
      const weight = (original.match(/[.\[:]/g)?.length || 0) * 100 + parts.filter(part => /^[a-z]/.test(part)).length
      rule.walkDecls(property, declaration => {
        const priority = weight + (declaration.important ? 10000 : 0)
        if (priority >= winner.priority) winner = { priority, value: declaration.value }
      })
    }
  })
  return winner.value
}

test('search image and update notice override their legacy colors and round corners', () => {
  assert.equal(computed(['article.search-result', 'figure.search-image', 'div.search-image-preview'], 'border-radius'), '0')
  assert.equal(computed(['article.search-result', 'figure.search-image', 'div.search-image-preview'], 'background'), 'var(--surface-soft)')
  assert.equal(computed(['article.search-result', 'div.result-meta', 'span.entry-update-notice', 'a'], 'color'), 'inherit')
  assert.equal(computed(['article.search-result', 'div.result-snippets', 'mark.search-highlight'], 'background'), 'var(--select)')
  assert.equal(computed(['article.search-result', 'div.result-snippets', 'mark.search-highlight'], 'color'), 'var(--on-select)')
})

test('search uses the mockup measure and a shared wrapping row for libraries and quick filters', () => {
  assert.equal(computed(['main.search-page', 'div.search-wrap'], 'max-width'), '64rem')
  assert.equal(computed(['div.swiss-search-filters', 'div.quick-filters'], 'display'), 'flex')
  assert.equal(computed(['div.swiss-search-filters', 'div.quick-filters'], 'flex-wrap'), 'wrap')
  const page = readFileSync(new URL('./SearchView.vue', import.meta.url), 'utf8')
  const filters = readFileSync(new URL('../../components/SearchFilters.vue', import.meta.url), 'utf8')
  assert.match(page, /<SearchFilters[^>]*>[\s\S]*?<template #libraries>[\s\S]*?library-filter[\s\S]*?<\/SearchFilters>/)
  assert.match(filters, /<div class="quick-filters">\s*<slot name="libraries"/)
})

test('every tag in the yellow focused result or relationship row uses on-select text', () => {
  assert.equal(computed(['article.search-result:focus-within', 'div.result-meta', 'span.rq-tag.rq-tag--plain'], 'color'), 'var(--on-select)')
  assert.equal(computed(['table.graph-table', 'tbody', 'tr:hover', 'td', 'div.graph-source-meta', 'span.rq-tag.rq-tag--plain'], 'color'), 'var(--on-select)')
})

test('search page panel styles preserve the shared hidden rule', () => {
  const page = sheets.at(-1)!
  assert.match(page, /\.rq-tabs__panel:not\(\[hidden\]\)/)
  const shared = sheets[0]!
  assert.match(shared, /\.rq-tabs__panel\[hidden\]\s*\{\s*display:\s*none\s*!important/)
  assert.ok(sheets.some(sheet => sheet.includes('.search-result:focus-within:not(:has(.search-result:focus-within))')), 'a focused folded result does not also highlight its parent result')
})
