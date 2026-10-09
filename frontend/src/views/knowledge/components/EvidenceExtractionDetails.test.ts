import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import zhCN from '../../../i18n/locales/zh-CN'
import enUS from '../../../i18n/locales/en-US'
import ts from 'typescript'
import { shouldShowExtractionCoverage } from '../../../utils/extractionCoverage'

const require = createRequire(import.meta.url)
const filename = 'EvidenceExtractionDetails.vue'
const source = readFileSync(new URL('./EvidenceExtractionDetails.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename })
const script = compileScript(descriptor, { id: 'extraction-mode-test' })
const template = compileTemplate({
  id: 'extraction-mode-test', filename, source: descriptor.template!.content, ssr: true,
  ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings },
})
assert.deepEqual(template.errors, [])

async function renderDetails(evidence: object, compact = false, locale?: 'zh-CN' | 'en-US') {
  const emptyComponent = defineComponent({ setup: () => () => null })
  const i18n = locale ? createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } }) : null
  const modules: Record<string, unknown> = {
    '@/utils/extractionCoverage': { shouldShowExtractionCoverage },
    '@/components/ModelCallSummary.vue': { __esModule: true, default: emptyComponent },
    'vue-i18n': { useI18n: () => i18n?.global || ({ locale: ref('en-US'), t: (key: string, params?: object) => params ? `${key} ${JSON.stringify(params)}` : key }) },
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', compiled)((name: string) => modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const app = createSSRApp(component, { evidence, compact })
  const stub = defineComponent({ setup: (_, { attrs, slots }) => () => h('div', attrs, slots.default?.()) })
  app.component('t-tag', stub)
  app.component('t-alert', stub)
  return renderToString(app)
}

test('template-only mode remains separate from complete segment coverage', async () => {
  const html = await renderDetails({ extraction_mode: 'template_only', extraction_status: 'complete', extraction_coverage: 'complete', processed_segments: 2, total_segments: 2 })
  assert.match(html, /knowledgeWorkbench.evidence.extractionModes.template_only/)
  assert.match(html, /knowledgeWorkbench.evidence.templateOnlyHint/)
  assert.match(html, /extractionDetails.complete/)
  assert.match(html, /extractionDetails.segments/)
  assert.doesNotMatch(html, /extractionModes.full/)
})

test('full mode may still have partial segment coverage', async () => {
  const html = await renderDetails({ extraction_mode: 'full', extraction_status: 'failed', extraction_coverage: 'partial', processed_segments: 1, total_segments: 2 })
  assert.match(html, /knowledgeWorkbench.evidence.extractionModes.full/)
  assert.match(html, /extractionDetails.partial/)
  assert.match(html, /extractionDetails.partialHint/)
  assert.doesNotMatch(html, /knowledgeWorkbench.evidence.templateOnlyHint/)
})

test('compact evidence cards show the mode without the expanded explanation', async () => {
  const html = await renderDetails({ extraction_mode: 'template_only' }, true)
  assert.match(html, /extractionModes.template_only/)
  assert.doesNotMatch(html, /knowledgeWorkbench.evidence.templateOnlyHint/)
})

for (const locale of ['zh-CN', 'en-US'] as const) {
  for (const extraction_path of ['precheck_hit', 'precheck_miss_supplemented', 'precheck_miss']) {
    test(`${locale}: evidence details explain ${extraction_path} and preserve backfill metadata`, async () => {
      const html = await renderDetails({ extraction_mode: 'full', extraction_path, backfilled_at: '2026-10-09T12:00:00Z', backfill_template_updated_at: '2026-10-08T12:00:00Z' }, false, locale)
      assert.match(html, locale === 'zh-CN' ? /本次抽取路径/ : /Extraction path/)
      assert.match(html, locale === 'zh-CN' ? /补抽时间/ : /Backfilled at/)
      assert.match(html, locale === 'zh-CN' ? /补抽所用领域模板更新时间/ : /Domain template updated at for backfill/)
      assert.doesNotMatch(html, /knowledgeWorkbench\./)
      assert.match(html, /2026/)
    })
  }
}
