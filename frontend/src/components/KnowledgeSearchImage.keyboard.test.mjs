import assert from 'node:assert/strict'
import { after, before } from 'node:test'
import test from 'node:test'
import { setTimeout as delay } from 'node:timers/promises'
import { mkdir, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { browserAvailable, createBrowserHarness } from '../../test/browserHarness.mjs'

const clientSource = `
import { createApp, defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { Button, ImageViewer, Tag } from 'tdesign-vue-next'
import 'tdesign-vue-next/es/style/index.css'
import KnowledgeSearchArticle from '/src/components/KnowledgeSearchArticle.vue'
import KnowledgeSearchImage from '/src/components/KnowledgeSearchImage.vue'
import PicturePreview from '/src/components/picture-preview.vue'
import zh from '/src/i18n/locales/zh-CN.ts'
import en from '/src/i18n/locales/en-US.ts'

localStorage.setItem('reliquary_token', 'keyboard-preview-test-token')
const params = new URLSearchParams(location.search)
const fixture = params.get('fixture') || 'search'
document.documentElement.setAttribute('theme-mode', params.get('theme') || 'light')
window.__revokedImageURLs = []
const revokeObjectURL = URL.revokeObjectURL.bind(URL)
URL.revokeObjectURL = url => { window.__revokedImageURLs.push(url); revokeObjectURL(url) }
const image = 'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="blue"/></svg>')
const Root = defineComponent({
  setup() {
    const visible = ref(false)
    const articleVisible = ref(true), articleID = ref('keyboard-article')
    window.__fixture = {
      removeResults: () => articleVisible.value = false,
      changeArticle: () => articleID.value = 'keyboard-new-article',
    }
    return () => fixture === 'picture'
      ? h('div', [
        h('button', { id: 'picture-trigger', type: 'button', onClick: () => visible.value = true }, 'Preview'),
        visible.value ? h(PicturePreview, { reviewImg: visible.value, reviewUrl: image, onClosePreImg: () => visible.value = false }) : null,
      ])
      : fixture === 'compact'
        ? h(KnowledgeSearchImage, { articleId: 'keyboard-article', path: '../../attachments/compact.svg', title: 'Compact evidence', compact: true })
      : articleVisible.value ? h(KnowledgeSearchArticle, {
        query: 'figure',
        article: {
          article_id: articleID.value, title: 'Visual evidence', source: 'article',
          score: 0.9, published_at: null, processing_mode: 'full',
          snippets: [
            { content: '', match_type: 'image', image_path: '../../attachments/figure-first.svg' },
            { content: '', match_type: 'image', image_path: '../../attachments/figure-second.svg' },
          ],
        },
      }) : h('p', 'No results')
  },
})
const app = createApp(Root)
app.config.errorHandler = error => { throw error }
app.use(createI18n({ legacy: false, locale: params.get('locale') || 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } }))
app.component('t-button', Button)
app.component('t-tag', Tag)
app.component('t-image-viewer', ImageViewer)
app.mount('#app')
window.__fixtureReady = true
`

const requests = []
let harness
before(async () => {
  if (!browserAvailable) return
  harness = await createBrowserHarness(clientSource, { routes(request, response) {
    if (!/^\/api\/v1\/knowledge\/keyboard-(?:new-)?article\/vault-attachment\?/.test(request.url || '')) return false
    requests.push({ url: request.url, authorization: request.headers.authorization })
    response.setHeader('Content-Type', 'image/svg+xml')
    response.end('<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="blue"/></svg>')
    return true
  } })
})
after(async () => { await harness?.close() })

const viewerProbe = `(() => {
  const viewer = [...document.querySelectorAll('.t-image-viewer')].find(element => getComputedStyle(element).display !== 'none')
  const active = document.activeElement
  return {
    viewerVisible: !!viewer,
    focusInViewer: !!viewer && viewer.contains(active),
    activeTag: active?.tagName,
    activeClass: typeof active?.className === 'string' ? active.className : '',
  }
})()`

const thumbnailSelector = '.article-snippet:nth-child(3) .search-image-open'
async function searchPage(query = '') {
  const page = await harness.page(query)
  await page.waitFor(`window.__fixtureReady && document.querySelectorAll('.search-image-open img').length === 2
    && [...document.querySelectorAll('.search-image-open img')].every(image => image.complete && image.naturalWidth > 0)`, 'both authorized thumbnail images')
  await page.evaluate(`window.__previewTrigger = document.querySelector(${JSON.stringify(thumbnailSelector)})`)
  return page
}
async function openSearchPreview(page) {
  await page.click(thumbnailSelector)
  await page.waitFor(`(${viewerProbe}).viewerVisible`, 'visible real TDesign image viewer')
  await delay(250)
}
async function waitForClosedPreview(page, restoreFocus = true) {
  await page.waitFor(`!(${viewerProbe}).viewerVisible${restoreFocus ? ' && document.activeElement === window.__previewTrigger' : ''}`, 'closed preview and settled focus')
}
async function saveEvidence(page, name, probe) {
  if (!process.env.BROWSER_EVIDENCE_DIR) return
  await mkdir(process.env.BROWSER_EVIDENCE_DIR, { recursive: true })
  await writeFile(join(process.env.BROWSER_EVIDENCE_DIR, `${name}.json`), JSON.stringify(probe, null, 2) + '\n')
  await writeFile(join(process.env.BROWSER_EVIDENCE_DIR, `${name}.png`), await page.screenshot())
}

for (const locale of ['zh-CN', 'en-US']) {
  test(`${locale}: actual search result preview closes on Escape and restores its thumbnail focus`, {
    skip: !browserAvailable && 'Chrome is required; set CHROME_BIN or BROWSER_CDP_URL',
  }, async t => {
    const theme = locale === 'en-US' ? 'dark' : 'light'
    const page = await searchPage(`?locale=${locale}&theme=${theme}`)
    if (locale === 'en-US') await page.viewport(390)
    const thumbnailRequestCount = requests.length
    await openSearchPreview(page)
    const opened = await page.evaluate(viewerProbe)
    const imageSource = await page.evaluate(`window.__previewTrigger.querySelector('img').src`)
    assert.match(imageSource, /^blob:/)
    assert.equal(await page.evaluate(`[...document.querySelectorAll('.t-image-viewer')].find(element => getComputedStyle(element).display !== 'none')?.querySelector('.t-image-viewer__modal-image')?.src`), imageSource)
    assert.deepEqual(await page.evaluate(`(() => {
      const image = [...document.querySelectorAll('.t-image-viewer')].find(element => getComputedStyle(element).display !== 'none').querySelector('.t-image-viewer__modal-image')
      return { width: image.naturalWidth, height: image.naturalHeight, fit: getComputedStyle(image).objectFit }
    })()`), { width: 40, height: 20, fit: 'contain' }, 'the viewer displays the complete original image')
    await saveEvidence(page, `search-${locale}-${theme}-open`, opened)
    await page.pressEscape()
    await waitForClosedPreview(page)
    const closed = await page.evaluate(viewerProbe)
    const focusRestored = await page.evaluate('document.activeElement === window.__previewTrigger')
    await saveEvidence(page, `search-${locale}-${theme}-escape`, { ...closed, focusRestored })
    t.diagnostic(JSON.stringify({ locale, opened, closed, focusRestored }))
    assert.deepEqual({
      focusEnteredPreview: opened.focusInViewer,
      escaped: !closed.viewerVisible,
      focusReturnedToClickedThumbnail: focusRestored,
    }, {
      focusEnteredPreview: true,
      escaped: true,
      focusReturnedToClickedThumbnail: true,
    })
    assert.deepEqual(page.errors, [])
    const articleRequests = requests.filter(request => request.url.includes('/keyboard-article/'))
    assert.ok(articleRequests.length >= 2)
    assert.ok(articleRequests.every(request => request.authorization === 'Bearer keyboard-preview-test-token'))
    assert.ok(articleRequests.every(request => !request.url.includes('keyboard-preview-test-token')))
    await openSearchPreview(page)
    const reopened = await page.evaluate(viewerProbe)
    t.diagnostic(JSON.stringify({ locale, reopened }))
    assert.equal(reopened.focusInViewer, true, 'reopening the second thumbnail also enters the preview')
    await page.pressEscape()
    await waitForClosedPreview(page)
    assert.equal((await page.evaluate(viewerProbe)).viewerVisible, false)
    assert.equal(await page.evaluate('document.activeElement === window.__previewTrigger'), true)
    assert.equal(requests.length, thumbnailRequestCount, 'preview opening, closing and reopening reuse the authorized blobs')
    assert.deepEqual(await page.evaluate('window.__revokedImageURLs'), [], 'closing and reopening retains both thumbnail blobs')
  })
}

test('actual preview close button and overlay both retain close behavior and restore thumbnail focus', {
  skip: !browserAvailable && 'Chrome is required; set CHROME_BIN or BROWSER_CDP_URL',
}, async () => {
  const page = await searchPage()
  for (const closeMethod of ['button', 'overlay']) {
    await openSearchPreview(page)
    assert.equal((await page.evaluate(viewerProbe)).focusInViewer, true)
    if (closeMethod === 'button') await page.click('.t-image-viewer__modal-close-bt')
    else await page.click('.t-image-viewer__modal-mask', { x: 0.01, y: 0.5 })
    await waitForClosedPreview(page)
    assert.equal((await page.evaluate(viewerProbe)).viewerVisible, false, closeMethod)
    assert.equal(await page.evaluate('document.activeElement === window.__previewTrigger'), true, closeMethod)
  }
  assert.deepEqual(page.errors, [])
})

for (const transition of ['changeArticle', 'removeResults']) {
  test(`actual preview closes and releases blobs when results ${transition}`, {
    skip: !browserAvailable && 'Chrome is required; set CHROME_BIN or BROWSER_CDP_URL',
  }, async () => {
    const page = await searchPage()
    const previousSources = await page.evaluate(`[...document.querySelectorAll('.search-image-open img')].map(image => image.src)`)
    await openSearchPreview(page)
    await page.evaluate(`window.__fixture.${transition}()`)
    await waitForClosedPreview(page, false)
    await page.waitFor(`window.__revokedImageURLs.length === 2 && !window.__previewTrigger.isConnected`, 'released blobs and removed original trigger')
    assert.equal((await page.evaluate(viewerProbe)).viewerVisible, false)
    assert.deepEqual(await page.evaluate('window.__revokedImageURLs'), previousSources)
    assert.equal(await page.evaluate('window.__previewTrigger.isConnected'), false)
    assert.equal(await page.evaluate('document.activeElement === window.__previewTrigger'), false)
    if (transition === 'changeArticle') {
      await page.waitFor(`document.querySelectorAll('.search-image-open img').length === 2
        && [...document.querySelectorAll('.search-image-open img')].every(image => image.complete && image.naturalWidth > 0)`, 'replacement thumbnails')
      assert.equal(await page.evaluate(`[...document.querySelectorAll('.search-image-open img')].some(image => ${JSON.stringify(previousSources)}.includes(image.src))`), false)
      await page.evaluate(`window.__previewTrigger = document.querySelector(${JSON.stringify(thumbnailSelector)})`)
      await openSearchPreview(page)
      await page.pressEscape()
      await waitForClosedPreview(page)
      assert.equal((await page.evaluate(viewerProbe)).viewerVisible, false)
      assert.equal(await page.evaluate('document.activeElement === window.__previewTrigger'), true)
    }
    assert.deepEqual(page.errors, [])
  })
}

test('compact search image stays a thumbnail without a viewer trigger', {
  skip: !browserAvailable && 'Chrome is required; set CHROME_BIN or BROWSER_CDP_URL',
}, async () => {
  const page = await harness.page('?fixture=compact')
  await page.waitFor(`window.__fixtureReady && document.querySelector('.search-image.compact img')?.naturalWidth > 0`, 'compact thumbnail')
  assert.equal(await page.evaluate(`document.querySelectorAll('.search-image-open').length`), 0)
  assert.equal(await page.evaluate(`getComputedStyle(document.querySelector('.search-image.compact img')).objectFit`), 'contain')
  await page.click('.search-image.compact img')
  await page.pressEscape()
  assert.equal((await page.evaluate(viewerProbe)).viewerVisible, false)
  assert.deepEqual(page.errors, [])
})

test('actual shared picture preview mounted open closes on Escape and returns focus to its trigger', {
  skip: !browserAvailable && 'Chrome is required; set CHROME_BIN or BROWSER_CDP_URL',
}, async t => {
  const page = await harness.page('?fixture=picture')
  await page.waitFor(`window.__fixtureReady && !!document.querySelector('#picture-trigger')`, 'picture preview trigger')
  await page.click('#picture-trigger')
  await page.waitFor(`!!document.querySelector('.t-image-viewer')`, 'real shared picture viewer')
  await delay(250)
  const opened = await page.evaluate(viewerProbe)
  await page.pressEscape()
  await page.waitFor(`!(${viewerProbe}).viewerVisible && document.activeElement === document.querySelector('#picture-trigger')`, 'closed picture preview and returned focus')
  const closed = await page.evaluate(viewerProbe)
  const focusRestored = await page.evaluate(`document.activeElement === document.querySelector('#picture-trigger')`)
  t.diagnostic(JSON.stringify({ fixture: 'picture', opened, closed, focusRestored }))
  assert.deepEqual({ focusEnteredPreview: opened.focusInViewer, escaped: !closed.viewerVisible, focusRestored }, {
    focusEnteredPreview: true, escaped: true, focusRestored: true,
  })
  assert.deepEqual(page.errors, [])
})
