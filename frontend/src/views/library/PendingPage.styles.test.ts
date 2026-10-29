import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'
import postcss from 'postcss'

// Check source order and specificity for the components mounted by this page.
// This does not replace browser layout or computed-style acceptance.
const paths = ['./components/MergeCandidate.vue', './components/PendingReviewActions.vue', './components/MergeSuggestions.vue', './components/PendingReviews.vue', './PendingPage.vue']
const sheets = [readFileSync(new URL('../../components/ui/ui.css', import.meta.url), 'utf8')]
for (const path of paths) {
  const filename = new URL(path, import.meta.url).pathname
  for (const style of parse(readFileSync(filename, 'utf8'), { filename }).descriptor.styles) {
    const result = await compileStyleAsync({ filename, source: style.content, id: 'data-v-pending-test', scoped: style.scoped, preprocessLang: style.lang as 'less' | undefined })
    assert.deepEqual(result.errors, [])
    sheets.push(result.code)
  }
}
const css = postcss.parse(sheets.join('\n'))
function computed(path: string[], property: string) {
  let winner = { weight: -1, value: '' }
  css.walkRules(rule => {
    // Screen fixtures exclude print and responsive rules.
    if (rule.parent?.type === 'atrule') return
    for (const selector of rule.selectors) {
      if (/[:>+~*]/.test(selector)) continue
      const parts = selector.trim().split(/\s+/)
      let cursor = path.length - 1
      let matches = true
      for (let index = parts.length - 1; index >= 0; index--) {
        const part = parts[index]!.replace(/\[data-v-pending-test\]/g, '')
        if (part.includes('[')) { matches = false; break }
        const tag = part.match(/^[\w-]+/)?.[0]
        const classes = [...part.matchAll(/\.([\w-]+)/g)].map(match => match[1]!)
        const match = (node: string) => (!tag || node.split('.')[0] === tag) && classes.every(name => node.split('.').includes(name))
        if (index === parts.length - 1 && !match(path[cursor]!)) { matches = false; break }
        while (cursor >= 0 && !match(path[cursor]!)) cursor--
        if (cursor < 0) { matches = false; break }
        cursor--
      }
      if (!matches) continue
      const weight = (selector.match(/[.\[]/g)?.length || 0) * 100 + parts.filter(part => /^[a-z]/.test(part)).length
      rule.walkDecls(property, declaration => {
        const priority = weight + (declaration.important ? 10000 : 0)
        if (priority >= winner.weight) winner = { weight: priority, value: declaration.value }
      })
    }
  })
  return winner.value
}


test('pending layout uses design widths, token typography and compact empty states', () => {
  assert.equal(computed(['main.pending-page', 'div.pending-wrap'], 'max-width'), '1152px')
  assert.equal(computed(['header.pending-heading', 'h1'], 'font'), '800 var(--text-3xl)/1 var(--font)')
  assert.equal(computed(['div.merge-pair'], 'grid-template-columns'), 'minmax(0, 1fr) minmax(0, 1fr)')
  assert.equal(computed(['div.merge-pair'], 'border'), '2px solid var(--rule)')
  assert.equal(computed(['article.merge-candidate', 'h3'], 'font'), '800 var(--text-2xl)/1.1 var(--font)')
  assert.equal(computed(['li.review-record', 'h3'], 'font'), '800 var(--text-md)/var(--leading) var(--font)')
  assert.equal(computed(['div.pending-wrap', 'section.rq-empty'], 'padding'), 'var(--space-5) 0')
  assert.equal(computed(['section.pending-reviews'], 'min-height'), '')
  assert.equal(computed(['section.merge-suggestions'], 'min-height'), '')
})

test('evidence and review content override legacy text styles and keep source links visible', () => {
  assert.equal(computed(['article.merge-candidate', 'blockquote.candidate-evidence'], 'margin'), '0')
  assert.equal(computed(['article.merge-candidate', 'blockquote.candidate-evidence', 'p', 'mark'], 'color'), 'var(--on-select)')
  assert.equal(computed(['article.merge-candidate', 'blockquote.candidate-evidence', 'p', 'mark'], 'background'), 'var(--select)')
  assert.equal(computed(['article.merge-candidate', 'a'], 'color'), 'var(--text)')
  assert.equal(computed(['li.review-record', 'h3', 'a'], 'text-decoration'), 'underline')
  assert.equal(computed(['section.pending-review-actions', 'p.review-error'], 'color'), 'var(--danger)')
  assert.equal(computed(['article.merge-candidate'], 'overflow-wrap'), 'anywhere')
  assert.equal(computed(['blockquote.candidate-evidence', 'p'], 'overflow'), 'auto')
})

test('pending pages stack on narrow screens, never override hidden and contain no legacy controls', () => {
  for (const sheet of sheets.slice(1)) assert.doesNotMatch(sheet, /\[hidden\]|\b[\d.]+rem\b|--td-/)
  assert.match(sheets[3]!, /@media\s*\(max-width:\s*760px\)/)
  assert.match(sheets[3]!, /grid-template-columns:\s*minmax\(0, 1fr\)/)
  for (const path of paths) {
    const source = readFileSync(new URL(path, import.meta.url), 'utf8')
    assert.doesNotMatch(source, /<t-|MessagePlugin|DialogPlugin/)
  }
})
