import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import 'vue'

test('Swiss variables follow light, dark and system preferences through theme-mode', async () => {
  const attributes = new Map<string, string>()
  const storage = new Map<string, string>()
  const listeners = new Set<() => void>()
  let dark = true
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
  } })
  Object.defineProperty(globalThis, 'document', { configurable: true, value: {
    documentElement: { setAttribute: (key: string, value: string) => attributes.set(key, value) },
  } })
  Object.defineProperty(globalThis, 'window', { configurable: true, value: {
    matchMedia: () => ({ get matches() { return dark }, addEventListener: (_: string, listener: () => void) => listeners.add(listener) }),
  } })
  const { initTheme, useTheme } = await import('./useTheme.ts')
  initTheme()
  const { setTheme } = useTheme()
  const css = readFileSync(new URL('../styles/tokens.css', import.meta.url), 'utf8')
  const background = () => {
    const selector = attributes.get('theme-mode') === 'dark' ? ':root[theme-mode="dark"]' : ':root'
    assert.ok(css.includes(selector), `Missing theme selector: ${selector}`)
    const block = css.slice(css.indexOf(selector) + selector.length).match(/\{([^}]+)\}/)![1]!
    return block.match(/--bg:\s*([^;]+);/)![1]
  }
  assert.equal(background(), '#ffffff')
  setTheme('dark')
  assert.equal(attributes.get('theme-mode'), 'dark')
  assert.equal(background(), '#0d0d0d')
  setTheme('light')
  assert.equal(background(), '#ffffff', 'explicit light wins over a dark operating system')
  setTheme('system')
  assert.equal(background(), '#0d0d0d')
  dark = false
  for (const listener of listeners) listener()
  assert.equal(background(), '#ffffff')
  assert.equal(storage.get('Reliquary_anon_theme'), 'system')
  assert.equal(setTheme('invalid' as 'light'), false)
})

test('theme palettes preserve focus and library colors without introducing element resets', () => {
  const migrated = readFileSync(new URL('../styles/tokens.css', import.meta.url), 'utf8')
  const declarations = (css: string, selector: string) => {
    const start = css.indexOf(selector)
    assert.ok(start >= 0)
    const body = css.slice(start + selector.length).match(/\{([^}]+)\}/)![1]!
    return Object.fromEntries([...body.matchAll(/(--[\w-]+):\s*([^;]+);/g)].map(([, key, value]) => [key, value]))
  }
  const light = declarations(migrated, ':root')
  const dark = { ...light, ...declarations(migrated, ':root[theme-mode="dark"]') }
  assert.equal(light['--surface-soft'], '#f2f2f2')
  assert.equal(dark['--surface-soft'], '#1f1f1f')
  assert.equal(light['--select'], '#ffcc00')
  assert.equal(dark['--select'], '#ffcc00')
  assert.equal(dark['--on-select'], '#111111')
  assert.deepEqual(Array.from({ length: 8 }, (_, index) => light[`--lib-${index + 1}`]),
    ['#e2231a', '#0050b5', '#ffcc00', '#111111', '#00843d', '#ff6a13', '#6d2c91', '#8a8a8a'])
  assert.deepEqual(Array.from({ length: 8 }, (_, index) => dark[`--lib-${index + 1}`]),
    ['#ff3b30', '#4d8eff', '#ffcc00', '#f2f2f2', '#2fb36b', '#ff8a3d', '#a970d6', '#8a8a8a'])
  assert.equal((migrated.match(/\{/g) ?? []).length, 2, 'only root variable declarations are global')
})
