import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'

test('report downloads preserve the Markdown URL and request JSON explicitly', async () => {
  const requested: string[] = []
  const module = { exports: {} as any }
  const source = readFileSync(new URL('./knowledge-workflows.ts', import.meta.url), 'utf8')
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', compiled)(
    (name: string) => {
      assert.equal(name, '@/utils/request')
      return { getDown: (url: string) => { requested.push(url); return Promise.resolve(new Blob(['report'])) } }
    }, module, module.exports,
  )
  await module.exports.downloadKnowledgeWorkflowReport('kb/1', 'report?1')
  await module.exports.downloadKnowledgeWorkflowReport('kb/1', 'report?1', 'markdown')
  await module.exports.downloadKnowledgeWorkflowReport('kb/1', 'report?1', 'json')
  assert.deepEqual(requested, [
    '/api/v1/knowledge-bases/kb%2F1/reports/report%3F1/download',
    '/api/v1/knowledge-bases/kb%2F1/reports/report%3F1/download',
    '/api/v1/knowledge-bases/kb%2F1/reports/report%3F1/download?format=json',
  ])
})
