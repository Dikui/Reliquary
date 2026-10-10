import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import enUS from '../../../i18n/locales/en-US'
import zhCN from '../../../i18n/locales/zh-CN'
import { entityScopeParams } from '../../../api/entity-scope'

const require = createRequire(import.meta.url)
const filename = 'MergeSuggestions.vue'
const { descriptor } = parse(readFileSync(new URL('./MergeSuggestions.vue', import.meta.url), 'utf8'), { filename })
const script = compileScript(descriptor, { id: filename })
const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
assert.deepEqual(template.errors, [])

async function renderSuggestions(locale: 'zh-CN' | 'en-US', count: number, status: 'accepted' | 'rejected') {
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
    'tdesign-vue-next': { MessagePlugin: { success() {} } },
    '@/components/settings/useConfirmDelete': { useConfirmDelete: () => () => {} },
    '@/api/entities': { entityScopeParams, listEntityMergeSuggestions: () => Promise.resolve({ success: true, data: Array.from({ length: count }, (_, index) => ({ id: `suggestion-${index}`, status, new_entity_name: 'Aurora', candidate_entity_name: 'Aurora model' })) }) },
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const output = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', output)((name: string) => modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  component.setup = (props: object, context: object) => {
    const state = setup(props, context)
    onServerPrefetch(async () => { state.status.value = status; await nextTick(); await state.refresh() })
    return state
  }
  const app = createSSRApp(component, { kbId: 'kb-a', scope: 'knowledge_base' })
  const stub = defineComponent({ props: ['message', 'description'], setup: (props, { attrs, slots }) => () => h('div', attrs, [props.message, props.description, slots.default?.()]) })
  for (const name of ['t-alert', 't-button', 't-empty', 't-loading', 't-select', 't-tag']) app.component(name, stub)
  return renderToString(app)
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const count of [0, 1, 2]) {
    for (const status of ['accepted', 'rejected'] as const) {
      test(`${locale}: ${status} suggestions use the correct loaded count for ${count}`, async () => {
        const html = await renderSuggestions(locale, count, status)
        assert.ok(html.includes(locale === 'zh-CN' ? `已加载 ${count} 条建议` : `Loaded ${count} ${count === 1 ? 'suggestion' : 'suggestions'}<`), html)
        assert.doesNotMatch(html, /mergeSuggestions\./)
      })
    }
  }
}
