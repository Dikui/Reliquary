import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import axios, { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import { CONTENT_TYPES } from '../types/knowledgeClassification'
import { translatePersonalServerMessage } from '../utils/personalServerError'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zh, 'en-US': en }
const source = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')
const moduleOf = (value: unknown) => ({ __esModule: true, default: value })
function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const value = { exports: {} as any }
  new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? require(name), value, value.exports)
  return value.exports
}
function component(path: string, modules: Record<string, unknown>) {
  const filename = path.split('/').at(-1)!
  const { descriptor, errors } = parse(source(path), { filename })
  assert.deepEqual(errors, [])
  const script = compileScript(descriptor, { id: filename })
  const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const value = evaluate(script.content, modules).default
  value.ssrRender = evaluate(template.code, modules).ssrRender
  return value
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const operation of ['content-type', 'processing-review'] as const) {
    test(`${locale}: ${operation} pause travels through the real API and Axios path and preserves the displayed article`, async context => {
      const previousStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
      Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: (key: string) => key === 'reliquary_token' ? 'reliquary-user-fixture' : locale } })
      context.after(() => {
        if (previousStorage) Object.defineProperty(globalThis, 'localStorage', previousStorage)
        else delete (globalThis as any).localStorage
      })
      const calls: InternalAxiosRequestConfig[] = []
      const network = moduleOf({ create: (config: object) => axios.create({ ...config, adapter: async (packet: InternalAxiosRequestConfig) => {
        calls.push(packet)
        const response = { status: 503, statusText: 'Service Unavailable', headers: { 'retry-after': '300' }, config: packet, data: { success: false, error: {
          code: 'chatgpt_generation_paused', message: 'ChatGPT generation is paused. The article is unchanged.', details: { reason: 'subscription_sharing_usage_limit_exceeded', manual_retry_required: true },
        } } }
        throw new AxiosError('Request failed with status code 503', AxiosError.ERR_BAD_RESPONSE, packet, undefined, response)
      } }) })
      const request = evaluate(source('../utils/request.ts'), {
        axios: network,
        './index': { generateRandomString: () => 'synchronous-fixture', MAX_FILE_SIZE_MB: 100 },
        './personalServerError': { translatePersonalServerMessage },
        './api-base': { getApiBaseUrl: () => 'http://subscription-fixture.test' },
        '@/i18n': moduleOf({ global: { locale: { value: locale }, t: (key: string) => key === 'chatgpt.synchronousRetry' ? messages[locale].chatgpt.synchronousRetry : key } }),
      })
      const article = { id: 'article/quota', source_kind: 'article', content_type: 'tutorial', processing_mode: 'summary_only', parse_status: 'completed', processing_review_status: 'pending', processing_review_content_type: 'event', metadata: {}, title: 'Preserved article' }
      const before = structuredClone(article)
      const modules = {
        '@/types/knowledgeClassification': { CONTENT_TYPES }, 'tdesign-vue-next': { MessagePlugin: { success() { assert.fail('a paused correction cannot report success') } } },
        '@/utils/security': { isValidURL: () => true },
        '@/api/topics': { listTopics: async () => ({ success: true, data: [] }) },
        '@/api/knowledge-base/index': { getKnowledgeDetails: async () => ({ success: true, data: article }) },
        './KnowledgeStarToggle.vue': moduleOf(defineComponent({ setup: () => () => null })),
        '@/api/knowledge-judgments': evaluate(source('../api/knowledge-judgments.ts'), { '@/utils/request': request }),
        '@/api/knowledge-review': evaluate(source('../api/knowledge-review.ts'), { '@/utils/request': request }),
      }
      const view = component(operation === 'content-type' ? './KnowledgeArticleJudgments.vue' : './KnowledgeReviewActions.vue', modules)
      const setup = view.setup
      view.setup = (props: object, ctx: object) => {
        const state = setup(props, ctx)
        onServerPrefetch(async () => {
          state.typeDraft.value = 'research'
          if (operation === 'content-type') await state.saveType()
          else await state.submit('correct')
          assert.deepEqual(article, before)
        })
        return state
      }
      let emittedChanges = 0
      const app = createSSRApp(view, { knowledge: article, onUpdated: () => emittedChanges++, onChanged: () => emittedChanges++ })
      app.use(createI18n({ legacy: false, locale, messages }))
      const stub = defineComponent({ props: ['visible'], setup: (props, { attrs, slots }) => () => props.visible === false ? null : h('div', attrs, slots.default?.()) })
      for (const name of ['t-button', 't-select', 't-input', 't-dialog']) app.component(name, stub)
      const errors: unknown[] = [], warnings: string[] = []
      app.config.errorHandler = error => errors.push(error)
      app.config.warnHandler = warning => warnings.push(warning)
      const html = await renderToString(app)
      assert.deepEqual(errors, []); assert.deepEqual(warnings, [])
      assert.ok(html.includes(messages[locale].chatgpt.synchronousRetry))
      assert.equal(emittedChanges, 0)
      assert.equal(calls.length, 1)
      assert.equal(calls[0].url, `/api/v1/knowledge/article%2Fquota/${operation}`)
      assert.equal(calls[0].method, operation === 'content-type' ? 'put' : 'post')
      assert.equal(calls[0].headers.Authorization, 'Bearer reliquary-user-fixture')
      assert.equal(calls[0].headers['Accept-Language'], locale)
      assert.deepEqual(JSON.parse(calls[0].data), { ...(operation === 'processing-review' ? { action: 'correct' } : {}), content_type: 'research', confirm_remove_body: false })
    })
  }
}
