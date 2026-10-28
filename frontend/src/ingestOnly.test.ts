import assert from 'node:assert/strict'
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from './i18n/locales/zh-CN'
import en from './i18n/locales/en-US'
import * as settingsRoute from './config/settingsRoute'
import * as settingsAccess from './config/settingsAccess'
import * as integrations from './config/integrations'
import * as capabilities from './config/deploymentCapabilities'

const require = createRequire(import.meta.url)
const sourceRoot = dirname(fileURLToPath(import.meta.url))
const stub = defineComponent({ setup: (_props, { attrs, slots }) => () => h('div', attrs, slots.default?.()) })
async function render(path: string, locale: 'zh-CN' | 'en-US', modules: Record<string, unknown> = {}, props = {}, scenario: (state: any) => void = () => {}) {
  const filename = join(sourceRoot, path)
  const source = readFileSync(filename, 'utf8')
  const { descriptor, errors } = parse(source, { filename })
  assert.deepEqual(errors, [])
  const script = compileScript(descriptor, { id: path })
  const template = compileTemplate({ filename, id: path, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const mocks: Record<string, unknown> = { ...modules }
  for (const match of source.matchAll(/from\s+['"]([^'"]+\.vue)['"]/g)) mocks[match[1]] ??= { __esModule: true, default: stub }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', output)((name: string) => mocks[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  component.setup = (props: object, context: object) => {
    const state = setup(props, context)
    onServerPrefetch(() => scenario(state))
    return state
  }
  const app = createSSRApp(component, props)
  app.use(createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } }))
  for (const name of ['t-icon', 't-button', 't-select', 't-switch', 't-input-number']) app.component(name, stub)
  const caught: unknown[] = []
  app.config.errorHandler = error => caught.push(error)
  const context: { teleports?: Record<string, string> } = {}
  const html = await renderToString(app, context)
  assert.deepEqual(caught, [])
  return html + Object.values(context.teleports || {}).join('')
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: the article empty state directs external tools to the ingest interface`, async () => {
    const html = await render('components/EmptyEntry.vue', locale)
    const copy = locale === 'zh-CN' ? zh : en
    assert.ok(html.includes(copy.library.emptyArticles))
    assert.ok(html.includes(copy.library.ingestOnlyHint))
    assert.doesNotMatch(html, /type="file"|dragenter|upload|manual-editor/i)
  })

  test(`${locale}: the actual settings navigation excludes collection and canonicalizes the old datasource bookmark`, async () => {
    const replacements: unknown[] = []
    const html = await render('views/settings/Settings.vue', locale, {
      'vue-router': { RouterLink: stub, onBeforeRouteLeave() {}, onBeforeRouteUpdate() {}, useRoute: () => ({ path: '/platform/settings', query: { section: 'datasource' } }), useRouter: () => ({ replace: (value: unknown) => replacements.push(value), beforeEach: () => () => {} }) },
      '@/components/ui': { toast: { warning: () => assert.fail('Retired sections use the default settings page') } },
      '@/stores/deploymentCapabilities': { useDeploymentCapabilitiesStore: () => ({ loaded: true, isSupported: () => true }) },
      '@/config/settingsRoute': settingsRoute,
      '@/config/settingsAccess': settingsAccess,
      '@/config/integrations': integrations,
      '@/config/deploymentCapabilities': capabilities,
      'tdesign-vue-next': { MessagePlugin: { warning: () => assert.fail('Retired sections use the default settings page') } },
    }, {}, state => {
      assert.equal(state.currentSection.value, 'general')
      const keys = state.navGroups.value.flatMap((group: { items: string[] }) => group.items)
      for (const key of ['library', 'vault', 'models', 'operation-logs', 'integration-api']) assert.ok(keys.includes(key))
      for (const key of ['datasource', 'sync-logs', 'import', 'upload', 'manual']) assert.equal(keys.includes(key), false)
    })
    assert.ok(replacements.some(value => JSON.stringify(value) === JSON.stringify({ path: '/platform/settings', query: { section: 'general' } })))
    assert.doesNotMatch(html, /DataSource|data-source|订阅源|Subscriptions/)
  })


}

function productionFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return productionFiles(path)
    return /\.(vue|ts)$/.test(entry.name) && !entry.name.endsWith('.test.ts') && !path.includes('/__tests__/') ? [path] : []
  })
}
test('production frontend code has no references to removed intake modules or APIs', () => {
  const retired = /DataSourceSettings|DataSourceSyncLogs|DataSourceEditorDialog|DataSourceTypeIcon|datasourceIcons|api\/datasource|cronHumanize|UploadConfirmDialog|UploadConfirmHost|useUploadConfirmStore|KbUploadSourceDropdown|ManualEntryEditor|openManualEditor|updateManualEntry|createManualEntry|createEntryFromURL|uploadEntryFile|ResolveRemoteImages|uploadSources|collectDroppedFiles|entryFileUploaded|openURLImportDialog|reliquary:entry-file-drop|datasource:sync|data_source_id|sync_log_id|datasource\.connector/
  const failures = productionFiles(sourceRoot).flatMap(path => retired.test(readFileSync(path, 'utf8')) ? [relative(sourceRoot, path)] : [])
  assert.deepEqual(failures, [])
  for (const name of ['datasource', 'uploadConfirm', 'manualEditor', 'upload']) {
    for (const bundle of [zh, en]) assert.equal(name in bundle, false, name)
  }
  for (const path of ['api/datasource.ts', 'components/ManualEntryEditor.vue', 'components/UploadMask.vue', 'views/library/components/KbUploadSourceDropdown.vue', 'views/library/settings/DataSourceSettings.vue']) assert.equal(existsSync(join(sourceRoot, path)), false, path)
})
