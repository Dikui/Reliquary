import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync, readdirSync } from 'node:fs'
import { parse as parseSFC } from '@vue/compiler-sfc'
import { parse, NodeTypes, type RootNode, type TemplateChildNode } from '@vue/compiler-dom'
import ts from 'typescript'
import zh from './locales/zh-CN'
import en from './locales/en-US'
import { collectLocaleKeys, getLocaleValueAtPath } from './localeKeyAudit'
const retired = /文章|用户文档|整理结果|本人文档|\b(?:articles?|user documents?|personal documents?|organized results?|your documents)\b/i
// Protocol enum values, i18n keys, CSS selectors, import paths and code identifiers
// retain their stable spellings. Comments and test fixture text are not UI copy.
const technical = (value: string) => !/\s/.test(value) || /^(?:\.?\/|@\/|[A-Za-z]\w*\.[\w.]|[.#])/.test(value)
for (const [locale, bundle] of Object.entries({ 'zh-CN': zh, 'en-US': en })) test(`${locale}: material copy contains no retired names`, () => {
  const failures = [...collectLocaleKeys(bundle)].filter(key => retired.test(String(getLocaleValueAtPath(bundle, key)).replace(/\{[^}]+\}/g, '')))
  assert.deepEqual(failures, [])
})
test('production templates and displayed source literals contain no retired material names', () => {
  const failures: string[] = []
  function scanScript(source: string, path: string) {
    const file = ts.createSourceFile(path, source, ts.ScriptTarget.Latest, true)
    function visit(node: ts.Node) {
      if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && retired.test(node.text) && !technical(node.text)) failures.push(`${path}: ${node.text}`)
      ts.forEachChild(node, visit)
    }
    visit(file)
  }
  function walk(dir: URL) {
    for (const item of readdirSync(dir, { withFileTypes: true })) {
      const url = new URL(item.name + (item.isDirectory() ? '/' : ''), dir)
      if (item.isDirectory()) { if (!['i18n', '__tests__'].includes(item.name)) walk(url); continue }
      if (/\.test\.|TestHarness|TestHelpers/.test(item.name)) continue
      const source = readFileSync(url, 'utf8')
      if (item.name.endsWith('.vue')) {
        const { descriptor } = parseSFC(source)
        function visit(node: RootNode | TemplateChildNode) {
          if (node.type === NodeTypes.TEXT && retired.test(node.content)) failures.push(`${url.pathname}: ${node.content}`)
          if (node.type === NodeTypes.ROOT || node.type === NodeTypes.ELEMENT) node.children.forEach(visit)
        }
        if (descriptor.template) visit(parse(descriptor.template.content))
        scanScript((descriptor.script?.content || '') + (descriptor.scriptSetup?.content || ''), url.pathname)
      } else if (item.name.endsWith('.ts')) scanScript(source, url.pathname)
    }
  }
  walk(new URL('../', import.meta.url)); assert.deepEqual(failures, [])
})
