import { entryPresentationTestModules } from '../views/library/entryPresentationTestModules'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import ts from 'typescript'
import { ref } from 'vue'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('./useLibrary.ts', import.meta.url), 'utf8')

function createHook(detailsAPI: (id: string) => Promise<unknown>, list: object[] = []) {
  const modules: Record<string, unknown> = {
    pinia: { storeToRefs: () => ({ cardList: ref([]), total: ref(0) }) },
    '../utils/index': { formatStringDate: () => '2026-10-11' },
    'tdesign-vue-next': { MessagePlugin: {} },
    '@/api/library': {
      getEntryDetails: detailsAPI,
      listEntryFiles: async () => ({ success: true, data: list, total: list.length }),
      getEntryDetailsCon: async () => ({ success: true, data: [], total: 0 }),
    },
    '@/stores/entry': { entryStore: () => ({}) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    '@/stores/resources': { useResourcesStore: () => ({}) },
  }
  const output = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)(
    (name: string) => modules[name] ?? entryPresentationTestModules[name] ?? require(name), module, module.exports,
  )
  return module.exports.default('library-vault')
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

test('detail loading ignores retired image-completion fields and clears ingest identity on document switches', async () => {
  const missing = [{ type: 'image', source_url: 'https://example.com/source.png', reason: 'Login required', api_key_name: 'Collector', submitted_at: '2026-10-12T08:00:00Z' }]
  const hook = createHook(async id => ({ success: true, data: { id, updated_at: '2026-10-12T00:00:00Z', ...(id === 'incomplete' ? { acquisition: 'model_written', incomplete: true, missing_materials: missing, external_completion_attempted: true, related_entry_id: 'original' } : {}) } }))
  hook.getCardDetails({ id: 'incomplete' }); await settle()
  assert.equal(hook.details.acquisition, 'model_written')
  assert.equal(Object.hasOwn(hook.details, "incomplete"), false)
  assert.equal(Object.hasOwn(hook.details, "external_completion_attempted"), false)
  assert.equal(hook.details.related_entry_id, 'original')
  assert.equal(Object.hasOwn(hook.details, "missing_materials"), false)
  hook.getCardDetails({ id: 'complete' })
  assert.equal(hook.details.acquisition, undefined)
  assert.equal(Object.hasOwn(hook.details, "incomplete"), false)
  assert.equal(Object.hasOwn(hook.details, "external_completion_attempted"), false)
  assert.equal(hook.details.related_entry_id, '')
  assert.equal(Object.hasOwn(hook.details, "missing_materials"), false)
  await settle()
  assert.equal(hook.details.acquisition, undefined)
  assert.equal(Object.hasOwn(hook.details, "incomplete"), false)
})

test('changing documents clears vault state and an older response cannot restore it', async () => {
  const resolvers = new Map<string, (result: unknown) => void>()
  const hook = createHook(id => new Promise(resolve => { resolvers.set(id, resolve) }))
  hook.getCardDetails({ id: 'old-article' })
  hook.getCardDetails({ id: 'personal' })
  assert.equal(hook.details.source_kind, '')
  assert.equal(hook.details.vault_path, '')
  resolvers.get('personal')!({ success: true, data: {
    id: 'personal', source_kind: 'mine', vault_path: 'personal/notes.md', updated_at: '2026-10-11T00:00:00Z',
  } })
  await settle()
  resolvers.get('old-article')!({ success: true, data: {
    id: 'old-article', source_kind: 'article', vault_path: 'articles/old.md', updated_at: '2026-10-11T00:00:00Z',
  } })
  await settle()
  assert.equal(hook.details.id, 'personal')
  assert.equal(hook.details.source_kind, 'mine')
  assert.equal(hook.details.vault_path, 'personal/notes.md')
})

test('mine and report names display the indexed heading without removing dots', async () => {
  const records = ['mine', 'report'].map(source_kind => ({
    id: source_kind, source_kind, file_name: 'filename.md', title: '版本 1.2 使用说明',
    updated_at: '2026-10-11T00:00:00Z',
  }))
  const hook = createHook(async id => ({ success: true, data: records.find(row => row.id === id) }), records)
  await hook.loadEntries()
  assert.deepEqual(hook.cardList.value.map((row: any) => row.file_name), records.map(row => row.title))
  for (const record of records) {
    hook.getCardDetails(record)
    await settle()
    assert.equal(hook.details.title, record.title)
  }
})

test('same document refresh ignores an older detail response and missing processing fields clear previous values', async () => {
  const replies: ((response: unknown) => void)[] = []
  const hook = createHook(() => new Promise(resolve => replies.push(resolve)))
  hook.getCardDetails({ id: 'same' })
  hook.getCardDetails({ id: 'same' })
  const record = { id: 'same', updated_at: '2026-10-14T00:00:00Z', library_content_types: [{ name: '最新类型', enabled: true }], image_descriptions: [{ ordinal: 1, description: '最新图片说明' }] }
  replies[1]({ success: true, data: record }); await settle()
  replies[0]({ success: true, data: { ...record, library_content_types: [{ name: '旧类型', enabled: true }], image_descriptions: [{ description: '旧说明' }] } }); await settle()
  assert.deepEqual(hook.details.library_content_types, record.library_content_types)
  assert.deepEqual(hook.details.image_descriptions, record.image_descriptions)
  hook.getCardDetails({ id: 'without-images' })
  replies[2]({ success: true, data: { id: 'without-images', updated_at: record.updated_at } }); await settle()
  assert.deepEqual(hook.details.library_content_types, [])
  assert.deepEqual(hook.details.image_descriptions, [])
})

test('old source details retain update links and clearing or switching resets them', async () => {
 const hook=createHook(async id=>({success:true,data:{id,updated_at:'2026-10-14T00:00:00Z',...(id==='old'?{has_update:true,latest_entry_id:'latest'}:{})}}))
 hook.getCardDetails({id:'old'});await settle()
 assert.equal(hook.details.has_update,true);assert.equal(hook.details.latest_entry_id,'latest')
 hook.getCardDetails({id:'latest'})
 assert.equal(hook.details.has_update,false);assert.equal(hook.details.latest_entry_id,'')
 await settle();assert.equal(hook.details.has_update,false)
})


test('closing preview releases its content and ignores an unfinished detail response', async () => {
  let reply: (result: unknown) => void = () => {}
  const hook = createHook(() => new Promise(resolve => { reply = resolve }))
  hook.getCardDetails({ id: 'closing' })
  hook.details.md = [{ content: 'Preview content' }]
  hook.clearCardDetails()
  assert.deepEqual(hook.details.md, [])
  assert.equal(hook.details.id, '')
  reply({ success: true, data: { id: 'closing', title: 'Late response', updated_at: '2026-10-19T00:00:00Z' } })
  await settle()
  assert.equal(hook.details.id, '')
  assert.deepEqual(hook.details.md, [])
})
