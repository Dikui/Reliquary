import assert from 'node:assert/strict'
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
  const mocks: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => ({ t, tm: () => [], locale: ref(locale) }) },
    '@/utils/personalServerError': personalError,
    '@/stores/auth': { useAuthStore: () => ({ currentTenantId: '1', isLoggedIn: false }) },
    '@/components/settings/SettingDrawer.vue': { default: stub, __esModule: true },
    '@/utils/api-base': { getApiBaseUrl: () => 'http://localhost' },
    '@/utils/clipboard': { copyWithToast() {} },
    '@/utils/loginNotify': { notifyLoginSuccess() {} },
    '@/utils/passwordValidation': { newPasswordRules: () => [] },
    'vue-router': { useRouter: () => ({ replace() {}, push() {} }) },
    'swiper/vue': { Swiper: stub, SwiperSlide: stub },
    'swiper/modules': {},
    ...modules,
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    new Function('require', 'module', 'exports', ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText)(
      (name: string) => (name.endsWith('.css') || name.startsWith('swiper/css')) ? {} : name.endsWith('.svg') ? { default: '', __esModule: true } : mocks[name] ?? require(name), module, module.exports)
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
  for (const name of ['t-form', 't-form-item', 't-input', 't-button', 't-icon', 't-alert', 't-empty', 't-loading', 't-table', 't-tooltip', 't-textarea']) app.component(name, stub)
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
      '@/api/tenant': {
        createTenantAPIKey: async (tenant: number, payload: object) => { requests.push({ tenant, payload }); return { success: true, data: { token: 'test-key' } } },
        listTenantAPIKeys: async () => ({ success: true, data: [] }),
      },
    }, async state => {
      state.openCreate()
      state.formRef.value = { validate: async () => true }
      state.form.name = 'Personal automation'
      await state.save()
      assert.equal(state.formError.value, '')
      assert.equal(state.createdToken.value, 'test-key')
      assert.equal(state.editingVisible.value, false)
    })
    assert.deepEqual(requests, [{ tenant: 1, payload: { name: 'Personal automation' } }])
    assert.equal(state.columns.value.at(-1).title, locale === 'zh-CN' ? '操作' : 'Actions')
    const t = translator(locale)
    assert.equal(personalError.apiKeyErrorMessage({ message: 'expires_at_unix must be in the future' }, t, 'integrations.api.createApiKeyFailed'), t('integrations.api.apiKeyExpiresAtInvalid'))
    assert.equal(personalError.apiKeyErrorMessage({ message: 'internal backend error' }, t, 'integrations.api.createApiKeyFailed'), t('integrations.api.createApiKeyFailed'))
  })

  test(`${locale}: registration-closed response uses localized copy in the actual form`, async () => {
    const messages: string[] = []
    await componentScenario('./auth/Login.vue', locale, {
      'tdesign-vue-next': { MessagePlugin: { error: (message: string) => messages.push(message) } },
      '@/api/auth': { register: async () => { throw { status: 403, message: 'Registration is closed' } } },
    }, async state => {
      state.registrationEnabled.value = true
      state.registerFormRef.value = { validate: async () => true }
      await state.handleRegister()
    })
    assert.deepEqual(messages, [locale === 'zh-CN' ? '已有用户，注册已关闭。请登录现有账户。' : 'Registration is closed. Please sign in with the existing account.'])
    assert.notEqual(messages[0], 'Registration is closed')
  })

  test(`${locale}: read-only synchronization history exposes one close button and closes its model`, async () => {
    const updates: boolean[] = []
    const drawer = defineComponent({ props: ['confirmBtn', 'cancelBtn'], setup(props, { attrs, slots }) {
      onServerPrefetch(() => {
        assert.equal(props.confirmBtn, null)
        assert.equal(props.cancelBtn, translator(locale)('common.close'))
        ;(attrs.onCancel as Function)()
      })
      return () => h('div', slots.default?.())
    } })
    await componentScenario('./knowledge/settings/DataSourceSyncLogs.vue', locale, { '@/api/datasource': {} }, async () => {}, {
      dataSourceId: 'rss', visible: true, 'onUpdate:visible': (value: boolean) => updates.push(value),
    }, drawer)
    assert.deepEqual(updates, [false])
  })
}

test('390px login navigation reserves its own normal-flow row before the introduction', async () => {
  const rules = await compiledSFCStyleRules(new URL('./auth/Login.vue', import.meta.url))
  assert.equal(styleDeclarationsAtWidth(rules, '.login-header', 390).position, 'relative')
  assert.equal(styleDeclarationsAtWidth(rules, '.login-header', 390).display, 'flex')
  assert.equal(styleDeclarationsAtWidth(rules, '.header-logo', 390).position, 'static')
  assert.equal(styleDeclarationsAtWidth(rules, '.header-links', 390).position, 'static')
  assert.equal(styleDeclarationsAtWidth(rules, '.header-logo', 1280).position, 'fixed')
})
