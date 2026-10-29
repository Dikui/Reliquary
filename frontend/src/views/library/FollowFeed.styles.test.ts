import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'
import postcss from 'postcss'

// Check source order and specificity for the components mounted by this page.
// This does not replace browser layout or computed-style acceptance.
const paths = ['../../components/EntryUpdateNotice.vue', './components/FollowFeedItem.vue', './components/AddFollowDialog.vue', './FollowFeed.vue']
const sheets = [readFileSync(new URL('../../components/ui/ui.css', import.meta.url), 'utf8')]
for (const path of paths) {
  const filename = new URL(path, import.meta.url).pathname
  for (const style of parse(readFileSync(filename, 'utf8'), { filename }).descriptor.styles) {
    const result = await compileStyleAsync({ filename, source: style.content, id: 'data-v-follow-test', scoped: style.scoped, preprocessLang: style.lang as 'less' | undefined })
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
        const part = parts[index]!.replace(/\[data-v-follow-test\]/g, '')
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

test('follows use design columns, token typography and compact empty states', () => {
  assert.equal(computed(['main.follow-feed-page'], 'grid-template-columns'), 'minmax(0, 1fr) 288px')
  assert.equal(computed(['main.follow-feed-page', 'section.feed-main', 'header.feed-heading', 'h1'], 'font-size'), 'var(--type-page-size)')
  assert.equal(computed(['li.feed-item', 'div.feed-item-body', 'h3.feed-item-title'], 'font-size'), 'var(--text-md)')
  assert.equal(computed(['li.feed-item', 'div.feed-item-body', 'h3.feed-item-title'], 'margin'), '0')
  assert.equal(computed(['main.follow-feed-page', 'section.feed-results', 'section.rq-empty'], 'padding'), 'var(--space-5) 0')
  assert.equal(computed(['li.feed-item', 'span.feed-status.feed-new'], 'background'), 'var(--accent)')
  for (const sheet of sheets.slice(2)) assert.doesNotMatch(sheet, /\b[\d.]+rem\b|--td-/)
  assert.match(sheets.at(-1)!, /@media\s*\(max-width:\s*900px\)/)
})

test('the old update notice inherits the new palette and keeps a visible focus outline', () => {
  assert.equal(computed(['li.feed-item', 'div.feed-meta', 'span.entry-update-notice'], 'color'), 'var(--text)')
  assert.match(sheets[2]!, /\.feed-meta.*?\.entry-update-notice a:focus-visible[^}]*outline-color:\s*var\(--accent\)/s)
})

test('yellow targets retain on-select text including their hints, and page styles never override hidden', () => {
  const page = sheets.at(-1)!
  assert.match(page, /\.feed-target\[aria-current="true"\][^{]*\{[^}]*background:\s*var\(--select\);[^}]*color:\s*var\(--on-select\)/)
  assert.match(page, /\.feed-target\[aria-current="true"\][^{]*\.feed-target-hint[^}]*color:\s*var\(--on-select\)/)
  for (const sheet of sheets.slice(1)) assert.doesNotMatch(sheet, /\[hidden\]/)
})

test('the heading actions and compact toolbar leave the feed first in the reading area', () => {
  assert.equal(computed(['header.feed-heading', 'div.feed-heading-actions'], 'display'), 'flex')
  assert.equal(computed(['header.feed-heading', 'div.feed-heading-actions'], 'align-items'), 'center')
  assert.equal(computed(['header.feed-heading', 'div.feed-read-filters'], 'margin-bottom'), '')
  assert.equal(computed(['form.feed-toolbar'], 'display'), 'flex')
  assert.equal(computed(['form.feed-toolbar'], 'align-items'), 'center')
  assert.equal(computed(['form.feed-toolbar', 'label.feed-library-field', 'select.rq-control'], 'min-height'), '32px')
  assert.equal(computed(['form.feed-toolbar', 'button.rq-button.feed-refresh'], 'min-height'), '32px')
  const source = readFileSync(new URL('./FollowFeed.vue', import.meta.url), 'utf8')
  assert.ok(source.indexOf('v-for="group in groups"') < source.indexOf('class="feed-count"'), 'counts follow materials instead of delaying the first date group')
  assert.doesNotMatch(source, /feed-target-select[^}]*height:\s*72px/)
})
