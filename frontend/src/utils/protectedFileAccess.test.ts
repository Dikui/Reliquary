import assert from 'node:assert/strict'
import test from 'node:test'

import {
  buildProtectedFileRequest,
  isProtectedFileProxyPath,
  isProviderFileURL,
  resolveProtectedFileAccess,
  setDefaultProtectedFileAccess,
  type ProtectedFileAccessContext,
} from './protectedFileAccess.ts'

const RESOURCE = 'resource://AbCdEfGhIjKlMnOpQrStUv'

test.afterEach(() => {
  setDefaultProtectedFileAccess(null)
})

test('space access routes through the space-scoped proxy', () => {
  const request = buildProtectedFileRequest(RESOURCE, { mode: 'space' })

  assert.equal(request?.url, `/files?file_path=${encodeURIComponent(RESOURCE)}`)
})

test('library access routes through the Library-scoped proxy', () => {
  const request = buildProtectedFileRequest(RESOURCE, { mode: 'library', libraryId: 'library 1' })

  assert.equal(
    request?.url,
    `/api/v1/libraries/library%201/files?file_path=${encodeURIComponent(RESOURCE)}`,
  )
})


test('non-storage sources are not proxied', () => {
  assert.equal(buildProtectedFileRequest('https://example.com/a.png', { mode: 'space' }), null)
  assert.equal(isProviderFileURL('https://example.com/a.png'), false)
  assert.equal(isProviderFileURL('local://42/a.png'), true)
})

test('a component scope refines the default space plane', () => {
  const override: ProtectedFileAccessContext = { mode: 'library', libraryId: 'library-1' }

  assert.deepEqual(resolveProtectedFileAccess(override), override)
  assert.deepEqual(resolveProtectedFileAccess(), { mode: 'space' })
  assert.deepEqual(
    resolveProtectedFileAccess({ mode: 'library', libraryId: '  ' }),
    { mode: 'space' },
  )
})

test('all file proxies are recognized as protected proxy paths', () => {
  assert.equal(isProtectedFileProxyPath('/files'), true)
  assert.equal(isProtectedFileProxyPath('/api/v1/libraries/library-1/files'), true)
  assert.equal(isProtectedFileProxyPath('/api/v1/entries/article-id/vault-attachment'), true)
})

test('vault attachments use the authenticated article proxy with their relative path', () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    getItem: () => 'vault-secret-token',
  } })
  try {
    const relative = '../../attachments/article-id/图表.png'
    const source = `vault://article-id/${encodeURIComponent(relative)}`
    assert.equal(isProviderFileURL(source), true)
    for (const access of [{ mode: 'space' }, { mode: 'library', libraryId: 'library-1' }] as ProtectedFileAccessContext[]) {
      const request = buildProtectedFileRequest(source, access)!
      const url = new URL(request.url, 'http://localhost')
      assert.equal(url.pathname, '/api/v1/entries/article-id/vault-attachment')
      assert.equal(url.searchParams.get('path'), relative)
      assert.deepEqual(request.headers, { Authorization: 'Bearer vault-secret-token' })
      assert.doesNotMatch(request.url, /vault-secret-token|token=/)
    }
    for (const invalid of ['vault://article-id/', 'vault://article-id/%E0%A4', 'vault://missing-id']) {
      assert.equal(buildProtectedFileRequest(invalid, { mode: 'space' }), null, invalid)
    }
  } finally {
    if (previous) Object.defineProperty(globalThis, 'localStorage', previous)
    else Reflect.deleteProperty(globalThis, 'localStorage')
  }
})
