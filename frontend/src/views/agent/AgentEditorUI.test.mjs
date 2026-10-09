import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileStyleAsync, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import postcss from 'postcss'
import ts from 'typescript'
import enUS from '../../i18n/locales/en-US.ts'
import zhCN from '../../i18n/locales/zh-CN.ts'
import { evaluateToolRequirement } from '../../utils/tool-capabilities.ts'

const require = createRequire(import.meta.url)
const agentSource = readFileSync(new URL('./AgentEditorModal.vue', import.meta.url), 'utf8')
const workflowSource = readFileSync(new URL('../knowledge/components/KnowledgeWorkbench.vue', import.meta.url), 'utf8')
const toolsExpression = agentSource.match(/const allTools = computed\(\(\) => (\[[\s\S]*?\])\);/)?.[1]
const reasonFunction = agentSource.match(/const missKindToReason = [\s\S]*?\n};/)?.[0]
const availabilityBody = agentSource.match(/const availableTools = computed\(\(\) => \{([\s\S]*?)\n\}\);/)?.[1]
const cards = agentSource.match(/<div class="tool-grid">[\s\S]*?<\/t-checkbox>\s*<\/div>/)?.[0]
assert.ok(toolsExpression && reasonFunction && availabilityBody && cards)
const template = compileTemplate({ id: 'agent-tool-cards', filename: 'AgentEditorModal.vue', source: cards })
assert.deepEqual(template.errors, [])
const compiled = ts.transpileModule(template.code, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
const exports = {}
new Function('require', 'exports', compiled)(require, exports)

function toolsFor(t, scope, hasKb) {
  const allTools = { value: new Function('t', `return ${toolsExpression}`)(t) }
  const code = ts.transpileModule(`${reasonFunction}\nfunction available() {${availabilityBody}}`, {}).outputText
  return new Function('t', 'allTools', 'scopeCapabilities', 'hasKnowledgeBase', 'evaluateToolRequirement', `${code}; return available()`)(
    t, allTools, { value: scope }, { value: hasKb }, evaluateToolRequirement,
  )
}

for (const [locale, name] of [['zh-CN', '数据分析'], ['en-US', 'Data analysis']]) {
  for (const [state, scope, hasKb, reasonKey] of [
    ['vector indexing', { vector: true, keyword: false, context: false }, true, null],
    ['keyword indexing', { vector: false, keyword: true, context: false }, true, null],
    ['no indexing', { vector: false, keyword: false, context: false }, true, 'requiresRagKb'],
    ['no knowledge base', { vector: false, keyword: false, context: false }, false, 'requiresKb'],
  ]) {
    test(`${locale} tool cards keep their names with ${state} and show only missing dependency hints`, async () => {
      const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
      const t = (...args) => i18n.global.t(...args)
      const tools = toolsFor(t, scope, hasKb)
      assert.equal(tools.length, 14)
      assert.equal(new Set(tools.map(tool => tool.label)).size, 14)
      assert.equal(tools.find(tool => tool.value === 'data_analysis').label, name)
      for (const tool of tools) {
        for (const key of ['requiresKb', 'requiresRagKb']) {
          assert.notEqual(tool.label, t(`agentEditor.tools.${key}`), `${tool.value} must keep its tool name`)
        }
        assert.ok(!tool.label.includes('需启用') && !tool.label.includes('Requires RAG'))
        if (tool.disabled) assert.equal(tool.disabledReason, t(`agentEditor.tools.${reasonKey}`))
        else assert.equal(tool.disabledReason, undefined)
      }
      const context = { group: { tools }, formData: { config: { allowed_tools: [] } }, $t: t }
      const app = createSSRApp(defineComponent({ setup: () => () => exports.render(context, []) }))
      app.component('t-checkbox', defineComponent({
        props: ['disabled', 'value'],
        setup: (props, { slots }) => () => h('label', { 'data-tool': props.value, 'aria-disabled': props.disabled }, slots.default?.()),
      }))
      const html = await renderToString(app)
      const names = [...html.matchAll(/class="tool-card-name">([^<]+)<\/span>/g)].map(match => match[1])
      assert.equal(names.length, 14)
      assert.ok(names.includes(name))
      assert.equal((html.match(/class="tool-card-hint"/g) || []).length, tools.filter(tool => tool.disabled).length)
      assert.equal(tools.some(tool => tool.disabled), !!reasonKey)
    })
  }
}

async function stylesheet(source) {
  const { descriptor } = parse(source)
  const result = await compileStyleAsync({
    id: 'data-v-layout-test', source: descriptor.styles.map(style => style.content).join('\n'),
    scoped: true, preprocessLang: 'less',
  })
  assert.deepEqual(result.errors, [])
  const css = postcss.parse(result.code)
  return (width, own, ancestors = []) => {
    const values = {}, priorities = {}
    css.walkRules(rule => {
      for (let parent = rule.parent; parent; parent = parent.parent) {
        if (parent.type === 'atrule') {
          if (parent.name !== 'media') return
          const max = parent.params.match(/max-width:\s*(\d+)px/)
          if (!max || width > Number(max[1])) return
        }
      }
      for (const raw of rule.selectors) {
        const selector = raw.replace(/\[[^\]]+\]/g, '')
        if (selector.includes(':')) continue
        const parts = selector.trim().split(/\s+/)
        const target = [...parts.pop().matchAll(/\.([\w-]+)/g)].map(match => match[1])
        const parents = [...parts.join(' ').matchAll(/\.([\w-]+)/g)].map(match => match[1])
        if (!target.length || !target.every(name => own.includes(name)) || !parents.every(name => ancestors.includes(name))) continue
        const specificity = target.length + parents.length
        rule.walkDecls(decl => {
          if ((priorities[decl.prop] ?? -1) <= specificity) {
            priorities[decl.prop] = specificity
            values[decl.prop] = decl.value
          }
        })
      }
    })
    return values
  }
}

test('390px agent editor uses top navigation, full-width fields and readable cards and buttons', async () => {
  const at = await stylesheet(agentSource)
  assert.equal(at(390, ['settings-container'])['flex-direction'], 'column')
  assert.equal(at(390, ['settings-sidebar']).width, '100%')
  assert.equal(at(390, ['settings-nav'])['overflow-x'], 'auto')
  assert.equal(at(390, ['nav-item'])['flex'], '0 0 auto')
  assert.equal(at(390, ['setting-row'])['flex-direction'], 'column')
  for (const field of ['setting-info', 'setting-control']) assert.equal(at(390, [field])['max-width'], '100%')
  assert.equal(at(390, ['content-wrapper']).padding, '20px 16px')
  assert.equal(at(390, ['tool-card-name'])['white-space'], 'normal')
  assert.equal(at(390, ['tool-card-desc']).display, 'block')
  assert.equal(at(390, ['tool-card-desc']).overflow, 'visible')
  assert.equal(at(390, ['settings-footer-actions'])['flex-wrap'], 'wrap')
  assert.equal(at(390, ['t-button'], ['settings-footer-actions'])['white-space'], 'normal')
  assert.equal(at(390, ['t-button__text'], ['settings-footer-actions'])['overflow-wrap'], 'anywhere')
  assert.equal(at(1440, ['settings-sidebar']).width, '208px')
  assert.equal(at(1440, ['settings-modal']).width, '90vw')
  assert.equal(at(1440, ['content-wrapper']).padding, '28px 40px 48px')
  assert.equal(at(1440, ['setting-control'])['max-width'], '58%')
  assert.equal(at(1440, ['settings-container'])['flex-direction'], undefined)
})

test('390px workflow types occupy a full grid row and add-step labels wrap without changing desktop layout', async () => {
  const at = await stylesheet(workflowSource)
  assert.match(workflowSource, /<t-select v-model="step.type"[^>]*class="workflow-step__type"/)
  assert.equal(at(390, ['workflow-step__top'])['grid-template-columns'], '26px minmax(0, 1fr)')
  assert.equal(at(390, ['workflow-step__type'])['grid-column'], '1 / -1')
  assert.equal(at(390, ['workflow-step__type']).width, '100%')
  assert.equal(at(390, ['workflow-step__type'])['min-width'], '0')
  assert.equal(at(390, ['workflow-steps-heading'])['grid-template-columns'], 'minmax(0, 1fr)')
  assert.equal(at(390, ['add-step-select'], ['workflow-steps-heading']).width, '100%')
  assert.equal(at(390, ['t-button'], ['workflow-steps-heading'])['white-space'], 'normal')
  assert.equal(at(390, ['t-button__text'], ['workflow-steps-heading'])['overflow-wrap'], 'anywhere')
  assert.equal(at(1440, ['workflow-step__top'])['grid-template-columns'], '30px minmax(100px, 1fr) minmax(150px, 1fr) auto')
  assert.equal(at(1440, ['workflow-steps-heading']).display, 'flex')
  assert.equal(at(1440, ['workflow-step__type'])['grid-column'], undefined)
})
