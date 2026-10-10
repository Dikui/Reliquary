import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import enUS from '../i18n/locales/en-US'
import zhCN from '../i18n/locales/zh-CN'

const require = createRequire(import.meta.url)
const stageNames = {
  entity_judgment: ['实体身份判断', 'Entity identity judgment'],
  entity_embedding: ['实体名称向量生成', 'Entity name embedding'],
  entity_embedding_rebuild: ['实体名称向量重建', 'Entity name embedding rebuild'],
} as const

function productionStages(directory: URL): string[] {
  const stages = new Set<string>()
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = new URL(entry.name + (entry.isDirectory() ? '/' : ''), directory)
    if (entry.isDirectory()) for (const stage of productionStages(path)) stages.add(stage)
    else if (entry.name.endsWith('.go') && !entry.name.endsWith('_test.go')) {
      for (const match of readFileSync(path, 'utf8').matchAll(/WithLLMCallMetadata\([^\n]*?"(entity_[a-z_]+)"/g)) stages.add(match[1]!)
    }
  }
  return [...stages].sort()
}

function compileComponent(path: URL, modules: Record<string, unknown>) {
  const filename = path.pathname
  const { descriptor } = parse(readFileSync(path, 'utf8'), { filename })
  const script = compileScript(descriptor, { id: filename })
  const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
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

async function renderCalls(locale: 'zh-CN' | 'en-US', stages: string[]) {
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
  }
  const summary = compileComponent(new URL('./ModelCallSummary.vue', import.meta.url), modules)
  modules['@/components/ModelCallSummary.vue'] = { __esModule: true, default: summary }
  const calls = stages.map(stage => ({ stage, tier: 'entity_resolution', calls: 1 }))
  const app = createSSRApp(summary, { calls })
  const stub = defineComponent({ setup: (_, { attrs, slots }) => () => h('div', attrs, slots.default?.()) })
  app.component('t-tag', stub)
  app.component('t-alert', stub)
  return renderToString(app)
}

test('every entity model call produced in Go has a Chinese and English stage name', () => {
  const stages = productionStages(new URL('../../../internal/', import.meta.url))
  assert.deepEqual(stages, Object.keys(stageNames).sort())
  for (const stage of stages) {
    const key = stage as keyof typeof stageNames
    assert.equal(zhCN.modelCalls.stages[key], stageNames[key][0])
    assert.equal(enUS.modelCalls.stages[key], stageNames[key][1])
  }
})

for (const [locale, language] of [['zh-CN', 0], ['en-US', 1]] as const) {
  test(`${locale}: all current stages render localized names`, async () => {
    const html = await renderCalls(locale, Object.keys(stageNames))
    for (const names of Object.values(stageNames)) assert.ok(html.includes(names[language]), names[language])
    assert.doesNotMatch(html, /entity_[a-z_]+/)
  })

  test(`${locale}: an unknown stage never displays its internal identifier`, async () => {
    const html = await renderCalls(locale, ['unmapped_private_stage'])
    assert.ok(html.includes(['其他模型调用', 'Other model call'][language]), html)
    assert.doesNotMatch(html, /unmapped_private_stage|modelCalls\./)
  })
}
