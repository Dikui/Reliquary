import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { ModuleKind, ScriptTarget, transpileModule } from 'typescript'

test('an invalidated library request cannot restore the previous account cache', async () => {
  const pending: Array<(value: unknown) => void> = []
  const dependencies = {
    defineStore: (_id: string, setup: () => unknown) => setup,
    ref: (value: unknown) => ({ value }),
    computed: (get: () => unknown) => ({ get value() { return get() } }),
    watch: () => {},
    getCurrentLanguage: () => 'en-US',
    getLibrary: () => new Promise(resolve => pending.push(resolve)),
  }
  // Execute the actual store setup with controlled API timing, without browser dependencies.
  const source = readFileSync(new URL('./resources.ts', import.meta.url), 'utf8')
  const { outputText } = transpileModule(source, {
    compilerOptions: { module: ModuleKind.CommonJS, target: ScriptTarget.ES2022 },
  })
  const exports: any = {}
  new Function('require', 'exports', outputText)(() => dependencies, exports)
  const store = exports.useResourcesStore()
  const previous = store.fetchLibrary()
  store.invalidate()
  pending.shift()!({ data: { id: 'previous-account-library' } })
  assert.equal(await previous, null)
  assert.equal(store.library.value, null)
  assert.equal(store.isFresh('library'), false)

  const current = store.fetchLibrary()
  pending.shift()!({ data: { id: 'current-account-library' } })
  assert.deepEqual(await current, { id: 'current-account-library' })
  assert.deepEqual(await store.fetchLibrary(), { id: 'current-account-library' })
  assert.equal(pending.length, 0)
})
