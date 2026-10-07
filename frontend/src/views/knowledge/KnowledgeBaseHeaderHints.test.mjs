import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import less from 'less'
import postcss from 'postcss'
import ts from 'typescript'
import enUS from '../../i18n/locales/en-US.ts'
import zhCN from '../../i18n/locales/zh-CN.ts'

const require = createRequire(import.meta.url)
const filename = 'KnowledgeBase.vue'
const source = readFileSync(new URL('./KnowledgeBase.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename })
const hints = descriptor.template.content.match(/<p v-if="(?:unsupportedFileTypes.length|missingStorageEngine)"[\s\S]*?<\/p>/g)
assert.equal(hints?.length, 2, 'both knowledge header warnings must be covered')
const template = compileTemplate({ id: 'header-hints-test', filename, source: `<div>${hints.join('\n')}</div>` })
assert.deepEqual(template.errors, [])
const compiled = ts.transpileModule(template.code, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
const exports = {}
new Function('require', 'exports', compiled)(require, exports)

function findNodes(node, predicate) {
  if (!node || typeof node !== 'object') return []
  return [ ...(predicate(node) ? [node] : []),
    ...(Array.isArray(node.children) ? node.children.flatMap(child => findNodes(child, predicate)) : []),
  ]
}

for (const locale of ['zh-CN', 'en-US']) {
  test(`${locale} header warnings keep full descriptions and working configuration buttons`, async () => {
    const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
    const clicks = { parser: 0, storage: 0 }
    const context = {
      unsupportedFileTypes: ['doc', 'docx', 'pdf', 'ppt', 'pptx', 'xls', 'xlsx', 'epub', 'html', 'htm', 'md', 'txt', 'csv', 'json', 'xml', 'png', 'jpg', 'jpeg', 'webp'],
      missingStorageEngine: true,
      $t: (...args) => i18n.global.t(...args),
      goToParserSettings: () => { clicks.parser++ },
      handleOpenKBSettings: () => { clicks.storage++ },
    }
    let tree
    const component = defineComponent({ setup: () => () => { tree = exports.render(context, []); return tree } })
    const app = createSSRApp(component)
    app.component('t-icon', defineComponent({ setup: () => () => h('svg') }))
    const html = await renderToString(app)
    assert.ok(html.includes('.webp'), 'the long parser type list is retained')
    assert.ok(html.includes(i18n.global.t('knowledgeBase.missingStorageEngine')))

    const paragraphs = findNodes(tree, node => node.type === 'p')
    assert.equal(paragraphs.length, 2)
    for (const paragraph of paragraphs) {
      assert.ok(paragraph.props.class.split(' ').includes('knowledge-header-hint'))
      assert.equal(findNodes(paragraph, node => node.props?.class === 'knowledge-header-hint__text').length, 1)
      const [button] = findNodes(paragraph, node => node.type === 'button')
      assert.ok(button, 'configuration is a keyboard-accessible button')
      assert.equal(button.props.type, 'button')
      assert.ok(button.props.class.split(' ').includes('knowledge-header-hint__action'))
      let stopped = false
      button.props.onClick({ stopPropagation() { stopped = true } })
      assert.ok(stopped, 'the action runs once without bubbling into the hint')
    }
    assert.deepEqual(clicks, { parser: 1, storage: 1 })
    for (const key of ['knowledgeBase.goToParserSettings', 'knowledgeBase.goToStorageSettings']) {
      assert.ok(html.includes(i18n.global.t(key)))
    }
  })
}

test('at 390px both hints wrap text and keep actions inside a shrinkable grid column', async () => {
  const { css } = await less.render(descriptor.styles.map(style => style.content).join('\n'))
  const stylesheet = postcss.parse(css)
  // Resolve the applicable class rules in cascade order, including specificity.
  function stylesAt(width, ownClasses, ancestorClasses) {
    const resolved = {}
    const priorities = {}
    stylesheet.walkRules(rule => {
      for (let parent = rule.parent; parent; parent = parent.parent) {
        if (parent.type === 'atrule' && parent.name === 'media') {
          const maxWidth = parent.params.match(/max-width:\s*(\d+)px/)
          if (!maxWidth || width > Number(maxWidth[1])) return
        }
      }
      for (const selector of rule.selectors) {
        if (selector.includes(':')) continue
        const parts = selector.trim().split(/\s+/)
        const target = parts.pop()
        const targetClasses = [...target.matchAll(/\.([\w-]+)/g)].map(match => match[1])
        const ancestors = [...parts.join(' ').matchAll(/\.([\w-]+)/g)].map(match => match[1])
        if (!targetClasses.length || !targetClasses.every(name => ownClasses.includes(name))
          || !ancestors.every(name => ancestorClasses.includes(name))) continue
        const specificity = targetClasses.length + ancestors.length
        rule.walkDecls(declaration => {
          if ((priorities[declaration.prop] ?? -1) <= specificity) {
            priorities[declaration.prop] = specificity
            resolved[declaration.prop] = declaration.value
          }
        })
      }
    })
    return resolved
  }

  for (const [hint, action] of [['parser-hint', 'parser-hint-link'], ['storage-engine-warning', 'warning-link']]) {
    const hintClasses = [hint, 'knowledge-header-hint']
    const parentClasses = ['document-header', ...hintClasses]
    const narrowHint = stylesAt(390, hintClasses, ['document-header'])
    assert.equal(narrowHint.display, 'grid')
    assert.equal(narrowHint['grid-template-columns'], 'auto minmax(0, 1fr)')
    assert.equal(narrowHint['min-width'], '0')
    assert.equal(narrowHint['max-width'], '100%')
    const text = stylesAt(390, ['knowledge-header-hint__text'], parentClasses)
    assert.equal(text['min-width'], '0')
    assert.equal(text['overflow-wrap'], 'anywhere')
    assert.equal(text['white-space'], 'normal')
    const button = stylesAt(390, [action, 'knowledge-header-hint__action'], parentClasses)
    assert.equal(button['grid-column'], '2')
    assert.equal(button['max-width'], '100%')
    assert.equal(button['white-space'], 'normal')
    assert.equal(button['margin-left'], '0')
    assert.equal(stylesAt(1024, hintClasses, ['document-header']).display, 'flex')
  }
})
