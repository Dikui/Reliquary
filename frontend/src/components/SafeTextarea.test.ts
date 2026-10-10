import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import test from 'node:test'

function probe(mode: string) {
  const result = spawnSync(process.execPath, ['--import', 'tsx', new URL('./__tests__/textareaMountProbe.mjs', import.meta.url).pathname, mode], { encoding: 'utf8' })
  assert.equal(result.status, 0, result.stdout + result.stderr)
  return JSON.parse(result.stdout.trim().split('\n').at(-1)!)
}

test('safe autosize cancels measurements when its parent removes it during mount', () => {
  assert.equal(probe('wrapper').invalidMeasurements, 0)
})

test('safe autosize preserves row limits, input events, focus methods and visible-pane resizing', () => {
  const summary = probe('behavior')
  assert.deepEqual(summary.heights, ['48px', '96px'])
  assert.equal(summary.changeEvents, 1)
  assert.equal(summary.focus, 1)
  assert.equal(summary.blur, 1)
  assert.equal(summary.resizeObserversAfterUnmount, 0)
})

test('TDesign does not clip overflowing textarea content at its row limit', () => {
  const css = readFileSync(new URL('../../node_modules/tdesign-vue-next/es/textarea/style/index.css', import.meta.url), 'utf8')
  const inner = css.match(/\.t-textarea__inner\s*\{([^}]+)\}/)?.[1]
  assert.ok(inner)
  assert.doesNotMatch(inner, /overflow(?:-y)?:\s*(?:hidden|clip)/)
})


test('clearing, IME composition and external edits preserve the model without readonly warnings or replacing the DOM node', () => {
  assert.equal(probe('readonly').readonlyWarnings, 0)
})
