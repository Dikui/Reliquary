import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { transpileModule, ModuleKind } from 'typescript'
function load(storage: Record<string, string>) {
  globalThis.localStorage = { getItem: key => storage[key] ?? null } as Storage
  const code = transpileModule(readFileSync(new URL('./settingsStorage.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ModuleKind.CommonJS } }).outputText
  const exports = {} as typeof import('./settingsStorage')
  new Function('require', 'exports', code)(() => ({ safeRemoveItem: (key: string) => { delete storage[key] } }), exports)
  return exports
}
const defaults = () => ({ ollamaConfig: { baseUrl: 'http://localhost:11434', enabled: true } })
test('settings object validation rejects non-object JSON values', () => {
  const api = load({})
  for (const value of [null, [], 'x']) assert.equal(api.isStoredSettingsRecord(value), false)
  assert.equal(api.isStoredSettingsRecord({}), true)
})
test('defaults are deep cloned and missing storage does not write anything', () => {
  const storage = {}, api = load(storage), original = defaults(), value = api.loadAndReconcileSettings(original)
  value.ollamaConfig.baseUrl = 'https://models.example'
  assert.equal(original.ollamaConfig.baseUrl, 'http://localhost:11434')
  assert.deepEqual(storage, {})
})
for (const invalid of ['{broken', 'null', '[]']) test(`corrupted settings reset and clear storage for ${invalid}`, () => {
  const storage = { Reliquary_settings: invalid }, api = load(storage)
  assert.deepEqual(api.loadAndReconcileSettings(defaults()), defaults())
  assert.deepEqual(storage, {})
})
test('valid settings retain the configured Ollama endpoint and discard retired keys', () => {
  const storage = { Reliquary_settings: JSON.stringify({ ollamaConfig: { baseUrl: 'http://models.example', enabled: true }, retiredSetting: true }) }
  const value = load(storage).loadAndReconcileSettings(defaults())
  assert.deepEqual(value, { ollamaConfig: { baseUrl: 'http://models.example', enabled: true } })
})
