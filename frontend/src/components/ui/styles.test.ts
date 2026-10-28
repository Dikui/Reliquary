import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const tokens = readFileSync(new URL('../../styles/tokens.css', import.meta.url), 'utf8')
const styles = readFileSync(new URL('./ui.css', import.meta.url), 'utf8')
const preview = readFileSync(new URL('../../views/dev/UiPreview.vue', import.meta.url), 'utf8')

function declarations(source: string, selector: string) {
  const rules = [...source.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/([^{}]+)\{([^{}]*)\}/g)]
  const rule = rules.find(match => match[1]!.trim() === selector)
  assert.ok(rule, `Missing selector: ${selector}`)
  const body = rule[2]!
  return Object.fromEntries(body.split(';').map(value => value.trim()).filter(Boolean).map(value => {
    const colon = value.indexOf(':')
    return [value.slice(0, colon).trim(), value.slice(colon + 1).trim()]
  }))
}

test('Swiss dimensions preserve the 16px design baseline under the legacy 14px root', () => {
  const light = declarations(tokens, ':root')
  assert.deepEqual(Object.fromEntries(Object.keys(light).filter(key => key.startsWith('--text-')).map(key => [key, light[key]])), {
    '--text-xs': '12px', '--text-sm': '14px', '--text-md': '16px', '--text-lg': '20px',
    '--text-xl': '28px', '--text-2xl': '36px', '--text-3xl': '44px',
  })
  assert.deepEqual(Array.from({ length: 7 }, (_, index) => light[`--space-${index + 1}`]),
    ['4px', '8px', '12px', '16px', '24px', '32px', '48px'])
  assert.equal(light['--control'], '40px')
  assert.equal(light['--content'], '1216px')
  assert.equal(light['--sidebar'], '224px')
  assert.equal(light['--switcher-height'], '48px')
  assert.equal(declarations(styles, '.rq-control')['min-height'], 'var(--control)')
  assert.equal(declarations(styles, '.rq-control').padding, 'var(--space-2) var(--space-3)')
  assert.equal(declarations(styles, '.rq-button')['min-height'], 'var(--control)')
  assert.equal(declarations(styles, '.rq-textarea')['min-height'], '96px')
  assert.equal(declarations(styles, '.rq-checkbox, .rq-radio').width, '17.6px')
  assert.equal(declarations(styles, '.rq-checkbox, .rq-radio').height, '17.6px')
  for (const source of [tokens, styles, preview]) assert.doesNotMatch(source, /\b\d*\.?\d+rem\b/, 'new dimensions must not depend on the legacy root size')
})

test('body and button text use the normal scale while field controls match the design weight', () => {
  const base = styles.match(/font:\s*([^;]+);/)![1]
  assert.equal(base, 'var(--text-md)/var(--leading) var(--font)')
  assert.equal(declarations(styles, ':where(.rq-field, .rq-chip, .rq-table-wrap)')['font-size'], 'var(--text-sm)')
  assert.equal(declarations(styles, '.rq-field .rq-control')['font-size'], 'var(--text-sm)')
  assert.equal(declarations(styles, '.rq-field .rq-control')['font-weight'], '600')
  assert.equal(declarations(styles, '.rq-field__label')['font-weight'], '600')
})

test('checked checkboxes and radios use the accent palette in both themes', () => {
  assert.equal(declarations(styles, '.rq-checkbox, .rq-radio')['accent-color'], 'var(--accent)')
  assert.equal(declarations(tokens, ':root')['--accent'], '#e2231a')
  assert.equal(declarations(tokens, ':root[theme-mode="dark"]')['--accent'], '#ff3b30')
})

test('focused rows keep status, muted and tag text on the selection palette in both themes', () => {
  const foreground = declarations(styles, ':is(.rq-focus-row, .rq-table__row--focused),\n:is(.rq-focus-row, .rq-table__row--focused) *')
  assert.equal(foreground.color, 'var(--on-select) !important', 'focused descendants override their semantic and hover text colors')
  assert.equal(declarations(styles, '.rq-focus-row').background, 'var(--select)')
  assert.equal(declarations(styles, ':is(.rq-focus-row, .rq-table__row--focused) .rq-tag').background, 'transparent', 'tag backgrounds must not hide selection text')
  const light = declarations(tokens, ':root')
  const dark = declarations(tokens, ':root[theme-mode="dark"]')
  assert.equal(light['--on-select'], '#111111')
  assert.equal(dark['--on-select'] || light['--on-select'], '#111111')
})

test('hidden tab panels cannot be made visible by a page layout display rule', () => {
  assert.equal(declarations(styles, '.rq-tabs__panel[hidden]').display, 'none !important')
  const detail = readFileSync(new URL('../../views/library/EntryDetail.vue', import.meta.url), 'utf8')
  assert.match(detail, /:deep\(\.rq-tabs__panel:not\(\[hidden\]\)\)/)
})

test('active tabs retain the contrasting palette on hover in both themes', () => {
  assert.equal(declarations(styles, '.rq-tabs__trigger:not(:disabled):not([data-state="active"]):hover').background, 'var(--surface-soft)')
  const active = declarations(styles, '.rq-tabs__trigger[data-state="active"]')
  assert.equal(active.background, 'var(--text)')
  assert.equal(active.color, 'var(--bg)')
  for (const [selector, background, foreground] of [
    [':root', '#111111', '#ffffff'],
    [':root[theme-mode="dark"]', '#f2f2f2', '#0d0d0d'],
  ]) {
    const theme = declarations(tokens, selector!)
    assert.equal(theme['--text'], background)
    assert.equal(theme['--bg'], foreground)
  }
  assert.doesNotMatch(styles, /\.rq-tabs__trigger:not\(:disabled\):hover\s*\{/)
})

test('button labels stay on one line inside containers that wrap long text anywhere', () => {
  const buttonText = declarations(styles, '.rq-button, .rq-button__label')
  assert.equal(buttonText['white-space'], 'nowrap')
  assert.equal(buttonText['overflow-wrap'], 'normal')
})
