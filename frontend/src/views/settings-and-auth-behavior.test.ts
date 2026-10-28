import { settingsTestUi } from './settings/settingsTestUi'
import assert from 'node:assert/strict'
import { safeTextareaTestModule } from '../components/__tests__/safeTextareaStub'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { createSSRApp, defineComponent, h, onServerPrefetch, ref } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import * as personalError from '../utils/personalServerError'
import * as searchHelpers from '../utils/search'
import * as workspaceCopy from '../utils/workspaceCopy'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../utils/sfcStyleTestHelpers'

const require = createRequire(import.meta.url)
function translator(locale: 'zh-CN' | 'en-US') {
  return createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } }).global.t
}
const stub = defineComponent({ setup(_, { attrs, slots }) { return () => h('div', attrs, slots.default?.()) } })
async function componentScenario(path: string, locale: 'zh-CN' | 'en-US', modules: Record<string, unknown>, scenario: (state: any) => Promise<void>, props = {}, drawer = stub) {
  const filename = path.split('/').at(-1)!
  const { descriptor } = parse(readFileSync(new URL(path, import.meta.url), 'utf8'), { filename })
  const script = compileScript(descriptor, { id: 'personal-fix-test' })
  const template = compileTemplate({ id: 'personal-fix-test', filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const t = translator(locale)
  const mocks: Record<string, unknown> = { '@/components/ui': settingsTestUi(),
    '@/components/SafeTextarea.vue': safeTextareaTestModule,
    '@/components/SearchFilters.vue': { default: stub, __esModule: true },
    '@/utils/search': searchHelpers,
    '@/api/search': { getSearchOptions: async () => ({ success: true, data: { entities: [], topics: [] } }) },
    'vue-i18n': { useI18n: () => ({ t, tm: () => [], locale: ref(locale) }) },
    '@/utils/personalServerError': personalError,
    '@/stores/auth': { useAuthStore: () => ({ currentSpaceId: '1', isLoggedIn: false }) },
    '@/components/settings/SettingDrawer.vue': { default: stub, __esModule: true },
    '@/utils/api-base': { getApiBaseUrl: () => 'http://localhost' },
    '@/utils/clipboard': { copyWithToast() {} },
    '@/utils/workspaceCopy': workspaceCopy,
    'vue-router': { useRouter: () => ({ replace() {}, push() {} }) },
    ...modules,
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    new Function('require', 'module', 'exports', ts.transpileModule(code.replaceAll('__FRONTEND_VERSION__', "'test'").replaceAll('__FRONTEND_COMMIT__', "'test'"), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText)(
      (name: string) => name.endsWith('.css') ? {} : name.endsWith('.svg') ? { default: '', __esModule: true } : mocks[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  let state: any
  component.setup = (props: object, ctx: object) => {
    state = setup(props, ctx)
    onServerPrefetch(() => scenario(state))
    return state
  }
  const app = createSSRApp(component, props)
  app.config.globalProperties.$t = t
  for (const name of ['t-form', 't-form-item', 't-input', 't-button', 't-icon', 't-alert', 't-empty', 't-loading', 't-table', 't-tooltip', 't-textarea', 't-tag']) app.component(name, stub)
  app.component('t-drawer', drawer)
  const errors: unknown[] = []
  app.config.errorHandler = error => errors.push(error)
  const html = await renderToString(app)
  assert.deepEqual(errors, [])
  return { state, html }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: API Key blank expiry creates a permanent key without sending epoch zero`, async () => {
    const requests: any[] = []
    const { state } = await componentScenario('./integrations/ApiIntegrationSettings.vue', locale, {
      'tdesign-vue-next': { MessagePlugin: { success() {} } },
      '@/api/library': { listLibraries: async () => ({ success: true, data: [] }) },
      '@/api/space': {
        createAPIKey: async (payload: object) => { requests.push({ payload }); return { success: true, data: { token: 'test-key' } } },
        listAPIKeys: async () => ({ success: true, data: [] }),
      },
    }, async state => {
      state.openCreate()
      state.form.name = 'Personal automation'
      await state.save()
      assert.equal(state.formError.value, '')
      assert.equal(state.createdToken.value, 'test-key')
      assert.equal(state.editingVisible.value, false)
    })
    assert.deepEqual(requests, [{ payload: { name: 'Personal automation' } }])
    assert.equal(state.columns.value.at(-1).label, locale === 'zh-CN' ? '操作' : 'Actions')
    const t = translator(locale)
    assert.equal(personalError.apiKeyErrorMessage({ message: 'expires_at_unix must be in the future' }, t, 'integrations.api.createApiKeyFailed'), t('integrations.api.apiKeyExpiresAtInvalid'))
    assert.equal(personalError.apiKeyErrorMessage({ message: 'internal backend error' }, t, 'integrations.api.createApiKeyFailed'), t('integrations.api.createApiKeyFailed'))
  })

}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: space info shows used storage above the former limit without a quota or percentage`, async () => {
    const { html, state } = await componentScenario('./settings/SpaceInfo.vue', locale, {
      './GlobalModelTierSettings.vue': { default: stub, __esModule: true },
      'tdesign-vue-next': { MessagePlugin: { success() {} } },
      '@/api/space': { getSpaceInfo: async () => ({ success: true, data: { id: 1, name: 'Personal space', storage_used: 11 * 1024 ** 3, created_at: '2026-10-09T00:00:00Z' } }) },
    }, async state => { await state.loadInfo() })
    assert.equal(state.error.value, '')
    assert.ok(html.includes(translator(locale)('space.storage.usedLabel')))
    assert.ok(html.includes('11 GB'))
    assert.ok(!html.includes('%'))
    assert.ok(!html.includes(locale === 'zh-CN' ? '配额' : 'Quota'))
    assert.ok(html.includes(translator(locale)('space.details.editName')))
  })

  test(`${locale}: default space copy is localized while unchanged edits retain server values`, async () => {
    const updates: unknown[] = []
    const { html, state } = await componentScenario('./settings/SpaceInfo.vue', locale, {
      'tdesign-vue-next': { MessagePlugin: { success() {} } },
      '@/api/space': {
        getSpaceInfo: async () => ({ success: true, data: { id: 1, name: 'Personal workspace', description: 'Personal knowledge' } }),
        updateSpace: async (input: unknown) => { updates.push(input); return { success: true } },
      },
    }, async state => {
      await state.loadInfo()
      state.startEditName()
      assert.equal(state.canSubmit.value, false)
      await state.saveSpaceName()
      state.startEditDescription()
      assert.equal(state.canSubmitDescription.value, false)
      await state.saveSpaceDescription()
    })
    assert.ok(html.includes(translator(locale)('space.defaultName')))
    assert.ok(html.includes(translator(locale)('space.defaultDescription')))
    if (locale === 'zh-CN') assert.doesNotMatch(html, /Personal workspace|Personal entry/)
    assert.equal(state.spaceInfo.value.name, 'Personal workspace')
    assert.equal(state.spaceInfo.value.description, 'Personal knowledge')
    assert.deepEqual(updates, [])
  })

  for (const encrypted of [false, true]) {
    test(`${locale}: system info displays ${encrypted ? 'encrypted' : 'plaintext'} credential storage`, async () => {
      const { html } = await componentScenario('./settings/SystemInfo.vue', locale, {
        '@/api/system': { getSystemInfo: async () => ({ data: { version: 'test', credential_encryption_enabled: encrypted } }) },
      }, async state => { await state.loadInfo() })
      const t = translator(locale)
      assert.ok(html.includes(t(encrypted ? 'system.credentialEncryptionEnabled' : 'system.credentialEncryptionDisabled')))
      assert.ok(html.includes(t(encrypted ? 'system.credentialEncryptionEnabledDescription' : 'system.credentialEncryptionDisabledDescription')))
      if (encrypted) assert.ok(!html.includes(t('system.credentialEncryptionDisabledDescription')))
    })
  }
}
