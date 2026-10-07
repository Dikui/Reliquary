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
    listKnowledgeBases: () => new Promise(resolve => pending.push(resolve)),
  }
  // Execute the actual store setup with controlled API timing, without browser dependencies.
  const source = readFileSync(new URL('./chatResources.ts', import.meta.url), 'utf8')
  const { outputText } = transpileModule(source, {
    compilerOptions: { module: ModuleKind.CommonJS, target: ScriptTarget.ES2022 },
  })
  const exports: any = {}
  new Function('require', 'exports', outputText)(() => dependencies, exports)
  const store = exports.useChatResourcesStore()
  const previous = store.fetchKnowledgeBasesForList()
  store.invalidate()
  pending.shift()!({ data: [{ id: 'previous-account-kb' }] })
  assert.deepEqual(await previous, [])
  assert.deepEqual(store.rawKnowledgeBases.value, [])
  assert.equal(store.isFresh('knowledgeBases'), false)

  const current = store.fetchKnowledgeBasesForList()
  pending.shift()!({ data: [{ id: 'current-account-kb' }] })
  assert.deepEqual(await current, [{ id: 'current-account-kb' }])
  assert.deepEqual(await store.fetchKnowledgeBasesForList(), [{ id: 'current-account-kb' }])
  assert.equal(pending.length, 0)
})
