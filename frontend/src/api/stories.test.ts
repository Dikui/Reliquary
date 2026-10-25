import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

test('manual corrections identify the donor and retained target explicitly', async () => {
  const requests: unknown[] = [], module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL('./stories.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
  new Function('require', 'module', 'exports', code)(() => Object.fromEntries(['get', 'post'].map(method => [method, (url: string, body?: unknown) => {
    requests.push({ method, url, ...(body === undefined ? {} : { body }) }); return Promise.resolve({ success: true })
  }])), module, module.exports)
  const api = module.exports
  await api.moveFact('fact/a', 'story-b')
  await api.splitFact('fact/a', 'New story')
  await api.mergeFacts('fact/a', 'kept-fact')
  await api.mergeStories('story/a', 'kept-story')
  await api.getFact('fact/a')
  assert.deepEqual(requests, [
    { method: 'post', url: '/api/v1/facts/fact%2Fa/move', body: { story_id: 'story-b' } },
    { method: 'post', url: '/api/v1/facts/fact%2Fa/split', body: { title: 'New story' } },
    { method: 'post', url: '/api/v1/facts/fact%2Fa/merge', body: { keep_fact_id: 'kept-fact' } },
    { method: 'post', url: '/api/v1/stories/story%2Fa/merge', body: { keep_story_id: 'kept-story' } },
    { method: 'get', url: '/api/v1/facts/fact%2Fa' },
  ])
})
