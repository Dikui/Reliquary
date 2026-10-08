import assert from 'node:assert/strict'
import test from 'node:test'

import { diffTextLines } from './textLineDiff.ts'

function render(lines: ReturnType<typeof diffTextLines>): string {
  return lines
    .map((l) => (l.type === 'same' ? `  ${l.text}` : l.type === 'del' ? `- ${l.text}` : `+ ${l.text}`))
    .join('\n')
}

test('identical texts diff to all-same rows', () => {
  const out = diffTextLines('a\nb\nc', 'a\nb\nc')
  assert.equal(out.length, 3)
  assert.ok(out.every((l) => l.type === 'same'))
})

test('a changed middle line becomes del+add', () => {
  assert.equal(render(diffTextLines('a\nold\nc', 'a\nnew\nc')), '  a\n- old\n+ new\n  c')
})

test('pure insertion and pure deletion', () => {
  assert.equal(render(diffTextLines('a\nc', 'a\nb\nc')), '  a\n+ b\n  c')
  assert.equal(render(diffTextLines('a\nb\nc', 'a\nc')), '  a\n- b\n  c')
})

test('empty sides', () => {
  assert.equal(diffTextLines('', 'x\ny').filter((l) => l.type === 'add').length, 2)
  assert.equal(diffTextLines('x\ny', '').filter((l) => l.type === 'del').length, 2)
  assert.deepEqual(diffTextLines('', ''), [])
})

test('unchanged prefix/suffix stay in order around a block edit', () => {
  const oldText = 'h1\nintro\nbody old 1\nbody old 2\noutro'
  const newText = 'h1\nintro\nbody new\noutro'
  assert.equal(
    render(diffTextLines(oldText, newText)),
    '  h1\n  intro\n- body old 1\n- body old 2\n+ body new\n  outro',
  )
})

test('coarse fallback beyond the LCS limit loses no lines', () => {
  const oldText = Array.from({ length: 2000 }, (_, i) => `o${i}`).join('\n')
  const newText = Array.from({ length: 2000 }, (_, i) => `n${i}`).join('\n')
  const out = diffTextLines(oldText, newText)
  assert.equal(out.filter((l) => l.type === 'del').length, 2000)
  assert.equal(out.filter((l) => l.type === 'add').length, 2000)
})
