import * as bodyReference from '../utils/bodyReference'
import * as libraryScopeHelpers from '../utils/libraryScope'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { defineComponent, h, reactive } from 'vue'
import * as vue from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import * as protectedAccess from '../utils/protectedFileAccess'
import * as searchHelpers from '../utils/search'
import * as highlight from './GlobalCommandPalette/useHighlight'
import * as markdownPlainText from '../utils/markdownPlainText'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../utils/sfcStyleTestHelpers'
import { click, descendants, element, hasClass, loadComponent, registerPrimitives, renderer, settle } from '../views/library/componentTestHarness'

const require = createRequire(import.meta.url)
const moduleOf = (value: unknown) => ({ __esModule: true, default: value })
const empty = moduleOf(defineComponent({ setup: () => () => null }))
const messages = { 'zh-CN': zh, 'en-US': en }

function loadSearchAPI(post: (url: string, body: any) => Promise<unknown>) {
  const source = readFileSync(new URL('../api/search.ts', import.meta.url), 'utf8')
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const value = { exports: {} as any }
  new Function('require', 'module', 'exports', code)((name: string) => name === '@/utils/request' ? {
    post, get: async (url: string) => {
      if (url === '/api/v1/libraries') return { success: true, data: [{ id: 'images' }] }
      assert.equal(url, '/api/v1/search/options')
      return { success: true, data: { entities: [], } }
    },
  } : name === '@/utils/libraryScope' ? libraryScopeHelpers : require(name), value, value.exports)
  return value.exports
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: API-loaded search image opens the existing viewer with the authenticated blob and closes safely`, async () => {
    const requests: { url: string; options: RequestInit }[] = []
    const searches: any[] = [], revoked: string[] = [], navigations: unknown[] = []
    const previous = new Map<string, PropertyDescriptor | undefined>()
    const originalRevoke = URL.revokeObjectURL
    for (const [key, value] of Object.entries({
      localStorage: { getItem: () => 'private-image-token' },
      fetch: async (url: string, options: RequestInit) => {
        requests.push({ url, options })
        return { ok: true, blob: async () => new Blob(['image-fixture'], { type: 'image/png' }) }
      },
    })) {
      previous.set(key, Object.getOwnPropertyDescriptor(globalThis, key))
      Object.defineProperty(globalThis, key, { configurable: true, value })
    }
    URL.revokeObjectURL = url => { revoked.push(url); originalRevoke(url) }
    const viewer = defineComponent({
      props: { visible: Boolean, images: Array, closeOnOverlay: Boolean, closeOnEscKeydown: Boolean }, emits: ['close'],
      setup: (props, { emit }) => () => props.visible ? h('section', {
        role: 'dialog', 'data-preview-images': props.images,
        'data-overlay-close': props.closeOnOverlay, 'data-escape-close': props.closeOnEscKeydown,
      }, [h('button', { 'data-close-preview': true, onClick: () => emit('close') }, 'Close')]) : null,
    })
    const preview = loadComponent(new URL('./ImagePreview.vue', import.meta.url), {
      'tdesign-vue-next': { ImageViewer: viewer },
    })
    const image = loadComponent(new URL('./SearchImage.vue', import.meta.url), {
      '@/utils/protectedFileAccess': protectedAccess, './ImagePreview.vue': moduleOf(preview),
    })
    const article = loadComponent(new URL('./SearchEntry.vue', import.meta.url), {
      './EntryUpdateNotice.vue': empty, './SearchImage.vue': moduleOf(image), './EntryClassification.vue': empty,
      './GlobalCommandPalette/useHighlight': highlight,
      '@/utils/markdownPlainText': markdownPlainText,
    })
    const route = reactive({ query: { q: 'a blue circle' } })
    const api = loadSearchAPI(async (url, body) => {
      assert.equal(url, '/api/v1/search')
      searches.push(body)
      const id = searches.length === 1 ? 'image-a' : 'image-b'
      return { success: true, data: { results: [{
        entry_id: id, title: 'Visual evidence', source: 'article', published_at: null,
        score: 0.9, processing_mode: 'full', snippets: [{
          content: '', match_type: 'image', image_path: `../../attachments/${id}/figure.png`,
        }],
      }] } }
    })
    const page = loadComponent(new URL('../views/library/SearchView.vue', import.meta.url), {
      // URL state supplies the query; this renderer has no native DOM input.
      vue: { ...vue, vModelText: {} },
      '@/api/search': api, '@/utils/search': searchHelpers,
    '@/utils/bodyReference': bodyReference,
    '@/composables/useLibraryScope': { useLibraryScope: () => vue.ref([]) },
    '@/components/LibraryScopeSelect.vue': empty,
      '@/components/SearchEntry.vue': moduleOf(article), '@/components/SearchFilters.vue': empty,
      'vue-router': { useRoute: () => route, useRouter: () => ({ replace: async () => {}, push: async (path: unknown) => { navigations.push(path) } }) },
    })
    const app = renderer.createApp(page)
    app.use(createI18n({ legacy: false, locale, messages })); registerPrimitives(app)
    app.component('t-empty', defineComponent({ setup: () => () => null }))
    const root = element('root')
    const errors: unknown[] = []
    app.config.errorHandler = error => errors.push(error)
    try {
      app.mount(root); await settle()
      assert.equal(searches.length, 1)
      assert.equal(searches[0].query, 'a blue circle')
      assert.deepEqual(searches[0].libraries, ['images'])
      const thumbnail = descendants(root).find(node => hasClass(node, 'search-image-open'))
      assert.ok(thumbnail, 'the loaded picture has an operable preview trigger')
      assert.equal(thumbnail.tag, 'button')
      assert.equal(thumbnail.props['aria-label'], messages[locale].knowledgeSearch.previewImage.replace('{title}', 'Visual evidence'))
      const source = descendants(thumbnail).find(node => node.tag === 'img')!.props.src
      assert.match(source, /^blob:/)
      await click(thumbnail)
      const dialog = descendants(root).find(node => node.props.role === 'dialog')!
      assert.ok(dialog)
      assert.deepEqual(dialog.props['data-preview-images'], [source])
      assert.equal(dialog.props['data-overlay-close'], true)
      assert.equal(dialog.props['data-escape-close'], true)
      assert.equal(requests.length, 1, 'preview reuses the authorized blob instead of fetching an unprotected URL')
      assert.equal(navigations.length, 0)
      const attachment = new URL(requests[0].url, 'http://localhost')
      assert.equal(attachment.pathname, '/api/v1/entries/image-a/vault-attachment')
      assert.equal(attachment.searchParams.get('path'), '../../attachments/image-a/figure.png')
      assert.deepEqual(requests[0].options.headers, {})
      assert.doesNotMatch(requests[0].url + source, /private-image-token/)
      await click(descendants(dialog).find(node => node.props['data-close-preview'])!)
      assert.equal(descendants(root).some(node => node.props.role === 'dialog'), false)
      assert.deepEqual(revoked, [], 'closing the viewer keeps the thumbnail blob alive')
      await click(thumbnail)
      route.query = { q: 'another figure' }
      await settle()
      assert.equal(searches.length, 2)
      assert.equal(descendants(root).some(node => node.props.role === 'dialog'), false)
      assert.deepEqual(revoked, [source], 'changing the search closes the old preview and releases its blob')
      assert.equal(requests[0].options.signal?.aborted, true)
      assert.deepEqual(errors, [])
    } finally {
      app.unmount()
      URL.revokeObjectURL = originalRevoke
      for (const [key, descriptor] of previous) {
        if (descriptor) Object.defineProperty(globalThis, key, descriptor)
        else Reflect.deleteProperty(globalThis, key)
      }
    }
  })
}

test('thumbnail CSS confines the whole image to a fixed frame without intrinsic grid overflow or cropping', async () => {
  const rules = await compiledSFCStyleRules(new URL('./SearchImage.vue', import.meta.url))
  for (const width of [390, 1440]) {
    const frame = styleDeclarationsAtWidth(rules, '.search-image-preview', width)
    const image = styleDeclarationsAtWidth(rules, 'img', width)
    const trigger = styleDeclarationsAtWidth(rules, '.search-image-open', width)
    assert.equal(frame['position'], 'relative')
    assert.equal(frame['aspect-ratio'], '4 / 3')
    assert.equal(image['position'], 'absolute', 'intrinsic image size cannot grow a grid row past its aspect ratio')
    assert.equal(image['inset'], '0')
    assert.equal(image['width'], '100%'); assert.equal(image['height'], '100%')
    assert.equal(image['max-width'], '100%'); assert.equal(image['max-height'], '100%')
    assert.equal(image['object-fit'], 'contain')
    assert.equal(trigger['position'], 'absolute'); assert.equal(trigger['inset'], '0')
    assert.equal(trigger['padding'], '0'); assert.equal(trigger['border'], '0')
    assert.equal(styleDeclarationsAtWidth(rules, '.search-image', width)['width'], '240px')
    assert.equal(styleDeclarationsAtWidth(rules, '.search-image.compact', width)['width'], '120px')
  }
})


test('the shared image viewer preserves remote image sources supplied by its caller', async () => {
  const images = ['https://source.example/remote.png', '//source.example/protocol-relative.png']
  const snapshots: { visible: boolean; images: unknown[] }[] = []
  const viewer = defineComponent({ props: { visible: Boolean, images: Array }, setup(props) {
    return () => { snapshots.push({ visible: props.visible, images: props.images || [] }); return null }
  } })
  const component = loadComponent(new URL('./ImagePreview.vue', import.meta.url), {
    'tdesign-vue-next': { ImageViewer: viewer },
  })
  const app = renderer.createApp(component, { visible: true, images })
  try {
    app.mount(element('root')); await settle()
    assert.ok(snapshots.some(value => value.visible))
    assert.ok(snapshots.every(value => JSON.stringify(value.images) === JSON.stringify(images)))
  } finally {
    app.unmount()
  }
})
