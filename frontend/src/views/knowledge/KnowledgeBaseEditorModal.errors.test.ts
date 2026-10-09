import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { createSSRApp, onServerPrefetch } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zhCN from '../../i18n/locales/zh-CN'
import enUS from '../../i18n/locales/en-US'
import * as personalServerError from '../../utils/personalServerError'
import * as modelTiers from '../../utils/modelTiers'

const require = createRequire(import.meta.url)
const expectedMessages: Record<'zh-CN' | 'en-US', Record<number, string>> = {
  'zh-CN': {
    2301: '图片处理模型必须是 VLLM 模型或已开启支持视觉/多模态的对话模型。',
    2302: '图片处理模型不存在，请重新选择。',
  },
  'en-US': {
    2301: 'Image processing requires a VLLM model or a conversation model with vision or multimodal support enabled.',
    2302: 'The image processing model was not found. Select another model.',
  },
}
function evaluate(source: string, modules: Record<string, unknown>) {
  const module = { exports: {} as any }
  const code = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true },
  }).outputText
  new Function('require', 'module', 'exports', code)(
    (name: string) => {
      if (Object.hasOwn(modules, name)) return modules[name]
      if (name.endsWith('.vue')) return { __esModule: true, default: {} }
      return require(name)
    }, module, module.exports,
  )
  return module.exports
}

function realRequestAndAPI(locale: 'zh-CN' | 'en-US', code: number) {
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const rawMessage = code === 2301 ? '图片处理模型必须是 VLLM 模型或已开启支持视觉/多模态的对话模型' : '图片处理模型不存在'
  let rejectResponse: (error: any) => Promise<never>
  const requests: Array<{ url: string; config: any }> = []
  const instance = {
    interceptors: {
      request: { use() {} },
      response: { use(_success: unknown, failure: typeof rejectResponse) { rejectResponse = failure } },
    },
    put(url: string, config: unknown) {
      requests.push({ url, config })
      return rejectResponse({ config: { url }, response: {
        status: 400, data: { success: false, error: { code, message: rawMessage, details: { model_id: 'selected-image-model' } } },
      } })
    },
    post(url: string, config: unknown) { return this.put(url, config) },
  }
  const request = evaluate(readFileSync(new URL('../../utils/request.ts', import.meta.url), 'utf8'), {
    axios: { create: () => instance },
    './index': { generateRandomString: () => 'image-error-test', MAX_FILE_SIZE_MB: 50 },
    '@/i18n': i18n, './personalServerError': personalServerError,
    './api-base': { getApiBaseUrl: () => '/api/v1' },
  })
  const api = evaluate(readFileSync(new URL('../../api/initialization/index.ts', import.meta.url), 'utf8'), {
    '../../utils/request': request, '@/i18n': i18n,
  })
  return { i18n, api, requests, rawMessage }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const code of [2301, 2302]) {
    test(`${locale}: settings save displays localized ${code} through interceptor, updateKBConfig and actual component`, async () => {
      const { i18n, api, requests, rawMessage } = realRequestAndAPI(locale, code)
      const notifications: string[] = []
      const { descriptor } = parse(readFileSync(new URL('./KnowledgeBaseEditorModal.vue', import.meta.url), 'utf8'))
      const script = compileScript(descriptor, { id: 'kb-error-chain-test' })
      const component = evaluate(script.content, {
        '@/api/initialization': api,
        '@/api/knowledge-base': { updateKnowledgeBase: async () => ({ success: true }) },
        '@/api/knowledge-domains': {},
        '@/stores/auth': { useAuthStore: () => ({ isLoggedIn: true }) },
        '@/stores/ui': { useUIStore: () => ({}) },
        '@/stores/chatResources': { useChatResourcesStore: () => ({ allModels: [] }) },
        '@/stores/editorResources': { useEditorResourcesStore: () => ({}) },
        '@/config/contextualGuides': {}, '@/utils/modelDefaults': {}, '@/utils/clipboard': {},
        '@/utils/modelTiers': modelTiers,
        'tdesign-vue-next': { MessagePlugin: {
          error: (message: string) => notifications.push(message),
          success: () => assert.fail('rejected settings must not show success'),
          warning: () => assert.fail('fixture must pass form validation'),
        }, DialogPlugin: {} },
      }).default
      const originalSetup = component.setup
      const events: string[] = []
      component.setup = (props: object, context: any) => {
        const state = originalSetup(props, context)
        onServerPrefetch(async () => {
          state.formData.value = state.initFormData()
          Object.assign(state.formData.value, { name: 'Vision KB', domainId: 'domain' })
          Object.assign(state.formData.value.modelConfig, { llmModelId: 'chat', embeddingModelId: 'embedding' })
          Object.assign(state.formData.value.multimodalConfig, { enabled: true, vllmModelId: 'selected-image-model' })
          assert.equal(state.validateForm(), true)
          await state.doSubmit()
          assert.equal(state.saving.value, false)
        })
        return state
      }
      component.render = () => null
      const app = createSSRApp(component, { visible: false, mode: 'edit', kbId: 'kb',
        onSuccess: () => events.push('success'), 'onUpdate:visible': () => events.push('close'),
      })
      app.use(i18n)
      await renderToString(app)
      const expected = expectedMessages[locale][code]
      assert.deepEqual(notifications, [expected])
      assert.notEqual(expected, rawMessage)
      assert.deepEqual(events, [], 'failed save keeps the editor open')
      assert.equal(requests.length, 1)
      assert.equal(requests[0].url, '/api/v1/initialization/config/kb')
      assert.equal(requests[0].config.vlm_config.model_id, 'selected-image-model')
    })

    test(`${locale}: legacy initialization also preserves translated message and structured error ${code}`, async () => {
      const { api, i18n, rawMessage } = realRequestAndAPI(locale, code)
      await assert.rejects(api.initializeSystemByKB('kb', {}), (error: any) => {
        const key = code === 2301 ? 'model.imageProcessingUnsupported' : 'model.imageProcessingNotFound'
        assert.equal(error.message, expectedMessages[locale][code])
        assert.equal(error.message, i18n.global.t(key))
        assert.notEqual(error.message, rawMessage)
        assert.equal(error.status, 400)
        assert.equal(error.error.code, code)
        assert.equal(error.error.details.model_id, 'selected-image-model')
        return true
      })
    })
  }
}
