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
import { shouldShowExtractionCoverage } from '../utils/extractionCoverage'

const require = createRequire(import.meta.url)
const stageNames = {
  context_extraction: ['对象与模板字段抽取', 'Entity and template field extraction'],
  context_full_extraction: ['开放字段与事件补缺', 'Open field and event supplementation'],
  context_backfill_extraction: ['历史消息补缺', 'Historical message supplementation'],
  context_entity_judgment: ['对象身份判断', 'Entity identity judgment'],
  context_entity_resolution: ['对象身份解析', 'Entity identity resolution'],
  context_event_judgment: ['事件同次变化判断', 'Event occurrence judgment'],
  context_event_resolution: ['事件聚合解析', 'Event aggregation resolution'],
  context_attention: ['关注价值评分', 'Attention value scoring'],
  context_entity_embedding: ['对象名称向量生成', 'Entity name embedding'],
  context_entity_embedding_rebuild: ['对象名称向量重建', 'Entity name embedding rebuild'],
} as const

function productionStages(directory: URL): string[] {
  const stages = new Set<string>()
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = new URL(entry.name + (entry.isDirectory() ? '/' : ''), directory)
    if (entry.isDirectory()) for (const stage of productionStages(path)) stages.add(stage)
    else if (entry.name.endsWith('.go') && !entry.name.endsWith('_test.go')) {
      for (const match of readFileSync(path, 'utf8').matchAll(/WithLLMCallMetadata\([^\n]*?"(context_[a-z_]+)"/g)) stages.add(match[1]!)
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

async function renderCalls(locale: 'zh-CN' | 'en-US', stages: string[], extractionPath?: string, evidence = false, extractionMode = 'full') {
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
    '@/utils/extractionCoverage': { shouldShowExtractionCoverage },
  }
  const summary = compileComponent(new URL('./ModelCallSummary.vue', import.meta.url), modules)
  modules['@/components/ModelCallSummary.vue'] = { __esModule: true, default: summary }
  const calls = stages.map(stage => ({ stage, tier: 'extraction', calls: 1 }))
  const component = evidence ? compileComponent(new URL('../views/knowledge/components/EvidenceExtractionDetails.vue', import.meta.url), modules) : summary
  const app = createSSRApp(component, evidence ? { evidence: { extraction_mode: extractionMode, extraction_path: extractionPath, model_calls: calls } } : { calls, extractionPath })
  const stub = defineComponent({ setup: (_, { attrs, slots }) => () => h('div', attrs, slots.default?.()) })
  app.component('t-tag', stub)
  app.component('t-alert', stub)
  return renderToString(app)
}

test('every context model call produced in Go has a Chinese and English stage name', () => {
  const stages = productionStages(new URL('../../../internal/', import.meta.url))
  assert.deepEqual(stages, Object.keys(stageNames).sort())
  for (const stage of stages) {
    const key = stage as keyof typeof stageNames
    assert.equal(zhCN.extractionDetails.stages[key], stageNames[key][0])
    assert.equal(enUS.extractionDetails.stages[key], stageNames[key][1])
  }
})

for (const [locale, language] of [['zh-CN', 0], ['en-US', 1]] as const) {
  test(`${locale}: all current stages render localized names`, async () => {
    const html = await renderCalls(locale, Object.keys(stageNames), 'precheck_miss')
    for (const names of Object.values(stageNames)) assert.ok(html.includes(names[language]), names[language])
    assert.doesNotMatch(html, /context_[a-z_]+/)
  })

  for (const extractionPath of ['precheck_miss', 'precheck_miss_supplemented', 'precheck_hit']) {
    test(`${locale}: the first extraction call follows evidence path ${extractionPath}`, async () => {
      const html = await renderCalls(locale, ['context_extraction'], extractionPath, true)
      const name = extractionPath === 'precheck_hit' ? ['完整抽取', 'Full extraction'][language] : stageNames.context_extraction[language]
      assert.ok(html.includes(`<td>${name}</td>`), html)
      assert.doesNotMatch(html, /context_extraction/)
    })
  }

  test(`${locale}: a rejected precheck still names the actual full extraction call`, async () => {
    const html = await renderCalls(locale, ['context_extraction'], 'precheck_hit', true, 'template_only')
    assert.ok(html.includes(`<td>${['完整抽取', 'Full extraction'][language]}</td>`), html)
  })

  test(`${locale}: old evidence or a span without a path uses a general extraction name`, async () => {
    const html = await renderCalls(locale, ['context_extraction'])
    assert.ok(html.includes(`<td>${['结构化抽取', 'Structured extraction'][language]}</td>`), html)
    assert.doesNotMatch(html, /context_extraction/)
  })

  test(`${locale}: an unknown stage never displays its internal identifier`, async () => {
    const html = await renderCalls(locale, ['context_unmapped_private_stage'])
    assert.ok(html.includes(['其他模型调用', 'Other model call'][language]), html)
    assert.doesNotMatch(html, /context_unmapped_private_stage|extractionDetails\./)
  })
}
