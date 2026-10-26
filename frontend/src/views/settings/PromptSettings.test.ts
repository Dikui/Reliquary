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
import { normalizeSettingsSection } from '../../config/settingsRoute'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('./PromptSettings.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename: 'PromptSettings.vue' })
const script = compileScript(descriptor, { id: 'prompt-settings-test' })
const template = compileTemplate({ id: 'prompt-settings-test', filename: 'PromptSettings.vue', source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
assert.deepEqual(template.errors, [])

async function scenario(locale: 'zh-CN' | 'en-US', failSave: boolean, action: (state: any, requests: any[], dialog: () => any) => Promise<void>) {
  const requests: any[] = []
  let confirmation: any
  let stored = { key: 'prompt.article_processing', id: 0, value: 'Original prompt', value_type: 'string', category: 'prompts' }
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const originalWindow = globalThis.window
  globalThis.window = { addEventListener() {}, removeEventListener() {} } as any
  const stub = defineComponent({ setup(_, { slots }) { return () => h('div', slots.default?.()) } })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
    'tdesign-vue-next': {
      MessagePlugin: { success(message: string) { requests.push({ toast: message }) } },
      DialogPlugin: { confirm(options: any) { confirmation = options; return { destroy() {} } } },
    },
    '@/components/SafeTextarea.vue': { __esModule: true, default: defineComponent({ props: ['modelValue'], setup(props) { return () => h('textarea', props.modelValue) } }) },
    '@/api/system': {
      listSystemSettings: async () => [{ ...stored }, { key: 'queue.core_concurrency', value: 1 }],
      updateSystemSetting: async (key: string, value: string) => {
        requests.push({ key, value })
        if (failSave) throw new Error('Save unavailable')
        stored = { ...stored, id: 31, value }
        return { ...stored }
      },
    },
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
    onServerPrefetch(async () => { await state.load(); await action(state, requests, () => confirmation) })
    return state
  }
  const app = createSSRApp(component)
  app.use(i18n)
  for (const name of ['t-button', 't-loading', 't-empty', 't-tag']) app.component(name, stub)
  try { return { html: await renderToString(app), requests } }
  finally { globalThis.window = originalWindow }
}

test('prompt settings save edited content and load it again through the existing settings API', async () => {
  assert.equal(normalizeSettingsSection('prompts'), 'prompts')
  const { html, requests } = await scenario('zh-CN', false, async (state, calls) => {
    assert.equal(state.templates.value.length, 1)
    state.draft.value = 'Updated prompt\n保留换行'
    await state.save()
    assert.equal(state.dirty.value, false)
    assert.equal(state.selected.value.row.id, 31)
    assert.equal(calls.filter(call => call.key).length, 1)
    await state.load()
    assert.equal(state.draft.value, 'Updated prompt\n保留换行')
  })
  assert.match(html, /资料理解/)
  assert.match(html, /已自定义/)
  assert.equal(requests.find(call => call.key).key, 'prompt.article_processing')
  assert.equal(requests.find(call => call.key).value, 'Updated prompt\n保留换行')
})

test('failed prompt saves retain the draft and cancelling navigation keeps edits', async () => {
  const { html } = await scenario('en-US', true, async (state, calls, dialog) => {
    state.draft.value = 'Keep this draft'
    await state.save()
    assert.equal(state.dirty.value, true)
    assert.equal(state.draft.value, 'Keep this draft')
    assert.equal(state.selected.value.row.value, 'Original prompt')
    assert.equal(state.error.value, 'Save unavailable')
    const leave = state.confirmLeave()
    dialog().onCancel()
    assert.equal(await leave, false)
    assert.equal(state.draft.value, 'Keep this draft')
    assert.equal(calls.some(call => call.toast), false)
  })
  assert.match(html, /role="alert"[^>]*>Save unavailable/)
  assert.match(html, /Unsaved changes/)
  assert.match(html, /Keep this draft/)
})
