import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { existsSync } from 'node:fs'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { setTimeout as delay } from 'node:timers/promises'
import vue from '@vitejs/plugin-vue'
import { createServer } from 'vite'

export const chromeExecutable = [
  process.env.CHROME_BIN,
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
].find(candidate => candidate && existsSync(candidate))
export const browserAvailable = !!(chromeExecutable || process.env.BROWSER_CDP_URL)

async function waitFor(check, description, timeout = 20_000) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline) {
    const result = await check()
    if (result) return result
    await delay(50)
  }
  throw new Error(`Timed out waiting for ${description}`)
}

class BrowserPage {
  #socket
  #sequence = 0
  #pending = new Map()
  errors = []

  constructor(socket) {
    this.#socket = socket
    socket.addEventListener('message', event => {
      const message = JSON.parse(event.data)
      if (message.method === 'Runtime.exceptionThrown') this.errors.push(message.params.exceptionDetails)
      if (!message.id) return
      const pending = this.#pending.get(message.id)
      if (!pending) return
      this.#pending.delete(message.id)
      clearTimeout(pending.timer)
      if (message.error) pending.reject(new Error(message.error.message))
      else pending.resolve(message.result)
    })
  }

  send(method, params = {}) {
    return new Promise((resolve, reject) => {
      const id = ++this.#sequence
      const timer = setTimeout(() => {
        this.#pending.delete(id)
        reject(new Error(`CDP command timed out: ${method}`))
      }, 20_000)
      this.#pending.set(id, { resolve, reject, timer })
      this.#socket.send(JSON.stringify({ id, method, params }))
    })
  }

  async evaluate(expression) {
    const result = await this.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true })
    if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails))
    return result.result.value
  }

  async waitFor(expression, description = expression) {
    return waitFor(() => this.evaluate(expression), description)
  }

  async click(selector, position = { x: 0.5, y: 0.5 }) {
    const point = await this.evaluate(`(() => {
      const element = [...document.querySelectorAll(${JSON.stringify(selector)})].find(candidate => candidate.getClientRects().length > 0)
      if (!element) return null
      element.scrollIntoView({ block: 'center' })
      const rect = element.getBoundingClientRect()
      return { x: rect.x + rect.width * ${position.x}, y: rect.y + rect.height * ${position.y} }
    })()`)
    assert.ok(point, `Click target exists: ${selector}`)
    await this.send('Input.dispatchMouseEvent', { type: 'mousePressed', button: 'left', clickCount: 1, ...point })
    await this.send('Input.dispatchMouseEvent', { type: 'mouseReleased', button: 'left', clickCount: 1, ...point })
  }

  async pressEscape() {
    const key = { key: 'Escape', code: 'Escape', windowsVirtualKeyCode: 27, nativeVirtualKeyCode: 27 }
    await this.send('Input.dispatchKeyEvent', { type: 'keyDown', ...key })
    await this.send('Input.dispatchKeyEvent', { type: 'keyUp', ...key })
  }

  async screenshot() {
    const result = await this.send('Page.captureScreenshot', { format: 'png' })
    return Buffer.from(result.data, 'base64')
  }

  async viewport(width, height = 900) {
    await this.send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: width < 600 })
  }

  close() { this.#socket.close() }
}

// Use the project's Vue compiler and the actual browser DOM without adding a test dependency.
export async function createBrowserHarness(clientSource, { routes = () => false } = {}) {
  assert.ok(browserAvailable, 'Set CHROME_BIN or BROWSER_CDP_URL to a Chrome or Chromium browser')
  const root = fileURLToPath(new URL('../', import.meta.url))
  const profile = await mkdtemp(join(tmpdir(), 'reliquary-browser-test-'))
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
    server: { host: process.env.BROWSER_CDP_URL ? '0.0.0.0' : '127.0.0.1', port: 0, allowedHosts: ['host.docker.internal'] },
  })
  let browser
  const pages = []
  try {
    await server.listen()
    const address = server.httpServer.address()
    assert.ok(address && typeof address !== 'string')
    const origin = `http://${process.env.BROWSER_TEST_HOST || '127.0.0.1'}:${address.port}`
    let browserURL = process.env.BROWSER_CDP_URL
    if (!browserURL) {
      let browserFailure
      browser = spawn(chromeExecutable, [
        '--headless=new', '--remote-debugging-port=0', '--no-first-run', '--no-default-browser-check',
        '--disable-background-networking', '--disable-extensions', '--disable-dev-shm-usage', '--disable-gpu',
        ...(process.platform === 'linux' ? ['--no-sandbox'] : []),
        `--user-data-dir=${profile}`, '--window-size=1280,900', 'about:blank',
      ], { stdio: ['ignore', 'ignore', 'pipe'] })
      let stderr = ''
      browser.stderr.on('data', chunk => { stderr += chunk })
      browser.on('exit', (code, signal) => { browserFailure = new Error(`Chrome exited: ${code ?? signal}; ${stderr}`) })
      const port = await waitFor(async () => {
        if (browserFailure) throw browserFailure
        try { return (await readFile(join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0] }
        catch { return undefined }
      }, 'Chrome debugging port')
      browserURL = `http://127.0.0.1:${port}`
    }
    return {
      async page(query = '') {
        const target = await (await fetch(`${browserURL}/json/new?about:blank`, { method: 'PUT' })).json()
        const socketURL = new URL(target.webSocketDebuggerUrl)
        socketURL.host = new URL(browserURL).host
        const socket = new WebSocket(socketURL)
        await new Promise((resolve, reject) => {
          socket.addEventListener('open', resolve, { once: true })
          socket.addEventListener('error', reject, { once: true })
        })
        const page = new BrowserPage(socket)
        pages.push({ page, targetID: target.id })
        await page.send('Runtime.enable')
        await page.send('Page.enable')
        await page.viewport(1280)
        await page.send('Page.navigate', { url: `${origin}/__browser-test__/index.html${query}` })
        return page
      },
      async close() {
        for (const { page, targetID } of pages) {
          await fetch(`${browserURL}/json/close/${targetID}`)
          page.close()
        }
        browser?.kill('SIGTERM')
        await server.close()
        await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 })
      },
    }
  } catch (error) {
    browser?.kill('SIGTERM')
    await server.close()
    await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 })
    throw error
  }
}
