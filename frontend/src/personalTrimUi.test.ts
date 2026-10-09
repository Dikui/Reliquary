import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileStyleAsync, compileTemplate, parse } from '@vue/compiler-sfc'
import { parse as parseDOM, NodeTypes, type RootNode, type TemplateChildNode } from '@vue/compiler-dom'
import { createSSRApp, defineComponent, h } from 'vue'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'

const require = createRequire(import.meta.url)

async function compiledStyles(path: string) {
  const filename = new URL(path, import.meta.url)
  const { descriptor } = parse(readFileSync(filename, 'utf8'), { filename: filename.pathname })
  const style = descriptor.styles.find(block => block.scoped)!
  const compiled = await compileStyleAsync({
    filename: filename.pathname, id: 'data-v-ui-regression', source: style.content,
    scoped: true, preprocessLang: 'less',
  })
  assert.deepEqual(compiled.errors, [])
  return compiled.rawResult!.root
}

test('compiled general settings styles keep selectors and previews inside mobile rows', async () => {
  const css = await compiledStyles('./views/settings/GeneralSettings.vue')
  const rules: Record<string, Record<string, string>> = {}
  css.walkRules(rule => {
    if (rule.parent?.type !== 'atrule' || !('params' in rule.parent) || rule.parent.params !== '(max-width: 640px)') return
    for (const selector of rule.selectors) {
      const name = selector.replace(/\[data-v-ui-regression\]/g, '')
      const declarations: Record<string, string> = {}
      rule.walkDecls(decl => { declarations[decl.prop] = decl.value })
      rules[name] = declarations
    }
  })
  assert.equal(rules['.setting-row']['flex-direction'], 'column')
  for (const selector of ['.setting-info', '.setting-control']) {
    assert.equal(rules[selector]['min-width'], '0')
    assert.equal(rules[selector].width, '100%')
    assert.equal(rules[selector]['max-width'], '100%')
  }
  assert.equal(rules['.setting-info']['padding-right'], '0')
  assert.equal(rules['.setting-control']['align-items'], 'flex-start')
  for (const selector of ['.setting-select', '.font-preview']) assert.equal(rules[selector].width, '100%')
  const source = readFileSync(new URL('./views/settings/GeneralSettings.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(source, /style="width: 280px;"/)
  assert.equal((source.match(/class="setting-select"/g) || []).length, 4)
})

test('compiled SSRF input styles reserve suffix space and keep tag close icons outside text clipping', async () => {
  const css = await compiledStyles('./views/system/SystemSettings.vue')
  const rules: Record<string, Record<string, string>> = {}
  css.walkRules(rule => {
    if (!rule.selector.includes('.ssrf-whitelist-input')) return
    const declarations: Record<string, string> = {}
    rule.walkDecls(decl => { declarations[decl.prop] = decl.value })
    rules[rule.selector.replace(/\[data-v-ui-regression\]/g, '')] = declarations
  })
  assert.match(rules['.ssrf-whitelist-input .t-input']['padding-right'], /suffix-icon-width, 24px/)
  assert.equal(rules['.ssrf-whitelist-input .t-input.t-input--prefix > .t-input__prefix']['flex-wrap'], 'wrap')
  assert.equal(rules['.ssrf-whitelist-input .t-tag']['min-width'], '0')
  assert.match(rules['.ssrf-whitelist-input .t-tag']['max-width'], /100%/)
  assert.equal(rules['.ssrf-whitelist-input .t-tag--text']['text-overflow'], 'ellipsis')
  assert.equal(rules['.ssrf-whitelist-input .t-tag--text'].overflow, 'hidden')
  assert.equal(rules['.ssrf-whitelist-input .t-tag__icon-close']['flex-shrink'], '0')
  const source = readFileSync(new URL('./views/system/SystemSettings.vue', import.meta.url), 'utf8')
  assert.match(source, /ssrf-whitelist-input"\s*excess-tags-display-type="break-line"\s*:tag-props="\{ maxWidth: '100%' \}"/)
})

test('actual generated question delete control renders for saved questions and excludes legacy questions', async () => {
  const source = readFileSync(new URL('./components/doc-content.vue', import.meta.url), 'utf8')
  const { descriptor } = parse(source)
  let fragment = ''
  function visit(node: RootNode | TemplateChildNode) {
    if (node.type === NodeTypes.ELEMENT && node.tag === 't-popconfirm' && node.loc.source.includes('delete-question-btn')) fragment = node.loc.source
    if (node.type === NodeTypes.ROOT || node.type === NodeTypes.ELEMENT) node.children.forEach(visit)
  }
  visit(parseDOM(descriptor.template!.content))
  assert.ok(fragment)
  const compiled = compileTemplate({ source: fragment, filename: 'question-control.vue', id: 'question-delete', ssr: true, ssrCssVars: [] })
  assert.deepEqual(compiled.errors, [])
  const code = ts.transpileModule(compiled.code, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
  const module = { exports: {} as { ssrRender?: Function } }
  new Function('require', 'module', 'exports', code)(require, module, module.exports)
  for (const [id, visible] of [['question-a', true], ['legacy-question-a', false]] as const) {
    const app = createSSRApp({
      setup: () => ({ question: { id }, index: 0, chunk: { original: {} }, isDeleting: () => false, handleDeleteQuestion: () => {} }),
      ssrRender: module.exports.ssrRender,
    })
    app.config.globalProperties.$t = (key: string) => key
    const warnings: string[] = []
    app.config.warnHandler = message => { warnings.push(message) }
    const stub = defineComponent({ setup: (_, { slots }) => () => h('div', slots.default?.()) })
    for (const name of ['t-popconfirm', 't-button', 't-icon']) app.component(name, stub)
    const html = await renderToString(app)
    assert.equal(html.includes('delete-question-btn'), visible, id)
    assert.deepEqual(warnings, [], id)
  }
})
