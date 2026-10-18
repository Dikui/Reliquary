import * as libraryScopeHelpers from '../utils/libraryScope'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test, { type TestContext } from 'node:test'
import axios, { type InternalAxiosRequestConfig } from 'axios'
import ts from 'typescript'
import { translatePersonalServerMessage } from '../utils/personalServerError'

const require = createRequire(import.meta.url)
function evaluateFile(path: string, modules: Record<string, unknown>) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8')
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', code)((name: string) => (name === '@/utils/libraryScope' ? libraryScopeHelpers : modules[name]) ?? require(name), module, module.exports)
  return module.exports
}

function fixture(t: TestContext, results: unknown[]) {
  const storage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: () => null } })
  const calls: InternalAxiosRequestConfig[] = []
  const network = { __esModule: true, default: { create: (options: object) => axios.create({ ...options, adapter: async (config: InternalAxiosRequestConfig) => {
    calls.push(config)
    return { data: { success: true, data: config.url === "/api/v1/libraries" ? [{ id: "all-a" }, { id: "all-b" }] : { results: structuredClone(results), excluded_missing_published_at: 0 } }, status: 200, statusText: 'OK', headers: {}, config }
  } }) } }
  const request = evaluateFile('../utils/request.ts', {
    axios: network,
    './index': { generateRandomString: () => 'search-fixture', MAX_FILE_SIZE_MB: 100 },
    './personalServerError': { translatePersonalServerMessage },
    './api-base': { getApiBaseUrl: () => 'http://search.test' },
    '@/i18n': { __esModule: true, default: { global: { locale: { value: 'zh-CN' }, t: (key: string) => key } } },
  })
  t.after(() => {
    request.stopServiceAvailabilityRetry()
    if (storage) Object.defineProperty(globalThis, 'localStorage', storage)
    else delete (globalThis as any).localStorage
  })
  return { calls, search: evaluateFile('./search.ts', { '@/utils/request': request }), follows: evaluateFile('./follows.ts', { '@/utils/request': request }) }
}

const article = (id: string, fields = {}) => ({ entry_id: id, source: 'article', acquisition: 'model_written', similar_reports: [], ...fields })

for (const kind of ['search', 'follow feed'] as const) {
  test(`${kind} uses incomplete fields from HTTP responses without article detail requests`, async t => {
    const nested = [article('nested-incomplete', { incomplete: true }), article('nested-complete', { incomplete: false }), article('nested-missing')]
    const results = [article('incomplete', { incomplete: true, similar_reports: nested }), article('complete', { incomplete: false }), article('missing')]
    const f = fixture(t, results)
    const request = kind === 'search' ? { query: 'Launch' } : { limit: 20, collapse_similar: true }
    const response = kind === 'search' ? await f.search.searchEntries(request) : await f.follows.getFollowFeed(request)
    assert.equal(response.success, true)
    assert.deepEqual(response.data.results, results)
    for (const rows of [response.data.results, response.data.results[0].similar_reports]) {
      assert.equal(rows[0].incomplete, true)
      assert.equal(rows[1].incomplete, false)
      assert.equal(rows[2].incomplete, undefined)
      assert.equal(Object.hasOwn(rows[2], 'incomplete'), false)
    }
    assert.deepEqual(f.calls.map(call => ({ method: call.method, url: call.url, body: call.data ? JSON.parse(call.data) : undefined })), [
      ...(kind === 'search' ? [{ method: 'get', url: '/api/v1/libraries', body: undefined }] : []),
      { method: 'post', url: kind === 'search' ? '/api/v1/search' : '/api/v1/follows/feed', body: kind === 'search' ? { ...request, libraries: ['all-a', 'all-b'] } : request },
    ])
  })
}

test('article search and filter options use the shared HTTP service routes', async () => {
  const calls: any[] = []
  const module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL('./search.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => {
    if (name === '@/utils/libraryScope') return libraryScopeHelpers
    assert.equal(name, '@/utils/request')
    return Object.fromEntries(['get', 'post'].map(method => [method, (url: string, body?: unknown) => { calls.push({ method, url, body }); return Promise.resolve({ success: true }) }]))
  }, module, module.exports)
  const request = { libraries: ['selected'], query: 'Launch', content_types: ['event'], entity_ids: ['e1'], topic_ids: ['t1'], published_from: '2026-10-01', collapse_similar: false, limit: 20 }
  await module.exports.searchEntries(request)
  await module.exports.getSearchOptions()
  assert.deepEqual(calls, [{ method: 'post', url: '/api/v1/search', body: request }, { method: 'get', url: '/api/v1/search/options', body: undefined }])
})


test('web search reloads all libraries for every request and preserves an explicit selection', async t => {
  const f = fixture(t, [])
  await f.search.searchEntries({ query: 'first' })
  await f.search.searchEntries({ query: 'second' })
  await f.search.searchEntries({ query: 'third', libraries: ['selected'] })
  assert.deepEqual(f.calls.map(c => c.url), ['/api/v1/libraries', '/api/v1/search', '/api/v1/libraries', '/api/v1/search', '/api/v1/search'])
  assert.deepEqual(JSON.parse(f.calls[1].data).libraries, ['all-a', 'all-b'])
  assert.deepEqual(JSON.parse(f.calls[4].data).libraries, ['selected'])
})

test('a failed library catalog prevents an unscoped web search', async () => {
  let posted = false
  const api = evaluateFile('./search.ts', { '@/utils/request': {
    get: async () => { throw new Error('catalog unavailable') },
    post: async () => { posted = true },
  } })
  await assert.rejects(api.searchEntries({ query: 'query' }), /catalog unavailable/)
  assert.equal(posted, false)
})

test('web search sends the remembered library range and an explicit all-library selection overrides it', async t => {
  const f = fixture(t, [])
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: (key: string) => key === libraryScopeHelpers.LIBRARY_SCOPE_KEY ? '["all-b"]' : null } })
  await f.search.searchEntries({ query: 'remembered' })
  assert.deepEqual(JSON.parse(f.calls[1].data).libraries, ['all-b'])
  await f.search.searchEntries({ query: 'all', libraries: [] })
  assert.deepEqual(JSON.parse(f.calls[3].data).libraries, ['all-a', 'all-b'])
})
