import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import en from '../i18n/locales/en-US'
import zh from '../i18n/locales/zh-CN'
import { DEFAULT_MODEL_CONTEXT_WINDOW } from '../utils/contextWindow'
import { defaultThinkingControl, resolveThinkingControl } from '../utils/thinkingControl'
import { shouldShowOllamaUnavailableTip } from './modelEditorSourceState'
import { click, descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../views/library/componentTestHarness'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zh, 'en-US': en }
const installed = { name: 'qwen3:8b', size: 5 * 1024 ** 3, digest: 'local-model', modified_at: '2026-10-10T00:00:00Z' }

async function mountEditor(locale: 'zh-CN' | 'en-US', models: typeof installed[]) {
  const calls: string[] = []
  const i18n = createI18n({ legacy: false, locale, messages })
  const request = { get: async (path: string) => {
    calls.push(path)
    if (path === '/api/v1/initialization/ollama/status') return { data: { available: true } }
    if (path === '/api/v1/initialization/ollama/models') return { data: { models } }
    return { data: [] }
  } }
  const module = { exports: {} as any }
  const code = ts.transpileModule(readFileSync(new URL('../api/initialization.ts', import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  new Function('require', 'module', 'exports', code)((name: string) => name === '../utils/request' ? request
    : name === '@/i18n' ? { __esModule: true, default: i18n } : require(name), module, module.exports)
  const wrapper = defineComponent({ props: ['visible'], setup: (props, { slots }) => () => props.visible ? h('section', slots.default?.()) : null })
  const component = loadComponent(new URL('./ModelEditorDialog.vue', import.meta.url), {
    '@/api/initialization': module.exports, '@/api/model': {}, '@/api/chatgpt': {},
    'tdesign-vue-next': { MessagePlugin: { warning() {}, error() {} }, DialogPlugin: {} },
    '@/stores/ui': { useUIStore: () => ({}) },
    '@/utils/contextWindow': { DEFAULT_MODEL_CONTEXT_WINDOW },
    '@/utils/thinkingControl': { defaultThinkingControl, resolveThinkingControl },
    '@/components/modelEditorSourceState': { shouldShowOllamaUnavailableTip },
    '@/components/settings/SettingDrawer.vue': { __esModule: true, default: wrapper },
    '@/components/credentials/CredentialResource.vue': { __esModule: true, default: wrapper },
  })
  const visible = ref(false)
  const app = renderer.createApp(defineComponent({ setup: () => () => h(component, { visible: visible.value, modelType: 'chat' }) }))
  app.use(i18n); registerPrimitives(app)
  const passthrough = defineComponent({ setup: (_, { slots }) => () => h('div', slots.default?.()) })
  for (const name of ['t-form', 't-form-item', 't-input-number', 't-switch', 't-radio-group', 't-radio', 't-slider']) app.component(name, passthrough)
  delete app._context.components['t-select']; delete app._context.components['t-option']
  app.component('t-select', defineComponent({
    props: ['modelValue', 'filter'], emits: ['input-change'],
    setup(props, { attrs, slots, emit }) {
      return () => h('select', { ...attrs, value: props.modelValue, onInput: (event: { target: { value: string } }) => {
        if (models.length) props.filter?.(event.target.value)
        emit('input-change', event.target.value)
      } }, slots.default?.())
    },
  }))
  app.component('t-option', defineComponent({ props: ['value', 'label'], setup: props => () => h('option', { value: props.value }, props.label) }))
  const root = element('root'), errors: unknown[] = [], warnings: string[] = []
  app.config.errorHandler = error => errors.push(error)
  app.config.warnHandler = warning => warnings.push(warning)
  app.mount(root); visible.value = true; await settle()
  const localSource = descendants(root).find(node => node.props.role === 'radio' && visibleText(node).includes(messages[locale].model.editor.sourceLocal))
  assert.ok(localSource)
  await click(localSource); await settle()
  const select = () => descendants(root).find(node => node.tag === 'select' && node.props.placeholder === messages[locale].model.searchPlaceholder)!
  assert.ok(select())
  await select().props.onFocus(); await settle()
  return { app, root, calls, errors, warnings, select,
    input: async (value: string) => { select().props.onInput({ target: { value } }); await settle() },
    options: () => descendants(select()).filter(node => node.tag === 'option').map(node => node.props.value),
  }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: empty Ollama model response still offers a download after input and removes it after clearing`, async () => {
    const editor = await mountEditor(locale, [])
    try {
      assert.ok(editor.calls.includes('/api/v1/initialization/ollama/models'))
      assert.deepEqual(editor.options(), [])
      await editor.input('qwen3:8b')
      assert.deepEqual(editor.options(), ['__download__qwen3:8b'])
      assert.ok(visibleText(editor.root).includes(messages[locale].model.editor.downloadLabel.replace('{keyword}', 'qwen3:8b')))
      await editor.input('')
      assert.deepEqual(editor.options(), [])
      await editor.input('   ')
      assert.deepEqual(editor.options(), [])
      assert.deepEqual(editor.errors, []); assert.deepEqual(editor.warnings, [])
    } finally { editor.app.unmount() }
  })

  test(`${locale}: installed Ollama model filtering and download choices preserve the existing behavior`, async () => {
    const editor = await mountEditor(locale, [installed])
    try {
      assert.deepEqual(editor.options(), ['qwen3:8b'])
      await editor.input('QWEN3:8B')
      assert.deepEqual(editor.options(), ['qwen3:8b'])
      await editor.input('qwen3')
      assert.deepEqual(editor.options(), ['qwen3:8b', '__download__qwen3'])
      await editor.input('gemma3:4b')
      assert.deepEqual(editor.options(), ['__download__gemma3:4b'])
      await editor.select().props.onVisibleChange(false); await settle()
      assert.deepEqual(editor.options(), ['qwen3:8b'])
      assert.deepEqual(editor.errors, []); assert.deepEqual(editor.warnings, [])
    } finally { editor.app.unmount() }
  })
}
