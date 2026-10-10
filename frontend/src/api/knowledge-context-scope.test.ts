import assert from 'node:assert/strict'
import test from 'node:test'
import { knowledgeContextBasePath, knowledgeContextScopeParams, normalizeKnowledgeContextScope, type KnowledgeContextScopeInput } from './knowledge-context-scope'

test('default and knowledge base scopes preserve existing context routes', () => {
  assert.equal(knowledgeContextBasePath('kb/a'), '/api/v1/knowledge-bases/kb%2Fa/context')
  assert.equal(knowledgeContextBasePath('kb/a', 'knowledge_base'), '/api/v1/knowledge-bases/kb%2Fa/context')
  assert.deepEqual(knowledgeContextScopeParams('knowledge_base', 'kb1', 'domain1'), { scope: 'knowledge_base', knowledge_base_id: 'kb1' })
})
test('domain queries use global context routes and carry only the domain selector', () => {
  assert.equal(knowledgeContextBasePath('kb1', 'domain'), '/api/v1/knowledge-context')
  assert.deepEqual(knowledgeContextScopeParams('domain', 'kb1', 'domain1'), { scope: 'domain', domain_id: 'domain1' })
})
test('all scope removes knowledge base and domain selectors', () => {
  assert.deepEqual(knowledgeContextScopeParams('all', 'kb1', 'domain1'), { scope: 'all' })
  assert.equal(knowledgeContextBasePath('kb1', 'all'), '/api/v1/knowledge-context')
})
test('the removed team scope returns a parameter error', () => {
  const legacyScope = 'team' as KnowledgeContextScopeInput
  const error = /scope must be knowledge_base, domain or all/
  assert.throws(() => normalizeKnowledgeContextScope(legacyScope), error)
  assert.throws(() => knowledgeContextBasePath('kb1', legacyScope), error)
  assert.throws(() => knowledgeContextScopeParams(legacyScope, 'kb1', 'domain1'), error)
})
