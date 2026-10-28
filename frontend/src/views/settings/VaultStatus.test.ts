import { settingsTestUi } from './settingsTestUi'
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
  libraries: [
    ...['official', 'media', 'references', 'personal', 'ai-output'].map((role, index) => ({
      library_id: role, name: ['官方信息', '媒体信息', '参考文档', '个人产出', 'AI产出'][index]!,
      system_role: role, directory: role, archived_entries: index === 3 ? 12 : index,
      ...(role === 'ai-output' ? { last_error: 'Cannot read ai-output/example.md' } : {}),
    })),
    { library_id: 'custom', name: 'Custom library', system_role: '', directory: 'custom-library', archived_entries: 3 },
  ],
}

async function scenario(locale: keyof typeof messages, getVaultStatus: () => Promise<unknown>, action: (state: any) => Promise<void>, api: Record<string, unknown> = {}, dialog: (options: any) => void = () => {}) {
  const i18n = createI18n({ legacy: false, locale, messages })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
    '@/api/vault': { getVaultStatus, getVaultFileRebuildStatus: async () => ({ success: true, data: { status: 'idle' } }), ...api },
    '@/components/ui': settingsTestUi({ confirm: (options: any) => { dialog(options); return { destroy() {} } } }),
    './MaintenanceStatus.vue': { __esModule: true, default: defineComponent({ setup: () => () => h('section', { 'data-maintenance-status': true }) }) },
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', compiled)((name: string) => modules[name] ?? (name === '@/styles/settings-form.css' ? {} : require(name)), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  let captured: any
  const setup = component.setup
  component.setup = (props: object, context: object) => {
    const state = setup(props, context)
    captured = state
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
  clearTimeout(captured?.pollTimer)
  assert.deepEqual(failures, [])
  assert.deepEqual(warnings, [])
  return html
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  const copy = messages[locale].settings.vault
  test(`${locale}: settings render every library archive, including empty and custom libraries`, async () => {
    const html = await scenario(locale, async () => ({ success: true, data: status }), state => state.load())
    for (const value of [copy.root, copy.rootHint, copy.archiveHint, copy.lastScan, copy.archivedEntries, copy.lastError, copy.notScanned, status.root, 'Cannot read ai-output/example.md']) assert.ok(html.includes(value), value)
    for (const library of status.libraries) {
      assert.ok(html.includes(library.name), library.name)
      assert.ok(html.includes(`${library.directory}/`), library.directory)
    }
    assert.match(html, /<dd>0<\/dd>/)
    assert.match(html, /<dd>12<\/dd>/)
    assert.match(html, /<dd>3<\/dd>/)
    assert.doesNotMatch(html, /<input|<textarea/)
    assert.ok(readFileSync(new URL('./Settings.vue', import.meta.url), 'utf8').includes('currentSection === \'vault\''))
    assert.match(readFileSync(new URL('./Settings.vue', import.meta.url), 'utf8'), /<VaultStatus\b/)
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

for (const locale of ['zh-CN', 'en-US'] as const) {
  const copy = messages[locale].settings.vault
  test(`${locale}: rebuild requires confirmation and cancellation does not submit`, async () => {
    let confirmation: any
    let calls = 0
    await scenario(locale, async () => ({ success: true, data: status }), async state => {
      state.confirmRebuild()
      assert.equal(confirmation.body, copy.rebuildConfirm)
      assert.equal(calls, 0)
      confirmation.onCancel()
      assert.equal(state.confirming.value, false)
      state.confirmRebuild()
      confirmation.onConfirm()
      await Promise.resolve()
      assert.equal(calls, 1)
    }, { rebuildVaultFiles: async () => { calls++; return { success: true, data: { status: 'running' } } } }, options => { confirmation = options })
  })

  test(`${locale}: rebuild disables the button while running and rejects duplicate starts`, async () => {
    let calls = 0
    const html = await scenario(locale, async () => ({ success: true, data: status }), async state => {
      await state.startRebuild()
      await state.startRebuild()
      state.confirmRebuild()
      assert.equal(calls, 1)
      assert.equal(state.rebuilding.value, true)
    }, { rebuildVaultFiles: async () => { calls++; return { success: true, data: { status: 'running' } } } })
    assert.ok(html.includes(copy.rebuildRunning))
    assert.match(html, /<button[^>]*disabled[^>]*>[\s\S]*?Export library|<button[^>]*disabled[^>]*>[\s\S]*?导出文件库/)
  })

  test(`${locale}: polling renders completed counts and refreshes directory status`, async () => {
    const html = await scenario(locale, async () => ({ success: true, data: status }), async state => {
      state.rebuilding.value = true
      await state.pollRebuild()
      assert.equal(state.rebuilding.value, false)
    }, { getVaultFileRebuildStatus: async () => ({ success: true, data: { status: 'success', result: { rebuilt: true, written: 3, skipped: 4, failed: 0 } } }) })
    assert.ok(html.includes(copy.rebuildComplete))
    assert.ok(html.includes('3'))
    assert.ok(html.includes('4'))
    assert.ok(html.includes(locale === 'en-US' ? 'failed: 0' : '失败 0'))
  })

  test(`${locale}: failed run renders its error and partial counts`, async () => {
    const html = await scenario(locale, async () => ({ success: true, data: status }), async state => {
      await state.pollRebuild()
      assert.equal(state.rebuilding.value, false)
    }, { getVaultFileRebuildStatus: async () => ({ success: true, data: { status: 'failed', error: 'Disk is full', result: { rebuilt: false, written: 2, skipped: 0, failed: 1 } } }) })
    assert.ok(html.includes('Disk is full'))
    assert.ok(html.includes(copy.rebuildFailed))
    assert.match(html, /role="alert"/)
  })

  test(`${locale}: failed start shows an error and enables retry`, async () => {
    const html = await scenario(locale, async () => ({ success: true, data: status }), async state => {
      await state.startRebuild()
      assert.equal(state.rebuilding.value, false)
    }, { rebuildVaultFiles: async () => { throw new Error('Request failed') } })
    assert.ok(html.includes('Request failed'))
    assert.match(html, /role="alert"/)
  })
}

test('an initial status response cannot replace a newly accepted rebuild', async () => {
  let finish: (value: unknown) => void = () => {}
  await scenario('en-US', async () => ({ success: true, data: status }), async state => {
    const pending = state.pollRebuild()
    await state.startRebuild()
    finish({ success: true, data: { status: 'idle' } })
    await pending
    assert.equal(state.rebuilding.value, true)
    assert.equal(state.rebuild.value.status, 'running')
  }, {
    getVaultFileRebuildStatus: () => new Promise(resolve => { finish = resolve }),
    rebuildVaultFiles: async () => ({ success: true, data: { status: 'running' } }),
  })
})

test('polling failure retains running state and shows an error', async () => {
  const html = await scenario('en-US', async () => ({ success: true, data: status }), async state => {
    state.rebuilding.value = true
    await state.pollRebuild()
    assert.equal(state.rebuilding.value, true)
  }, { getVaultFileRebuildStatus: async () => { throw new Error('Connection interrupted') } })
  assert.ok(html.includes('Connection interrupted'))
  assert.ok(html.includes(messages['en-US'].settings.vault.rebuildRunning))
})

test('reopening the page discovers a running rebuild and disables its button', async () => {
  const html = await scenario('en-US', async () => ({ success: true, data: status }), async state => {
    await state.pollRebuild()
    assert.equal(state.rebuilding.value, true)
  }, { getVaultFileRebuildStatus: async () => ({ success: true, data: { status: 'running' } }) })
  assert.match(html, /<button[^>]*disabled[^>]*>[\s\S]*?Export library/)
})

test('refresh during submission cannot cancel observation of the accepted task', async () => {
  let finish: (value: unknown) => void = () => {}
  let polls = 0
  await scenario('en-US', async () => ({ success: true, data: status }), async state => {
    const pending = state.startRebuild()
    state.refresh()
    assert.equal(polls, 0)
    finish({ success: true, data: { status: 'running' } })
    await pending
    assert.equal(state.rebuilding.value, true)
  }, {
    rebuildVaultFiles: () => new Promise(resolve => { finish = resolve }),
    getVaultFileRebuildStatus: async () => { polls++; return { success: true, data: { status: 'idle' } } },
  })
})
