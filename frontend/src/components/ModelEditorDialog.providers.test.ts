import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import enUS from '../i18n/locales/en-US'
import zhCN from '../i18n/locales/zh-CN'
import { DEFAULT_MODEL_CONTEXT_WINDOW } from '../utils/contextWindow'
import { defaultThinkingControl, resolveThinkingControl } from '../utils/thinkingControl'
import { shouldShowOllamaUnavailableTip } from './modelEditorSourceState'
import type { ModelProviderOption } from '../api/initialization'

const require = createRequire(import.meta.url)
const filename = 'ModelEditorDialog.vue'
const { descriptor } = parse(readFileSync(new URL('./ModelEditorDialog.vue', import.meta.url), 'utf8'), { filename })
const script = compileScript(descriptor, { id: filename })
const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
assert.deepEqual(template.errors, [])

// Simulates the server's preset response, including its per-type filtering.
const presetIds = ['generic', 'openai', 'anthropic', 'deepseek', 'aliyun', 'zhipu', 'moonshot', 'siliconflow', 'volcengine', 'openrouter', 'gemini', 'ollama']
const rerankIds = ['generic', 'aliyun', 'siliconflow']
const presets: ModelProviderOption[] = presetIds.map(value => ({
  value,
  label: `API ${value}`,
  description: `API description for ${value}`,
  defaultUrls: { chat: `https://models.example/${value}`, embedding: `https://models.example/${value}` },
  modelTypes: ['chat', ...(rerankIds.includes(value) ? ['rerank'] : []), ...(value === 'aliyun' ? ['embedding'] : [])],
}))
presets.find(preset => preset.value === 'aliyun')!.defaultUrls.embedding = 'https://dashscope.aliyuncs.com/compatible-mode/v1'

async function renderEditor({ locale = 'zh-CN', modelType = 'chat', modelData = null, testEmbedding = async () => ({ available: true, dimension: 512 }), listProviders = async (type: string) => presets.filter(preset => preset.modelTypes.includes(type)), scenario = async () => {} }: {
  locale?: 'zh-CN' | 'en-US'
  modelType?: 'chat' | 'embedding' | 'rerank'
  modelData?: any
  testEmbedding?: (payload: any) => Promise<{ available: boolean; dimension?: number }>
  listProviders?: (type: string) => Promise<ModelProviderOption[]>
  scenario?: (state: any) => Promise<void>
} = {}) {
  const requests: string[] = []
  const confirmed: any[] = []
  const chatgptRequests: string[] = []
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const stub = defineComponent({ setup: (_, { attrs, slots }) => () => h('div', attrs, slots.default?.()) })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
    'tdesign-vue-next': { MessagePlugin: { error() {}, success() {}, warning() {}, info() {} }, DialogPlugin: {} },
    '@/api/initialization': {
      listModelProviders: (type: string) => { requests.push(type); return listProviders(type) },
      listOllamaModels: async () => [{ name: 'qwen3:8b', size: 5 * 1024 * 1024 * 1024, digest: 'local-model', modified_at: '2026-10-10T00:00:00Z' }],
      testEmbeddingModel: testEmbedding,
    },
    '@/api/model': {},
    '@/utils/request': { get: async (path: string) => {
      chatgptRequests.push(path)
      return { success: true, data: path.endsWith('/accounts') ? { accounts: [{ id: 'chatgpt-account', email: 'user@example.com', subject: 'subject', client_id: 'oaiapp_workspace_a', status: 'connected', scopes: [], expires_at: '', welcome_acknowledged: true }], encryption_ready: true, callback_url: 'http://127.0.0.1:8080/auth/chatgpt/callback' } : [{ slug: 'chatgpt-model-slug', display_name: 'Account model' }] }
    } },
    '@/stores/ui': { useUIStore: () => ({}) },
    '@/utils/thinkingControl': { defaultThinkingControl, resolveThinkingControl },
    '@/utils/contextWindow': { DEFAULT_MODEL_CONTEXT_WINDOW },
    '@/components/modelEditorSourceState': { shouldShowOllamaUnavailableTip },
    '@/components/settings/SettingDrawer.vue': { __esModule: true, default: stub },
    '@/components/credentials/CredentialResource.vue': { __esModule: true, default: stub },
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  modules['@/api/chatgpt'] = evaluate(readFileSync(new URL('../api/chatgpt.ts', import.meta.url), 'utf8'))
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  let state: any
  component.setup = (props: object, context: object) => {
    state = setup(props, context)
    onServerPrefetch(async () => { await state.loadProviders(); await scenario(state); await nextTick() })
    return state
  }
  const app = createSSRApp(component, { visible: true, modelType, modelData, onConfirm: (value: unknown) => confirmed.push(value) })
  app.use(i18n)
  for (const name of new Set(descriptor.template!.content.match(/t-[a-z-]+(?=[\s>])/g))) {
    if (name !== 't-select' && name !== 't-option') app.component(name, stub)
  }
  app.component('t-select', defineComponent({
    props: ['popupProps'],
    setup: (props, { slots }) => () => h('div', { 'data-provider-select': props.popupProps?.overlayClassName === 'provider-select-popup' || undefined }, slots.default?.()),
  }))
  app.component('t-option', defineComponent({
    props: ['value', 'label'],
    setup: (props, { slots }) => () => h('div', { 'data-option-value': props.value }, [props.label, slots.default?.()]),
  }))
  return { html: await renderToString(app), state, requests, confirmed, chatgptRequests }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: image embedding fields render and send the multimodal connection format`, async () => {
    const payloads: any[] = []
    const modelData = { id: 'image-model', modelName: 'siglip-local', source: 'remote', provider: 'generic', baseUrl: 'http://localhost:18092/v1', supportsImages: true, multimodalFormat: 'jina', dimension: 512 }
    const { html, state } = await renderEditor({ locale, modelType: 'embedding', modelData, testEmbedding: async payload => { payloads.push(payload); return { available: true, dimension: 512 } }, scenario: async state => { state.formData.value = { ...state.formData.value, ...modelData }; await state.checkRemoteAPI() } })
    assert.ok(html.includes((locale === 'zh-CN' ? zhCN : enUS).model.editor.supportsImagesLabel))
    assert.ok(html.includes((locale === 'zh-CN' ? zhCN : enUS).model.editor.multimodalFormatJina))
    assert.equal(state.formData.value.supportsImages, true)
    assert.equal(payloads[0].supportsImages, true)
    assert.equal(payloads[0].multimodalFormat, 'jina')
    assert.equal(payloads[0].modelId, 'image-model')
  })
  test(`${locale}: switching an image embedding model to Ollama clears unsupported image capability`, async () => {
    const { html, state } = await renderEditor({ locale, modelType: 'embedding', scenario: async state => {
      state.formData.value.supportsImages = true
      await state.handleSourceChange('local')
    } })
    assert.equal(state.formData.value.supportsImages, false)
    assert.ok(html.includes((locale === 'zh-CN' ? zhCN : enUS).model.editor.supportsImagesLocalHint))
  })
  test(`${locale}: the provider dropdown renders exactly the twelve server presets`, async () => {
    const { html, state, requests } = await renderEditor({ locale })
    assert.deepEqual(requests, ['chat'])
    assert.deepEqual(state.providerOptions.value.map((preset: ModelProviderOption) => preset.value), presetIds)
    assert.deepEqual(Object.keys((locale === 'zh-CN' ? zhCN : enUS).model.editor.providers), presetIds)
    for (const preset of state.providerOptions.value) {
      assert.match(html, new RegExp(`data-option-value="${preset.value}"`))
      assert.ok(html.includes(preset.label), preset.label)
    }
    assert.doesNotMatch(html, /API description for/)
  })
}

test('rerank dropdown uses only the supported server presets', async () => {
  const { html, state, requests } = await renderEditor({ modelType: 'rerank' })
  assert.deepEqual(requests, ['rerank'])
  assert.deepEqual(state.providerOptions.value.map((preset: ModelProviderOption) => preset.value), rerankIds)
  assert.doesNotMatch(html, /data-option-value="(?:openai|zhipu|volcengine|ollama)"/)
})

test('Aliyun text embedding fills the compatible endpoint supplied by the server', async () => {
  const { state } = await renderEditor({ modelType: 'embedding', scenario: async state => {
    state.formData.value.provider = 'aliyun'
    await state.handleProviderChange('aliyun')
  } })
  assert.equal(state.formData.value.baseUrl, 'https://dashscope.aliyuncs.com/compatible-mode/v1')
})

test('selecting the Ollama preset opens the existing local model list', async () => {
  const { html, state } = await renderEditor({ scenario: async state => {
    state.formData.value.provider = 'ollama'
    await state.handleProviderChange('ollama')
  } })
  assert.equal(state.formData.value.source, 'local')
  assert.equal(state.formData.value.interfaceType, 'ollama')
  assert.equal(state.ollamaModelList.value[0].name, 'qwen3:8b')
  assert.match(html, /qwen3:8b/)
  assert.doesNotMatch(html, /data-provider-select/)
})

test('returning from Ollama to a remote source requires a compatible endpoint', async () => {
  const { state } = await renderEditor({ scenario: async state => {
    state.formData.value.provider = 'ollama'
    await state.handleProviderChange('ollama')
    await state.handleSourceChange('remote')
  } })
  assert.equal(state.formData.value.provider, 'generic')
  assert.equal(state.formData.value.interfaceType, undefined)
  assert.equal(state.formData.value.baseUrl, '')
})

test('a saved provider missing from server presets becomes generic and preserves its custom URL', async () => {
  const customUrl = 'https://custom-models.example/v1'
  const { state } = await renderEditor({ scenario: async state => {
    state.formData.value.provider = 'retired-provider'
    state.formData.value.baseUrl = customUrl
    await state.loadProviders()
  } })
  assert.equal(state.formData.value.provider, 'generic')
  assert.equal(state.formData.value.baseUrl, customUrl)
})

test('an empty API response does not restore a separate frontend preset table', async () => {
  const { html, state } = await renderEditor({ listProviders: async () => [] })
  assert.deepEqual(state.providerOptions.value, [])
  assert.doesNotMatch(html, /data-option-value="(?:generic|openai|aliyun)"/)
})

test('a response from a previous model type cannot overwrite the active preset list', async () => {
  let call = 0
  let finishPrevious!: (value: ModelProviderOption[]) => void
  const { state } = await renderEditor({
    listProviders: async type => {
      if (++call === 2) return new Promise(resolve => { finishPrevious = resolve })
      return presets.filter(preset => preset.modelTypes.includes(type))
    },
    scenario: async state => {
      const previous = state.loadProviders()
      state.draftModelType.value = 'rerank'
      await state.loadProviders()
      finishPrevious(presets)
      await previous
    },
  })
  assert.deepEqual(state.providerOptions.value.map((preset: ModelProviderOption) => preset.value), rerankIds)
  assert.equal(state.loadingProviders.value, false)
})

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: ChatGPT source loads account-specific model names through the real API and validates the selected slug`, async () => {
    const { state, html, confirmed, chatgptRequests } = await renderEditor({ locale, scenario: async state => {
      state.formData.value.apiKey = 'old-secret'; state.formData.value.baseUrl = 'https://old.example'; state.formData.value.customHeaders = [{ key: 'Secret', value: 'old-header' }]
      await state.handleSourceChange('chatgpt')
      state.formData.value.modelName = 'chatgpt-model-slug'; state.selectChatGPTModel()
      await state.handleConfirm()
      await state.handleSourceChange('chatgpt')
    } })
    assert.deepEqual(chatgptRequests.slice(0, 2), ['/api/v1/chatgpt/accounts', '/api/v1/chatgpt/accounts/chatgpt-account/models'])
    assert.equal(confirmed[0].source, 'chatgpt'); assert.equal(confirmed[0].chatgptAccountID, 'chatgpt-account')
    assert.equal(confirmed[0].modelName, 'chatgpt-model-slug'); assert.equal(confirmed[0].displayName, 'Account model')
    assert.equal(confirmed[0].apiKey, ''); assert.equal(confirmed[0].baseUrl, ''); assert.deepEqual(confirmed[0].customHeaders, [])
    assert.match(html, /Account model/); assert.match(html, /Using ChatGPT plan/)
    assert.equal(state.showThinkingControlField.value, false)
  })
}
test('ChatGPT is unavailable to embedding and rerank model sources', async () => {
  for (const modelType of ['embedding', 'rerank'] as const) {
    const { state, html, chatgptRequests } = await renderEditor({ modelType, scenario: async state => { await state.handleSourceChange('chatgpt') } })
    assert.notEqual(state.formData.value.source, 'chatgpt'); assert.deepEqual(chatgptRequests, [])
    assert.doesNotMatch(html, /chatgpt-model-account/)
  }
})
