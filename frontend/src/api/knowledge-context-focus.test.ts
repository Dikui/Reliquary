import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'
import * as scope from './knowledge-context-scope'

test('focus requests preserve all three scopes and stars use their dedicated encoded resources', async () => {
  const requested: Array<{ method: string; url: string }> = []
  const module = { exports: {} as any }
  const source = readFileSync(new URL('./knowledge-context.ts', import.meta.url), 'utf8')
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', compiled)(
    (name: string) => {
      if (name === './knowledge-context-scope') return scope
      assert.equal(name, '@/utils/request')
      return Object.fromEntries(['get', 'put', 'del'].map(method => [method, (url: string) => {
        requested.push({ method, url })
        return Promise.resolve({ success: true })
      }]))
    }, module, module.exports,
  )
  for (const queryScope of ['knowledge_base', 'domain', 'all'] as const) {
    await module.exports.getKnowledgeFocusScope(scope.knowledgeContextScopeParams(queryScope, 'kb/1', 'domain/1'))
  }
  await module.exports.starKnowledgeEntity('entity/1')
  await module.exports.unstarKnowledgeEntity('entity/1')
  await module.exports.starKnowledgeTopic('tag?1')
  await module.exports.unstarKnowledgeTopic('tag?1')
  assert.deepEqual(requested, [
    { method: 'get', url: '/api/v1/knowledge-context/focus?scope=knowledge_base&knowledge_base_id=kb%2F1' },
    { method: 'get', url: '/api/v1/knowledge-context/focus?scope=domain&domain_id=domain%2F1' },
    { method: 'get', url: '/api/v1/knowledge-context/focus?scope=all' },
    { method: 'put', url: '/api/v1/knowledge-context/entities/entity%2F1/star' },
    { method: 'del', url: '/api/v1/knowledge-context/entities/entity%2F1/star' },
    { method: 'put', url: '/api/v1/knowledge-context/topics/tag%3F1/star' },
    { method: 'del', url: '/api/v1/knowledge-context/topics/tag%3F1/star' },
  ])
})
