import assert from 'node:assert/strict'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'
import vue from '@vitejs/plugin-vue'
import { createServer } from 'vite'

// Sandbox runs must not start a browser. Opt in only outside the sandbox.
export const browserTestsEnabled = process.env.BROWSER_TESTS === '1'

// Vite serves the component fixture. Playwright provides the browser, page, and input.
export async function createBrowserHarness(clientSource, { routes = () => false } = {}) {
  if (!browserTestsEnabled) {
    throw new Error('Refusing to launch a browser. Set BROWSER_TESTS=1 to run these tests outside the sandbox.')
  }
  const root = fileURLToPath(new URL('../', import.meta.url))
  const fixtureID = '\0browser-test-fixture'
  const fixturePath = '/__browser-test__/fixture.js'
  const server = await createServer({
    root, configFile: false, logLevel: 'error',
    resolve: { alias: { '@': join(root, 'src') } },
    plugins: [vue(), {
      name: 'browser-test-fixture',
      resolveId: id => id === fixturePath ? fixtureID : undefined,
      load: id => id === fixtureID ? clientSource : undefined,
      configureServer(vite) {
        vite.middlewares.use(async (request, response, next) => {
          if (routes(request, response)) return
          if (!request.url?.startsWith('/__browser-test__/index.html')) return next()
          const html = await vite.transformIndexHtml(request.url,
            `<html><head><meta charset="utf-8"></head><body><div id="app"></div><script type="module" src="${fixturePath}"></script></body></html>`)
          response.setHeader('Content-Type', 'text/html')
          response.end(html)
        })
      },
    }],
    server: { host: '127.0.0.1', port: 0 },
  })
  let browser
  try {
    await server.listen()
    const address = server.httpServer.address()
    assert.ok(address && typeof address !== 'string')
    const origin = `http://127.0.0.1:${address.port}`
    // Visible window of Playwright's Chrome for Testing (`npx playwright install chromium`).
    browser = await chromium.launch({ headless: false })
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    return {
      async page(query = '') {
        const page = await context.newPage()
        page.setDefaultTimeout(20_000)
        const errors = []
        page.on('pageerror', error => errors.push(error))
        page.errors = errors
        await page.goto(`${origin}/__browser-test__/index.html${query}`)
        return page
      },
      async close() {
        try {
          await browser.close()
        } finally {
          await server.close()
        }
      },
    }
  } catch (error) {
    await browser?.close()
    await server.close()
    throw error
  }
}
