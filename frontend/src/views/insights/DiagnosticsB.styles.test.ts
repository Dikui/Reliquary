import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import postcss from 'postcss'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'
const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')
const sheet = read('./diagnostics-b.css')
const descriptor = parse(read('../../components/ModelCallContentViewer.vue')).descriptor
const legacy = await Promise.all(descriptor.styles.map(style => compileStyleAsync({ source: style.content, filename: 'ModelCallContentViewer.vue', id: 'data-v-content-test', scoped: true })))
const css = postcss.parse(read('../../components/ui/ui.css') + read('./diagnostics.css') + sheet + legacy.map(s => s.code).join('\n'))
function computed(path: string[], property: string, viewport = 1440) {
  let winner = { weight: -1, value: '' }
  css.walkRules(rule => {
    if (rule.parent?.type === 'atrule') {
      const media = rule.parent as postcss.AtRule
      const maximum = media.params.match(/^\(max-width: (\d+)px\)$/)
      if (media.name !== 'media' || !maximum || viewport > Number(maximum[1])) return
    }
    for (const raw of rule.selectors) {
      if (/[:>+~*]/.test(raw)) continue
      const selector = raw.replace(/\[data-v-content-test\]/g, ''), parts = selector.trim().split(/\s+/)
      if (selector.includes('[')) continue
      let cursor = path.length - 1, matches = true
      for (let i = parts.length - 1; i >= 0; i--) {
        const part = parts[i]!, tag = part.match(/^[\w-]+/)?.[0], classes = [...part.matchAll(/\.([\w-]+)/g)].map(m => m[1]!)
        const match = (node: string) => (!tag || node.split('.')[0] === tag) && classes.every(c => node.split('.').includes(c))
        if (i === parts.length - 1 && !match(path[cursor]!)) { matches = false; break }
        while (cursor >= 0 && !match(path[cursor]!)) cursor--
        if (cursor-- < 0) { matches = false; break }
      }
      if (!matches) continue
      const weight = (raw.match(/[.\[]/g)?.length || 0) * 100 + parts.filter(p => /^[a-z]/.test(p)).length
      rule.walkDecls(property, d => { const priority = weight + (d.important ? 10000 : 0); if (priority >= winner.weight) winner = { weight: priority, value: d.value } })
    }
  })
  return winner.value
}
test('drawer typography overrides the late scoped legacy viewer styles', () => {
  const path = ['section.analytics-dialog', 'div.call-detail', 'section.call-content']
  assert.equal(computed(path, 'color'), 'var(--text)')
  assert.equal(computed([...path, 'button'], 'border-radius'), '0')
  assert.equal(computed([...path, 'button'], 'background'), 'var(--surface)')
  assert.equal(computed([...path, 'article.message-block'], 'border-radius'), '0')
  assert.equal(computed([...path, 'pre'], 'background'), 'var(--surface-soft)')
  assert.equal(computed([...path, 'pre'], 'font-family'), 'var(--mono)')
})
test('tables scroll in their own containers and mobile headings retain top clearance', () => {
  assert.equal(computed(['main.analytics-page', 'div.table-scroll'], 'overflow-x'), 'auto')
  assert.equal(computed(['main.analytics-page', 'div.calls-table', 'table.rq-table'], 'min-width'), '1280px')
  assert.equal(computed(['main.analytics-page', 'table.rq-table', 'td.question-cell'], 'min-width'), '288px')
  assert.match(sheet, /@media \(max-width: 640px\)[\s\S]*padding-top: var\(--space-5\)/)
  assert.doesNotMatch(sheet, /\d(?:\.\d+)?rem\b|\[hidden\]/)
})
test('new pages use shared overlays and pixel tokens without loading old insight styles', () => {
  for (const name of ['EvaluationPage', 'ModelCallsPage', 'QuestionEditor', 'EntryPicker', 'RunResults']) assert.doesNotMatch(read(`./${name}.vue`), /<t-|MessagePlugin|DialogPlugin|insights\.css|\drem\b|entries\?entry_id/)
  const trend = read('./TrendChart.vue')
  assert.doesNotMatch(trend, /--td-|polyline|\drem\b/)
  assert.match(trend, /\.trend-success[^}]*var\(--text\)/)
  assert.match(trend, /\.trend-failure[^}]*var\(--danger\)/)
})


test('narrow forms keep plain labels and controls while configuration differences stay emphasized', () => {
  for (const width of [390, 640, 900, 1440]) {
    for (const parent of ['main.analytics-page', 'section.analytics-dialog']) {
      const fields = [parent, 'div.fields']
      assert.equal(computed(fields, 'text-decoration', width), '', `${width}px form grid has no decoration`)
      assert.equal(computed(fields, 'font-weight', width), '', `${width}px form grid has no inherited bolding`)
      for (const child of ['label.field', 'input.rq-control', 'select.rq-control']) {
        assert.equal(computed([...fields, child], 'text-decoration', width), '', `${width}px ${child}`)
      }
      assert.equal(computed(fields, 'grid-template-columns', width), width <= 640 ? 'minmax(0, 1fr)' : 'repeat(auto-fit, minmax(168px, 1fr))')
    }
    assert.equal(computed(['span.run-config-changed'], 'text-decoration', width), 'underline')
    assert.equal(computed(['span.run-config-changed'], 'font-weight', width), '700')
  }
})
