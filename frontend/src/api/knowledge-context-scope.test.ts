import assert from 'node:assert/strict'
import test from 'node:test'
import { knowledgeContextBasePath, knowledgeContextScopeParams } from './knowledge-context-scope'

test('default and knowledge base scopes preserve existing context routes', () => {
  assert.equal(knowledgeContextBasePath('kb/a'), '/api/v1/knowledge-bases/kb%2Fa/context')
  assert.equal(knowledgeContextBasePath('kb/a', 'knowledge_base'), '/api/v1/knowledge-bases/kb%2Fa/context')
  assert.deepEqual(knowledgeContextScopeParams('knowledge_base', 'kb1', 'domain1'), { scope: 'knowledge_base', knowledge_base_id: 'kb1' })
})
test('domain queries use team context routes and carry only the domain selector', () => {
  assert.equal(knowledgeContextBasePath('kb1', 'domain'), '/api/v1/knowledge-context')
  assert.deepEqual(knowledgeContextScopeParams('domain', 'kb1', 'domain1'), { scope: 'domain', domain_id: 'domain1' })
})
test('team scope removes knowledge base and domain selectors', () => {
  assert.equal(knowledgeContextBasePath('kb1', 'team'), '/api/v1/knowledge-context')
  assert.deepEqual(knowledgeContextScopeParams('team', 'kb1', 'domain1'), { scope: 'team' })
})
