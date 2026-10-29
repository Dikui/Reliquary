import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test, { type TestContext } from 'node:test'
import axios, { AxiosError, type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createPinia, defineStore, setActivePinia } from 'pinia'
import { createRenderer, createSSRApp, nextTick, onScopeDispose, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import { translatePersonalServerMessage } from '../utils/personalServerError'

const require = createRequire(import.meta.url)
const source = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')
function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const value = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? require(name), value, value.exports)
  return value.exports
}
const endpoints = [
  '/api/v1/entries/pending?page=1&page_size=1&kind=intake',
] as const
const envelope = (total: number) => ({ success: true, data: [], total, page: 1, page_size: 1 })
type Packet = {
  config: InternalAxiosRequestConfig
  finished: boolean
  claimed: boolean
  resolve: (data: unknown) => void
  reject: () => void
}
type HostNode = { kind: string; text: string; children: HostNode[]; parent: HostNode | null; props: Record<string, unknown> }
const hostNode = (kind: string, text = ''): HostNode => ({ kind, text, children: [], parent: null, props: {} })
const textOf = (node: HostNode): string => node.text + node.children.map(textOf).join('')
function renderer() {
  return createRenderer<HostNode, HostNode>({
    createElement: kind => hostNode(kind), createText: text => hostNode('text', text), createComment: text => hostNode('comment', text),
    patchProp: (node, key, _previous, value) => { node.props[key] = value },
    parentNode: node => node.parent,
    nextSibling: node => node.parent?.children[node.parent.children.indexOf(node) + 1] ?? null,
    setText: (node, text) => { node.text = text },
    setElementText: (node, text) => { node.text = text; node.children = [] },
    insert: (node, parent, anchor) => {
      if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1)
      const index = anchor ? parent.children.indexOf(anchor) : parent.children.length
      parent.children.splice(index, 0, node)
      node.parent = parent
    },
    remove: node => { node.parent?.children.splice(node.parent.children.indexOf(node), 1); node.parent = null },
  })
}
function fixture(t: TestContext) {
  const oldStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  const storage = new Map([['locale', 'zh-CN']])
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
  } })
  const packets: Packet[] = []
  const network = { __esModule: true, default: { create: (config: object) => axios.create({ ...config, adapter: (config: InternalAxiosRequestConfig) => new Promise<AxiosResponse>((resolve, reject) => {
    const packet: Packet = { config, finished: false, claimed: false,
      resolve: data => { packet.finished = true; resolve({ data, status: 200, statusText: 'OK', headers: {}, config }) },
      reject: () => {
        packet.finished = true
        const response = { data: { success: false, error: { message: 'Queue unavailable' } }, status: 503, statusText: 'Unavailable', headers: {}, config }
        reject(new AxiosError('Queue unavailable', AxiosError.ERR_BAD_RESPONSE, config, undefined, response))
      },
    }
    packets.push(packet)
  }) }) } }
  const request = evaluate(source('../utils/request.ts'), {
    axios: network,
    './index': { generateRandomString: () => 'pending-request', MAX_FILE_SIZE_MB: 100 },
    './personalServerError': { translatePersonalServerMessage },
    './api-base': { getApiBaseUrl: () => 'http://pending.test' },
    '@/i18n': { __esModule: true, default: { global: { locale: { value: 'zh-CN' }, t: (key: string) => key } } },
  })
  const reviews = evaluate(source('../api/entry-review.ts'), { '@/utils/request': request })
  const pinia = createPinia()
  setActivePinia(pinia)
  const storeModule = evaluate(source('./pendingCounts.ts'), { pinia: { defineStore }, vue: { ref, onScopeDispose }, '@/api/entry-review': reviews,
    '@/api/library': { listLibraries: async () => ({ success: true, data: [] }) },
    '@/api/entities': { listEntityMergeSuggestions: async () => ({ success: true, data: [] }) },
  })
  const store = storeModule.usePendingCountsStore()
  t.after(async () => {
    store.$dispose()
    for (const packet of packets) if (!packet.finished) packet.resolve(envelope(0))
    await new Promise<void>(resolve => setImmediate(resolve))
    request.stopServiceAvailabilityRetry()
    if (oldStorage) Object.defineProperty(globalThis, 'localStorage', oldStorage)
    else Reflect.deleteProperty(globalThis, 'localStorage')
  })
  async function next(url: string, method = 'get') {
    for (let attempt = 0; attempt < 20; attempt++) {
      const packet = packets.find(packet => !packet.finished && !packet.claimed && packet.config.url === url && packet.config.method === method)
      if (packet) { packet.claimed = true; return packet }
      await new Promise<void>(resolve => setImmediate(resolve))
    }
    assert.fail(`missing ${method} ${url}`)
  }
  async function batch() { return Promise.all(endpoints.map(url => next(url))) }
  async function finish(totals: number[]) {
    const reading = store.refresh()
    const requests = await batch()
    requests.forEach((packet, index) => packet.resolve(envelope(totals[index]!)))
    await reading
  }
  function badgeComponent(ssr: boolean) {
    const filename = 'PendingCountBadge.vue'
    const { descriptor, errors } = parse(source('../components/PendingCountBadge.vue'), { filename })
    assert.deepEqual(errors, [])
    const script = compileScript(descriptor, { id: filename })
    const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
    assert.deepEqual(template.errors, [])
    const modules = { '@/stores/pendingCounts': storeModule }
    const component = evaluate(script.content, modules).default
    const templateModule = evaluate(template.code, modules)
    component[ssr ? 'ssrRender' : 'render'] = templateModule[ssr ? 'ssrRender' : 'render']
    return component
  }
  async function badge(locale: 'zh-CN' | 'en-US') {
    const app = createSSRApp(badgeComponent(true))
    app.use(pinia)
    app.use(createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } }))
    const errorsFound: unknown[] = [], warnings: string[] = []
    app.config.errorHandler = cause => errorsFound.push(cause)
    app.config.warnHandler = warning => warnings.push(warning)
    const html = await renderToString(app)
    assert.deepEqual(errorsFound, [])
    assert.deepEqual(warnings, [])
    return html
  }
  function mountBadge() {
    const app = renderer().createApp(badgeComponent(false))
    app.use(pinia)
    app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } }))
    const root = hostNode('root')
    app.mount(root)
    t.after(() => app.unmount())
    return root
  }
  return { store, reviews, packets, next, batch, finish, badge, mountBadge }
}

function pageHandlers(bindings: Record<string, unknown>) {
  const page = source('../views/library/Library.vue')
  const { descriptor } = parse(page)
  assert.ok(descriptor.scriptSetup)
  const ast = ts.createSourceFile('Library.ts', descriptor.scriptSetup.content, ts.ScriptTarget.ESNext, true, ts.ScriptKind.TS)
  const names = ['handlePendingChanged', 'handleReviewChanged']
  const statements = ast.statements.filter(statement => ts.isVariableStatement(statement)
    && statement.declarationList.declarations.some(declaration => ts.isIdentifier(declaration.name) && names.includes(declaration.name.text)))
  assert.equal(statements.length, names.length)
  assert.match(descriptor.template!.content, /<PendingReviews\b[^>]*@changed="handlePendingChanged"/)
  assert.match(descriptor.template!.content, /<DocContent\b[^>]*@review-changed="handleReviewChanged"/)
  assert.match(source('../components/SideMenu.vue'), /<PendingCountBadge v-if="item.key === 'pending'"/);
  const code = statements.map(statement => statement.getText(ast)).join('\n') + '\nexport { handlePendingChanged, handleReviewChanged }'
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const value = { exports: {} as any }
  new Function(...Object.keys(bindings), 'module', 'exports', output)(...Object.values(bindings), value, value.exports)
  return value.exports
}

test('navigation badge loads only the intake count', async t => {
  const f = fixture(t)
  const root = f.mountBadge()
  assert.equal(f.store.loading, true)
  const [packet] = await f.batch()
  packet.resolve(envelope(4))
  for (let attempt = 0; attempt < 20 && f.store.loading; attempt++) await new Promise<void>(resolve => setImmediate(resolve))
  await nextTick()
  assert.equal(f.store.total, 4)
  assert.equal(f.packets.length, 1)
  assert.equal(textOf(root.children.find(node => node.kind === 'span')!), '4')
  for (const locale of ['zh-CN', 'en-US'] as const) assert.match(await f.badge(locale), />4<\/span>/)
})

test('refresh hides the old count and renders a confirmed zero', async t => {
  const f = fixture(t)
  await f.finish([6])
  const reading = f.store.refresh()
  assert.equal(f.store.total, null)
  ;(await f.batch())[0].resolve(envelope(0))
  await reading
  assert.equal(f.store.total, 0)
  assert.match(await f.badge('en-US'), />0<\/span>/)
})

test('failed or invalid intake counts stay unknown and permit retry', async t => {
  const f = fixture(t)
  for (const invalid of [null, { success: false, total: 5 }, { success: true }, envelope(-1), envelope(1.5), envelope(Number.MAX_SAFE_INTEGER + 1)]) {
    const reading = f.store.refresh()
    const [packet] = await f.batch()
    if (invalid === null) packet.reject()
    else packet.resolve(invalid)
    await reading
    assert.equal(f.store.total, null)
    assert.equal(f.store.loading, false)
  }
  await f.finish([3])
  assert.equal(f.store.total, 3)
})

test('a delayed snapshot or failure cannot replace a newer intake count', async t => {
  const f = fixture(t)
  for (const oldFails of [false, true]) {
    const old = f.store.refresh()
    const [packet] = await f.batch()
    await f.finish([24])
    if (oldFails) packet.reject()
    else packet.resolve(envelope(100))
    await old
    assert.equal(f.store.total, 24)
    assert.equal(f.store.loading, false)
  }
})

test('disposing the store prevents in-flight requests from restoring a count', async t => {
  const f = fixture(t)
  const reading = f.store.refresh()
  const [packet] = await f.batch()
  f.store.$dispose()
  packet.resolve(envelope(3))
  await reading
  assert.equal(f.store.total, null)
  assert.equal(f.store.loading, false)
  await f.store.refresh()
  assert.equal(f.packets.length, 1)
})
