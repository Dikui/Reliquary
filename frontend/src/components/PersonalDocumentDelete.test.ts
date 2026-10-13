import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('./PersonalDocumentDelete.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename: 'PersonalDocumentDelete.vue' })
const script = compileScript(descriptor, { id: 'personal-delete' })
const template = compileTemplate({ id: 'personal-delete', filename: 'PersonalDocumentDelete.vue', source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
assert.deepEqual(template.errors, [])
const shared = parse(readFileSync(new URL('./DocumentDeleteConfirm.vue', import.meta.url), 'utf8'), { filename: 'DocumentDeleteConfirm.vue' }).descriptor
const sharedScript = compileScript(shared, { id: 'delete-confirm' })
const sharedTemplate = compileTemplate({ id: 'delete-confirm', filename: 'DocumentDeleteConfirm.vue', source: shared.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: sharedScript.bindings } })
assert.deepEqual(sharedTemplate.errors, [])
const messages = { 'zh-CN': zh, 'en-US': en }

async function scenario(locale: keyof typeof messages, response: () => Promise<unknown>, action: (state: any) => Promise<void>, disabled = false) {
  const i18n = createI18n({ legacy: false, locale, messages })
  const calls: string[] = [], removed: string[] = [], notices: string[] = []
  function evaluate(code: string, modules: Record<string, unknown> = {}) {
    const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    const module = { exports: {} as any }
    new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const api = evaluate(readFileSync(new URL('../api/knowledge-base.ts', import.meta.url), 'utf8'), { '../utils/request': { del: async (url: string) => { calls.push(url); return response() } } })
  const sharedComponent = evaluate(sharedScript.content, { 'vue-i18n': { useI18n: () => i18n.global } }).default
  sharedComponent.ssrRender = evaluate(sharedTemplate.code).ssrRender
  const setup = sharedComponent.setup
  sharedComponent.setup = (props: object, context: object) => { const state = setup(props, context); onServerPrefetch(() => action(state)); return state }
  const component = evaluate(script.content, {
    'vue-i18n': { useI18n: () => i18n.global }, '@/api/knowledge-base': api,
    'tdesign-vue-next': { MessagePlugin: { success: (text: string) => notices.push(text) } },
    '@/components/DocumentDeleteConfirm.vue': { __esModule: true, default: sharedComponent },
  }).default
  component.ssrRender = evaluate(template.code).ssrRender
  const app = createSSRApp(component, { documentId: 'mine/doc', title: 'Working note', disabled, onDeleted: (id: string) => removed.push(id) })
  app.use(i18n)
  const stub = defineComponent({ props: ['visible', 'header', 'message', 'disabled'], setup: (props, { attrs, slots }) => () => props.visible === false ? null : h('div', { ...attrs, 'data-disabled': props.disabled || undefined }, [props.header, props.message, slots.default?.()]) })
  for (const name of ['t-button', 't-icon', 't-dialog', 't-alert']) app.component(name, stub)
  const errors: unknown[] = []
  app.config.errorHandler = error => errors.push(error)
  const html = await renderToString(app)
  assert.deepEqual(errors, [])
  return { html, calls, removed, notices }
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: personal document deletion requires confirmation and cancellation sends no request`, async () => {
    const result = await scenario(locale, async () => ({ success: true }), async state => { await state.confirm(); state.open(); state.close(); await state.confirm(); state.open() })
    assert.deepEqual(result.calls, [])
    assert.deepEqual(result.removed, [])
    assert.ok(result.html.includes(messages[locale].personalDocument.delete))
    assert.ok(result.html.includes('Working note'))
    assert.ok(result.html.includes(locale === 'zh-CN' ? '删除后重新导入' : 'import it again'))
  })
  test(`${locale}: confirmed personal deletion calls the ID route and announces completion`, async () => {
    const result = await scenario(locale, async () => ({ success: true, data: { document_id: 'mine/doc', status: 'deleted' } }), async state => { state.open(); await state.confirm(); assert.equal(state.visible.value, false) })
    assert.deepEqual(result.calls, ['/api/v1/documents/mine%2Fdoc'])
    assert.deepEqual(result.removed, ['mine/doc'])
    assert.deepEqual(result.notices, [messages[locale].personalDocument.deleted])
  })
  test(`${locale}: failed deletion keeps the confirmation open and supports retry`, async () => {
    let count = 0
    const result = await scenario(locale, async () => ++count === 1 ? { success: false, message: 'Storage busy' } : { success: true }, async state => {
      state.open(); await state.confirm(); assert.equal(state.visible.value, true); assert.equal(state.error.value, 'Storage busy'); assert.equal(state.busy.value, false)
      await state.confirm(); assert.equal(state.visible.value, false)
    })
    assert.equal(result.calls.length, 2)
    assert.deepEqual(result.removed, ['mine/doc'])
  })
}
test('personal document deletion disables duplicate submission and closing while pending', async () => {
  let finish: (value: unknown) => void = () => {}
  const result = await scenario('zh-CN', () => new Promise(resolve => { finish = resolve }), async state => {
    state.open(); const pending = state.confirm(); assert.equal(state.busy.value, true)
    await state.confirm(); state.close(); assert.equal(state.visible.value, true)
    finish({ success: true }); await pending
  })
  assert.equal(result.calls.length, 1)
})
test('disabled personal deletion sends no request', async () => {
  const result = await scenario('en-US', async () => ({ success: true }), async state => { state.open(); await state.confirm(); assert.equal(state.visible.value, false) }, true)
  assert.deepEqual(result.calls, [])
})
