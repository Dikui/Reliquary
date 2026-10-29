import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import ts from 'typescript'
import { createPinia } from 'pinia'

const require = createRequire(import.meta.url)
function storeFactory() {
  const module = { exports: {} as any }
  const source = readFileSync(new URL('./commandPalette.ts', import.meta.url), 'utf8')
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => name === './auth' ? { useAuthStore: () => ({ user: { id: 'user' }, effectiveSpaceId: 1 }) } : require(name), module, module.exports)
  return module.exports.useCommandPaletteStore(createPinia())
}

test('recent destinations preserve reference routes, deduplicate, reload and clear with recent searches', () => {
  const data = new Map<string, string>()
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: (key: string) => data.get(key) || null, setItem: (key: string, value: string) => data.set(key, value), removeItem: (key: string) => data.delete(key) } })
  const store = storeFactory()
  const target = { title: 'Material', path: '/platform/entries/entry-a', query: { reference: '{"paragraph":3}' } }
  store.pushTarget(target); store.pushTarget(target); store.pushRecent('Query')
  assert.equal(store.recentTargets.length, 1)
  const reloaded = storeFactory()
  assert.deepEqual(JSON.parse(JSON.stringify(reloaded.recentTargets)), [target])
  assert.deepEqual([...reloaded.recentQueries], ['Query'])
  reloaded.pushTarget({ title: 'Bad', path: 'https://example.com' })
  assert.equal(reloaded.recentTargets.length, 1)
  reloaded.clearRecent()
  assert.equal(reloaded.recentTargets.length, 0)
  assert.equal(reloaded.recentQueries.length, 0)
  assert.equal(data.size, 0)
})
