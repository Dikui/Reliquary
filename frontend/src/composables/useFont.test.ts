import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { MONO_STACKS, SANS_STACKS } from './useFont'

test('system font stacks equal the token defaults that useFont overrides', () => {
  const tokens = readFileSync(new URL('../styles/tokens.css', import.meta.url), 'utf8')
  const token = (name: string) => tokens.match(new RegExp(`--${name}: ([^;]+);`))?.[1]
  assert.equal(SANS_STACKS.system, token('font'))
  assert.equal(MONO_STACKS.system, token('mono'))
})
