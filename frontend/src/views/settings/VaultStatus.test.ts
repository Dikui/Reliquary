import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { createSSRApp, defineComponent, h, onServerPrefetch } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import enUS from '../../i18n/locales/en-US'
import zhCN from '../../i18n/locales/zh-CN'
import type { VaultStatus } from '../../api/vault'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const source = readFileSync(new URL('./VaultStatus.vue', import.meta.url), 'utf8')
const { descriptor, errors } = parse(source, { filename: 'VaultStatus.vue' })
assert.deepEqual(errors, [])
const script = compileScript(descriptor, { id: 'vault-status-test' })
const template = compileTemplate({ id: 'vault-status-test', filename: 'VaultStatus.vue', source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
assert.deepEqual(template.errors, [])

const status: VaultStatus = {
  root: '/srv/reliquary/vault',
  mine: { directory: 'custom-mine', last_scan_at: '2026-10-11T06:12:30Z', indexed_files: 12 },
  reports: { directory: 'ai-reports', indexed_files: 3, last_error: 'Cannot read ai-reports/example.md' },
}

async function scenario(locale: keyof typeof messages, getVaultStatus: () => Promise<unknown>, action: (state: any) => Promise<void>) {
  const i18n = createI18n({ legacy: false, locale, messages })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
    '@/api/vault': { getVaultStatus },
    './MaintenanceStatus.vue': { __esModule: true, default: defineComponent({ setup: () => () => h('section', { 'data-maintenance-status': true }) }) },
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', compiled)((name: string) => modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  component.setup = (props: object, context: object) => {
    const state = setup(props, context)
    onServerPrefetch(async () => { await action(state) })
    return state
  }
  const app = createSSRApp(component)
  app.use(i18n)
  app.component('t-button', defineComponent({ props: ['disabled'], setup: (props, { attrs, slots }) => () => h('button', { ...attrs, disabled: props.disabled }, slots.default?.()) }))
  app.component('t-alert', defineComponent({ props: ['message'], setup: (props, { attrs }) => () => h('div', attrs, props.message) }))
  const failures: unknown[] = []
  const warnings: string[] = []
  app.config.errorHandler = error => failures.push(error)
  app.config.warnHandler = warning => warnings.push(warning)
  const html = await renderToString(app)
  assert.deepEqual(failures, [])
  assert.deepEqual(warnings, [])
  return html
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  const copy = messages[locale].settings.vault
  test(`${locale}: settings render the read-only vault root and separate directory status`, async () => {
    const html = await scenario(locale, async () => ({ success: true, data: status }), state => state.load())
    for (const value of [copy.title, copy.root, copy.rootHint, copy.mine, copy.reports, copy.lastScan, copy.indexedFiles, copy.lastError, copy.noError, copy.notScanned, status.root, status.reports.last_error!]) assert.ok(html.includes(value), value)
    assert.ok(html.includes(copy.mineImportedHint))
    assert.ok(!html.includes(new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(status.mine.last_scan_at!))))
    assert.ok(html.includes('custom-mine/'))
    assert.ok(html.includes('ai-reports/'))
    assert.doesNotMatch(html, />reports\//)
    assert.match(html, /<dd>12<\/dd>/)
    assert.match(html, /<dd>3<\/dd>/)
    assert.doesNotMatch(html, /<input|<textarea/)
    assert.ok(readFileSync(new URL('./Settings.vue', import.meta.url), 'utf8').includes('currentSection === \'vault\''))
    assert.ok(readFileSync(new URL('./Settings.vue', import.meta.url), 'utf8').includes('<VaultStatus />'))
  })

  test(`${locale}: a failed refresh preserves the status and offers retry`, async () => {
    let requests = 0
    const html = await scenario(locale, async () => {
      if (++requests === 1) return { success: true, data: status }
      throw new Error('Status unavailable')
    }, async state => { await state.load(); await state.load(); assert.equal(state.loading.value, false) })
    assert.equal(requests, 2)
    assert.ok(html.includes('Status unavailable'))
    assert.ok(html.includes(messages[locale].common.retry))
    assert.ok(html.includes(status.root))
    assert.match(html, /role="alert"/)
  })

  test(`${locale}: an unsuccessful response shows a localized error without partial status`, async () => {
    const html = await scenario(locale, async () => ({ success: false }), state => state.load())
    assert.ok(html.includes(copy.loadFailed))
    assert.ok(html.includes(messages[locale].common.retry))
    assert.doesNotMatch(html, /vault-root/)
  })
}

test('vault status ignores duplicate refresh while loading', async () => {
  let finish: (value: unknown) => void = () => {}
  let requests = 0
  await scenario('en-US', () => { requests++; return new Promise(resolve => { finish = resolve }) }, async state => {
    const pending = state.load()
    assert.equal(state.loading.value, true)
    await state.load()
    assert.equal(requests, 1)
    finish({ success: true, data: status })
    await pending
    assert.equal(state.loading.value, false)
    assert.equal(state.status.value.root, status.root)
  })
})

test('vault status uses localized loading and ignores responses after teardown', async () => {
  let finish: (value: unknown) => void = () => {}
  const html = await scenario('zh-CN', () => new Promise(resolve => { finish = resolve }), async state => {
    const pending = state.load()
    state.generation++
    finish({ success: true, data: status })
    await pending
    assert.equal(state.status.value, null)
  })
  assert.ok(html.includes(messages['zh-CN'].common.loading))
  assert.match(html, /aria-busy="true"/)
})
