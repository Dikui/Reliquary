import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import ts from 'typescript'
import { ref } from 'vue'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('./useKnowledgeBase.ts', import.meta.url), 'utf8')

function createHook(detailsAPI: (id: string) => Promise<unknown>, list: object[] = []) {
  const modules: Record<string, unknown> = {
    pinia: { storeToRefs: () => ({ cardList: ref([]), total: ref(0) }) },
    '../utils/index': { formatStringDate: () => '2026-10-11' },
    'tdesign-vue-next': { MessagePlugin: {} },
    '@/api/knowledge-base': {
      getKnowledgeDetails: detailsAPI,
      listKnowledgeFiles: async () => ({ data: list, total: list.length }),
      getKnowledgeDetailsCon: async () => ({ success: true, data: [], total: 0 }),
    },
    '@/stores/knowledge': { knowledgeStore: () => ({}) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    '@/stores/resources': { useResourcesStore: () => ({}) },
    './useKnowledgeDuplicates': { useKnowledgeDuplicates: () => ({}) },
  }
  const output = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)(
    (name: string) => modules[name] ?? require(name), module, module.exports,
  )
  return module.exports.default('kb-vault')
}

const settle = () => new Promise<void>(resolve => setImmediate(resolve))

test('article details retain vault source and path from the actual API loading hook', async () => {
  const hook = createHook(async id => ({ success: true, data: {
    id, source_kind: 'article', vault_path: 'articles/2026-10/中文-12345678.md',
    updated_at: '2026-10-11T00:00:00Z',
  } }))
  hook.getCardDetails({ id: 'article' })
  await settle()
  assert.equal(hook.details.source_kind, 'article')
  assert.equal(hook.details.vault_path, 'articles/2026-10/中文-12345678.md')
})

test('changing documents clears vault state and an older response cannot restore it', async () => {
  const resolvers = new Map<string, (result: unknown) => void>()
  const hook = createHook(id => new Promise(resolve => { resolvers.set(id, resolve) }))
  hook.getCardDetails({ id: 'old-article' })
  hook.getCardDetails({ id: 'personal' })
  assert.equal(hook.details.source_kind, '')
  assert.equal(hook.details.vault_path, '')
  resolvers.get('personal')!({ success: true, data: {
    id: 'personal', source_kind: 'mine', vault_path: 'mine/notes.md', updated_at: '2026-10-11T00:00:00Z',
  } })
  await settle()
  resolvers.get('old-article')!({ success: true, data: {
    id: 'old-article', source_kind: 'article', vault_path: 'articles/old.md', updated_at: '2026-10-11T00:00:00Z',
  } })
  await settle()
  assert.equal(hook.details.id, 'personal')
  assert.equal(hook.details.source_kind, 'mine')
  assert.equal(hook.details.vault_path, 'mine/notes.md')
})

test('mine and report names display the indexed heading without removing dots', async () => {
  const records = ['mine', 'report'].map(source_kind => ({
    id: source_kind, source_kind, file_name: 'filename.md', title: '版本 1.2 使用说明',
    updated_at: '2026-10-11T00:00:00Z',
  }))
  const hook = createHook(async id => ({ success: true, data: records.find(row => row.id === id) }), records)
  await hook.getKnowled()
  assert.deepEqual(hook.cardList.value.map((row: any) => row.file_name), records.map(row => row.title))
  for (const record of records) {
    hook.getCardDetails(record)
    await settle()
    assert.equal(hook.details.title, record.title)
  }
})
