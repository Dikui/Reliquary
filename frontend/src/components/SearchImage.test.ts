import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import * as protectedAccess from '../utils/protectedFileAccess'
import { click, descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../views/library/componentTestHarness'

const messages = { 'zh-CN': zh, 'en-US': en }
for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: the actual thumbnail loads an authenticated vault attachment, retries and revokes its blob`, async () => {
    const previous = new Map<string, PropertyDescriptor | undefined>()
    const requests: { url: string; options: RequestInit }[] = []
    const revoked: string[] = []
    const originalRevoke = URL.revokeObjectURL
    const relative = '../../attachments/search-image-1/图表.png'
    for (const [key, value] of Object.entries({
      localStorage: { getItem: () => 'image-token' },
      fetch: async (url: string, options: RequestInit) => { requests.push({ url, options }); return { ok: requests.length > 1, status: 503, blob: async () => new Blob(['fixture'], { type: 'image/png' }) } },
    })) { previous.set(key, Object.getOwnPropertyDescriptor(globalThis, key)); Object.defineProperty(globalThis, key, { configurable: true, value }) }
    URL.revokeObjectURL = value => { revoked.push(value); originalRevoke(value) }
    const preview = loadComponent(new URL('./ImagePreview.vue', import.meta.url), {
      'tdesign-vue-next': { ImageViewer: defineComponent({ setup: () => () => null }) },
    })
    const component = loadComponent(new URL('./SearchImage.vue', import.meta.url), {
      '@/utils/protectedFileAccess': protectedAccess, './ImagePreview.vue': { __esModule: true, default: preview },
    })
    const app = renderer.createApp(component, { articleId: 'search-image-1', path: relative, title: 'Image evidence' })
    app.use(createI18n({ legacy: false, locale, messages })); registerPrimitives(app)
    const root = element('root')
    try {
      app.mount(root); await settle()
      assert.ok(visibleText(root).includes(messages[locale].knowledgeSearch.imageLoadFailed))
      await click(descendants(root).find(node => node.tag === 'button')!)
      const image = descendants(root).find(node => node.tag === 'img')!
      assert.match(image.props.src, /^blob:/)
      assert.ok(image.props.alt.includes('Image evidence'))
      assert.equal(image.props.loading, 'lazy')
      assert.equal(requests.length, 2)
      const target = new URL(requests[1].url, 'http://localhost')
      assert.equal(target.pathname, '/api/v1/entries/search-image-1/vault-attachment')
      assert.equal(target.searchParams.get('path'), relative)
      assert.deepEqual(requests[1].options.headers, { Authorization: 'Bearer image-token' })
      assert.equal(requests[1].options.credentials, 'include')
      assert.doesNotMatch(requests[1].url, /image-token/)
      app.unmount()
      assert.deepEqual(revoked, [image.props.src])
      assert.equal(requests[1].options.signal?.aborted, true)
    } finally {
      if (!revoked.length) app.unmount()
      URL.revokeObjectURL = originalRevoke
      for (const [key, descriptor] of previous) { if (descriptor) Object.defineProperty(globalThis, key, descriptor); else Reflect.deleteProperty(globalThis, key) }
    }
  })
}
