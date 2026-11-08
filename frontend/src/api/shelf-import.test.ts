import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

// Small spec, task 19 / ADR 0023: replace only HTTP transport and inspect the public wire contract.
function load() {
  const requests: { method: string; url: string; data?: any }[] = [], module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL('./shelf-import.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => {
    assert.equal(name, '@/utils/request')
    return Object.fromEntries(['get', 'post', 'postUpload'].map(method => [method, async (url: string, data?: unknown) => { requests.push({ method, url, data }); return { success: true } }]))
  }, module, module.exports)
  return { api: module.exports, requests }
}
test('workbook uploads carry the chosen confirmed plan and optional file date as multipart', async () => {
  const { api, requests } = load(), file = new File(['synthetic'], 'source.xlsx')
  await api.uploadShelfImport('library/a', file, 'map', '2026-10-28T00:00:00Z')
  await api.uploadShelfImport('library/a', file, 'map', '')
  assert.equal(requests[0]!.url, '/api/v1/libraries/library%2Fa/shelf/imports')
  const data = requests[0]!.data as FormData
  assert.equal(data.get('file'), file)
  assert.equal(data.get('plan_key'), 'map')
  assert.equal(data.get('file_date'), '2026-10-28T00:00:00Z')
  assert.equal(requests[1]!.data.has('file_date'), false)
})
test('first-time model requests carry only the file, plan key and explicit file kind', async () => {
  const { api, requests } = load(), file = new File(['synthetic'], 'source.xlsx')
  await api.suggestShelfImportPlan('library/a', file, 'new_map', 'certification_report')
  assert.equal(requests[0]!.url, '/api/v1/libraries/library%2Fa/shelf/import-plans/suggest')
  assert.deepEqual(Array.from(requests[0]!.data.entries()), [['file', file], ['plan_key', 'new_map'], ['file_kind', 'certification_report']])
})
test('plan saving confirms one changeset with the current plan revision', async () => {
  const { api, requests } = load()
  const plan = { key: 'map', name: ' Original ', revision: 7, file_kind: 'product_map', sheets: [] }
  await api.saveShelfImportPlan('library/a', plan)
  assert.deepEqual(requests[0], { method: 'post', url: '/api/v1/libraries/library%2Fa/shelf/changesets', data: { source: { type: 'web' }, confirmed: true, operations: [{ id: 'plan', type: 'save_import_plan', import_plan: plan }], decisions: [{ item_id: 'plan', action: 'apply' }] } })
})
test('batch history filters imports before paging', async () => {
  const { api, requests } = load()
  await api.listShelfImportBatches('library/a', 2)
  assert.equal(requests[0]!.url, '/api/v1/libraries/library%2Fa/shelf/changesets?source_type=import&page=2&page_size=20')
})
