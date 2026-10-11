import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { ModuleKind, ScriptTarget, transpileModule } from 'typescript'

test('an invalidated knowledge base request cannot restore the previous account cache', async () => {
  const pending: Array<(value: unknown) => void> = []
  const dependencies = {
    defineStore: (_id: string, setup: () => unknown) => setup,
    ref: (value: unknown) => ({ value }),
    computed: (get: () => unknown) => ({ get value() { return get() } }),
    watch: () => {},
    getCurrentLanguage: () => 'en-US',
    getKnowledgeBase: () => new Promise(resolve => pending.push(resolve)),
  }
  // Execute the actual store setup with controlled API timing, without browser dependencies.
  const source = readFileSync(new URL('./resources.ts', import.meta.url), 'utf8')
  const { outputText } = transpileModule(source, {
    compilerOptions: { module: ModuleKind.CommonJS, target: ScriptTarget.ES2022 },
  })
  const exports: any = {}
  new Function('require', 'exports', outputText)(() => dependencies, exports)
  const store = exports.useResourcesStore()
  const previous = store.fetchKnowledgeBase()
  store.invalidate()
  pending.shift()!({ data: { id: 'previous-account-kb' } })
  assert.equal(await previous, null)
  assert.equal(store.knowledgeBase.value, null)
  assert.equal(store.isFresh('knowledgeBase'), false)

  const current = store.fetchKnowledgeBase()
  pending.shift()!({ data: { id: 'current-account-kb' } })
  assert.deepEqual(await current, { id: 'current-account-kb' })
  assert.deepEqual(await store.fetchKnowledgeBase(), { id: 'current-account-kb' })
  assert.equal(pending.length, 0)
})
