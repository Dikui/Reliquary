import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import { Input, Select, Textarea } from 'tdesign-vue-next'
import ts from 'typescript'
import enUS from '../../../i18n/locales/en-US'
import zhCN from '../../../i18n/locales/zh-CN'
import * as entityTypes from '../../../api/entity-types'

const require = createRequire(import.meta.url)
function compile(path: URL, modules: Record<string, unknown>) {
  const { descriptor } = parse(readFileSync(path, 'utf8'), { filename: path.pathname })
  const script = compileScript(descriptor, { id: path.pathname })
  const template = compileTemplate({ id: path.pathname, filename: path.pathname, source: descriptor.template!.content,
    ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  return component
}

for (const [locale, messages] of [['zh-CN', zhCN], ['en-US', enUS]] as const) {
  test(`${locale}: entity form labels identify native name, type and alias controls`, async () => {
    const textarea = compile(new URL('../../../components/SafeTextarea.vue', import.meta.url), {
      'tdesign-vue-next': { Textarea }, '@/utils/textareaAutosize': {},
    })
    const editor = compile(new URL('./EntityEditor.vue', import.meta.url), {
      '@/api/entities': entityTypes,
      '@/components/ModalDialog.vue': defineComponent({ props: ['visible'], setup: (props, { slots }) => () => props.visible === false ? null : h('div', slots.default?.()) }),
      '@/components/SafeTextarea.vue': { __esModule: true, default: textarea },
      'tdesign-vue-next': { MessagePlugin: {} },
      'vue-router': { onBeforeRouteLeave() {}, onBeforeRouteUpdate() {} },
    })
    const app = createSSRApp(editor, { entity: null })
    app.use(createI18n({ legacy: false, locale, messages: { [locale]: messages } }))
    app.component('t-input', Input); app.component('t-select', Select)
    const stub = defineComponent({ props: ['visible'], setup: (props, { slots }) => () => props.visible === false ? null : h('div', slots.default?.()) })
    app.component('t-dialog', stub); app.component('t-button', stub)
    const html = await renderToString(app)
    for (const [id, label, tag] of [
      ['entity-canonical-name', messages.entityMaintenance.name, 'input'],
      ['entity-type', messages.entityMaintenance.type, 'select'],
      ['entity-aliases', messages.entityMaintenance.aliases, 'textarea'],
    ]) {
      assert.ok(html.includes(`<label for="${id}">${label}</label>`), label)
      assert.match(html, new RegExp(`<${tag}\\b[^>]*\\bid="${id}"`), `${label} points to the real editable control`)
    }
  })
}
