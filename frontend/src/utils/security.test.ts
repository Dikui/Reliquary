import assert from 'node:assert/strict'
import test from 'node:test'

import { createSafeImage, hydrateProtectedFileImages, isValidImageURL, protectProviderImageSrcInHTML } from './security.ts'
import { domPurifySecurityHooks } from './markdownDomPurify.ts'

test('protectProviderImageSrcInHTML uses a placeholder src for provider images', () => {
  const html = '<p><img alt="preview" src="local://10000/exports/a.jpg"></p>'
  const sanitized = protectProviderImageSrcInHTML(html)
  const renderedSrc = sanitized.match(/<img[^>]*\ssrc="([^"]+)"/)?.[1]

  assert.match(renderedSrc || '', /^data:image\/gif;base64,/)
  assert.match(sanitized, /data-protected-src="local:\/\/10000\/exports\/a\.jpg"/)
})

test('protectProviderImageSrcInHTML uses a placeholder src for resource references', () => {
  const html = '<p><img alt="preview" src="resource://AbCdEfGhIjKlMnOpQrStUv"></p>'
  const sanitized = protectProviderImageSrcInHTML(html)
  const renderedSrc = sanitized.match(/<img[^>]*\ssrc="([^"]+)"/)?.[1]

  assert.match(renderedSrc || '', /^data:image\/gif;base64,/)
  assert.match(
    sanitized,
    /data-protected-src="resource:\/\/AbCdEfGhIjKlMnOpQrStUv"/,
  )
})

test('vault image references are protected and hydrated through the authenticated article endpoint', async () => {
  const relative = '../../attachments/vault-hydration-id/图表.png'
  const source = `vault://vault-hydration-id/${encodeURIComponent(relative)}`
  assert.equal(isValidImageURL(source), true)
  const protectedHTML = protectProviderImageSrcInHTML(`<img src="${source}" alt="Chart">`)
  assert.match(protectedHTML, /src="data:image\/gif;base64,/)
  assert.ok(protectedHTML.includes(`data-protected-src="${source}"`))

  const previous = new Map<string, PropertyDescriptor | undefined>()
  const requests: { url: string; options: RequestInit }[] = []
  const attributes = new Map([['src', source]])
  const image = {
    dataset: {} as Record<string, string>, src: source,
    getAttribute: (key: string) => attributes.get(key) ?? null,
    setAttribute: (key: string, value: string) => { attributes.set(key, value) },
    removeAttribute: (key: string) => { attributes.delete(key) },
  }
  const selectors: string[] = []
  const root = { querySelectorAll: (selector: string) => { selectors.push(selector); return [image] } }
  for (const [key, value] of Object.entries({
    window: { location: { origin: 'http://localhost' } },
    localStorage: { getItem: () => 'vault-hydration-token' },
    fetch: async (url: string, options: RequestInit) => {
      requests.push({ url, options })
      return { ok: true, blob: async () => new Blob(['image fixture'], { type: 'image/png' }) }
    },
  })) {
    previous.set(key, Object.getOwnPropertyDescriptor(globalThis, key))
    Object.defineProperty(globalThis, key, { configurable: true, value })
  }
  try {
    await hydrateProtectedFileImages(root as unknown as ParentNode, { mode: 'library', libraryId: 'library-a' })
    assert.equal(requests.length, 1)
    const request = requests[0]
    const url = new URL(request.url, 'http://localhost')
    assert.equal(url.pathname, '/api/v1/entries/vault-hydration-id/vault-attachment')
    assert.equal(url.searchParams.get('path'), relative)
    assert.deepEqual(request.options.headers, {})
    assert.equal(request.options.credentials, 'include')
    assert.doesNotMatch(request.url, /vault-hydration-token|token=/)
    assert.equal(image.dataset.authHydrated, '1')
    assert.match(image.src, /^blob:/)
    assert.ok(selectors.some(selector => selector.includes('img[src^="vault://"]')))
    URL.revokeObjectURL(image.src)
  } finally {
    for (const [key, descriptor] of previous) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor)
      else Reflect.deleteProperty(globalThis, key)
    }
  }
})

test('historical remote images retain their image element and source URL', () => {
  const source = 'https://source.example/chart.png?size=2&version=1'
  assert.equal(isValidImageURL(source), true)
  assert.equal(createSafeImage(source, 'Chart', 'Original image'), `<img src="${source}" alt="Chart" title="Original image" class="markdown-image" style="max-width: 100%; height: auto;">`)
  const html = `<img src="${source}" srcset="https://source.example/large.png 2x"><svg><image href="https://source.example/vector.svg"/><path fill="url(https://source.example/fill.svg#pattern)"/></svg><p style="background: image-set('https://source.example/background.png' 1x)">Body</p>`
  assert.equal(protectProviderImageSrcInHTML(html), html)
})

test('existing HTML safety hooks preserve remote resource attributes while removing event handlers', () => {
  const original = { src: 'https://source.example/image.png', srcset: 'https://source.example/large.png 2x', style: 'background: url(https://source.example/background.png)', onerror: 'unsafe()' }
  const attributes = new Map(Object.entries(original))
  const element = { tagName: 'IMG', hasAttribute: (name: string) => attributes.has(name), getAttribute: (name: string) => attributes.get(name) || null, removeAttribute: (name: string) => attributes.delete(name), setAttribute: (name: string, value: string) => attributes.set(name, value) }
  domPurifySecurityHooks.beforeSanitizeElements(element as unknown as Node)
  assert.deepEqual(Object.fromEntries(attributes), { src: original.src, srcset: original.srcset, style: original.style })
})
