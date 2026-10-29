import assert from 'node:assert/strict'
import { after, before } from 'node:test'
import test from 'node:test'
import { setTimeout as delay } from 'node:timers/promises'
import { mkdir, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { browserTestsEnabled, createBrowserHarness, type BrowserHarness, type BrowserPage } from '../../test/browserHarness.mjs'

const skipBrowser = !browserTestsEnabled && 'Skipped: browser tests launch a visible Playwright Chrome for Testing window and run only when BROWSER_TESTS=1. Do not set that inside the sandbox.'

const clientSource = `
import { createApp, defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import '/src/styles/tokens.css'
import SearchEntry from '/src/views/library/SearchResult.vue'
import SearchImage from '/src/components/SearchImage.vue'
import zh from '/src/i18n/locales/zh-CN.ts'
import en from '/src/i18n/locales/en-US.ts'

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
    return () => fixture === 'compact'
        ? h(SearchImage, { articleId: 'keyboard-article', path: '../../attachments/compact.svg', title: 'Compact evidence', compact: true })
      : articleVisible.value ? h(SearchEntry, {
        query: 'figure', libraries: [],
        article: {
          entry_id: articleID.value, title: 'Visual evidence', source: 'article',
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
app.mount('#app')
window.__fixtureReady = true
`

const requests: { url: string; authorization: string | undefined }[] = []
let harness: BrowserHarness
before(async () => {
  if (!browserTestsEnabled) return
  harness = await createBrowserHarness(clientSource, { routes(request, response) {
    if (!/^\/api\/v1\/entries\/keyboard-(?:new-)?article\/vault-attachment\?/.test(request.url || '')) return false
    requests.push({ url: request.url!, authorization: request.headers.authorization })
    response.setHeader('Content-Type', 'image/svg+xml')
    response.end('<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="blue"/></svg>')
    return true
  } })
})
after(async () => { await harness?.close() })

const viewerProbe = `(() => {
  const viewer = [...document.querySelectorAll('.rq-dialog')].find(element => getComputedStyle(element).display !== 'none')
  const active = document.activeElement
  return {
    viewerVisible: !!viewer,
    focusInViewer: !!viewer && viewer.contains(active),
    activeTag: active?.tagName,
    activeClass: typeof active?.className === 'string' ? active.className : '',
  }
})()`

async function waitFor(page: BrowserPage, expression: string, description: string) {
  try {
    await page.waitForFunction(expression)
  } catch (error) {
    throw new Error(`Timed out waiting for ${description}`, { cause: error })
  }
}

const thumbnailSelector = '.result-snippet:nth-child(2) .search-image-open'
async function searchPage(query = '') {
  const page = await harness.page(query)
  await waitFor(page, `window.__fixtureReady && document.querySelectorAll('.search-image-open img').length === 2
    && [...document.querySelectorAll('.search-image-open img')].every(image => image.complete && image.naturalWidth > 0)`, 'both authorized thumbnail images')
  await page.evaluate(`window.__previewTrigger = document.querySelector(${JSON.stringify(thumbnailSelector)})`)
  return page
}
async function openSearchPreview(page: BrowserPage) {
  await page.click(thumbnailSelector)
  await waitFor(page, `(${viewerProbe}).viewerVisible`, 'visible image dialog')
  await delay(250)
}
async function waitForClosedPreview(page: BrowserPage, restoreFocus = true) {
  await waitFor(page, `!(${viewerProbe}).viewerVisible${restoreFocus ? ' && document.activeElement === window.__previewTrigger' : ''}`, 'closed preview and settled focus')
}
async function saveEvidence(page: BrowserPage, name: string, probe: unknown) {
  if (!process.env.BROWSER_EVIDENCE_DIR) return
  await mkdir(process.env.BROWSER_EVIDENCE_DIR, { recursive: true })
  await writeFile(join(process.env.BROWSER_EVIDENCE_DIR, `${name}.json`), JSON.stringify(probe, null, 2) + '\n')
  await writeFile(join(process.env.BROWSER_EVIDENCE_DIR, `${name}.png`), await page.screenshot())
}

for (const locale of ['zh-CN', 'en-US']) {
  test(`${locale}: actual search result preview closes on Escape and restores its thumbnail focus`, {
    skip: skipBrowser,
  }, async t => {
    const theme = locale === 'en-US' ? 'dark' : 'light'
    const page = await searchPage(`?locale=${locale}&theme=${theme}`)
    if (locale === 'en-US') await page.setViewportSize({ width: 390, height: 900 })
    const thumbnailRequestCount = requests.length
    await openSearchPreview(page)
    const opened = await page.evaluate(viewerProbe)
    const imageSource = await page.evaluate(`window.__previewTrigger.querySelector('img').src`)
    assert.match(imageSource, /^blob:/)
    assert.equal(await page.evaluate(`[...document.querySelectorAll('.rq-dialog')].find(element => getComputedStyle(element).display !== 'none')?.querySelector('.search-image-full')?.src`), imageSource)
    assert.deepEqual(await page.evaluate(`(() => {
      const image = [...document.querySelectorAll('.rq-dialog')].find(element => getComputedStyle(element).display !== 'none').querySelector('.search-image-full')
      return { width: image.naturalWidth, height: image.naturalHeight, fit: getComputedStyle(image).objectFit }
    })()`), { width: 40, height: 20, fit: 'contain' }, 'the viewer displays the complete original image')
    await saveEvidence(page, `search-${locale}-${theme}-open`, opened)
    await page.keyboard.press('Escape')
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
    assert.ok(articleRequests.every(request => !request.authorization))
    assert.ok(articleRequests.every(request => !request.url.includes('keyboard-preview-test-token')))
    await openSearchPreview(page)
    const reopened = await page.evaluate(viewerProbe)
    t.diagnostic(JSON.stringify({ locale, reopened }))
    assert.equal(reopened.focusInViewer, true, 'reopening the second thumbnail also enters the preview')
    await page.keyboard.press('Escape')
    await waitForClosedPreview(page)
    assert.equal((await page.evaluate(viewerProbe)).viewerVisible, false)
    assert.equal(await page.evaluate('document.activeElement === window.__previewTrigger'), true)
    assert.equal(requests.length, thumbnailRequestCount, 'preview opening, closing and reopening reuse the authorized blobs')
    assert.deepEqual(await page.evaluate('window.__revokedImageURLs'), [], 'closing and reopening retains both thumbnail blobs')
  })
}

test('actual preview close button and overlay both retain close behavior and restore thumbnail focus', {
  skip: skipBrowser,
}, async () => {
  const page = await searchPage()
  for (const closeMethod of ['button', 'overlay']) {
    await openSearchPreview(page)
    assert.equal((await page.evaluate(viewerProbe)).focusInViewer, true)
    if (closeMethod === 'button') await page.click('.rq-dialog .rq-icon-button')
    else {
      const box = await page.locator('.rq-overlay').boundingBox()
      assert.ok(box, 'overlay')
      await page.click('.rq-overlay', { position: { x: box.width * 0.01, y: box.height * 0.5 } })
    }
    await waitForClosedPreview(page)
    assert.equal((await page.evaluate(viewerProbe)).viewerVisible, false, closeMethod)
    assert.equal(await page.evaluate('document.activeElement === window.__previewTrigger'), true, closeMethod)
  }
  assert.deepEqual(page.errors, [])
})

for (const transition of ['changeArticle', 'removeResults']) {
  test(`actual preview closes and releases blobs when results ${transition}`, {
    skip: skipBrowser,
  }, async () => {
    const page = await searchPage()
    const previousSources = await page.evaluate(`[...document.querySelectorAll('.search-image-open img')].map(image => image.src)`)
    await openSearchPreview(page)
    await page.evaluate(`window.__fixture.${transition}()`)
    await waitForClosedPreview(page, false)
    await waitFor(page, `window.__revokedImageURLs.length === 2 && !window.__previewTrigger.isConnected`, 'released blobs and removed original trigger')
    assert.equal((await page.evaluate(viewerProbe)).viewerVisible, false)
    assert.deepEqual(await page.evaluate('window.__revokedImageURLs'), previousSources)
    assert.equal(await page.evaluate('window.__previewTrigger.isConnected'), false)
    assert.equal(await page.evaluate('document.activeElement === window.__previewTrigger'), false)
    if (transition === 'changeArticle') {
      await waitFor(page, `document.querySelectorAll('.search-image-open img').length === 2
        && [...document.querySelectorAll('.search-image-open img')].every(image => image.complete && image.naturalWidth > 0)`, 'replacement thumbnails')
      assert.equal(await page.evaluate(`[...document.querySelectorAll('.search-image-open img')].some(image => ${JSON.stringify(previousSources)}.includes(image.src))`), false)
      await page.evaluate(`window.__previewTrigger = document.querySelector(${JSON.stringify(thumbnailSelector)})`)
      await openSearchPreview(page)
      await page.keyboard.press('Escape')
      await waitForClosedPreview(page)
      assert.equal((await page.evaluate(viewerProbe)).viewerVisible, false)
      assert.equal(await page.evaluate('document.activeElement === window.__previewTrigger'), true)
    }
    assert.deepEqual(page.errors, [])
  })
}

test('compact search image stays a thumbnail without a viewer trigger', {
  skip: skipBrowser,
}, async () => {
  const page = await harness.page('?fixture=compact')
  await waitFor(page, `window.__fixtureReady && document.querySelector('.search-image.compact img')?.naturalWidth > 0`, 'compact thumbnail')
  assert.equal(await page.evaluate(`document.querySelectorAll('.search-image-open').length`), 0)
  assert.equal(await page.evaluate(`getComputedStyle(document.querySelector('.search-image.compact img')).objectFit`), 'contain')
  await page.click('.search-image.compact img')
  await page.keyboard.press('Escape')
  assert.equal((await page.evaluate(viewerProbe)).viewerVisible, false)
  assert.deepEqual(page.errors, [])
})
