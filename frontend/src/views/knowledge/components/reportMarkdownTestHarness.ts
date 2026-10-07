import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import { marked } from 'marked'
import markedKatex from 'marked-katex-extension'
import * as markdownRenderer from '../../../utils/chatMarkdownRenderer'

const require = createRequire(import.meta.url)

function productionScript(url: URL) {
  const { descriptor } = parse(readFileSync(url, 'utf8'))
  return ts.createSourceFile(url.pathname, descriptor.scriptSetup!.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
}

function evaluateScript(code: string, names: string[], values: unknown[]) {
  const compiled = ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
  return new Function(...names, compiled)(...values)
}

// Run the real document component's global registrations before report setup.
export function registerDocumentMarkdownForReportTest() {
  const source = productionScript(new URL('../../../components/doc-content.vue', import.meta.url))
  const registrations = source.statements.filter(statement =>
    ts.isExpressionStatement(statement) && ts.isCallExpression(statement.expression)
      && statement.expression.expression.getText(source) === 'marked.use',
  )
  if (registrations.length !== 2) throw new Error('Document Markdown registration changed; update the test harness')
  evaluateScript(registrations.map(statement => statement.getText(source)).join('\n'), ['marked', 'markedKatex'], [marked, markedKatex])
}

// Execute the Wiki page's production renderer without mounting its unrelated UI.
export function wikiMarkdownForReportTest() {
  const source = productionScript(new URL('../wiki/WikiBrowser.vue', import.meta.url))
  const functions = source.statements.filter(statement =>
    ts.isFunctionDeclaration(statement) && ['renderMarkdown', 'slugDisplayName'].includes(statement.name?.text || ''),
  )
  if (functions.length !== 2) throw new Error('Wiki Markdown renderer changed; update the test harness')
  return evaluateScript(
    functions.map(statement => statement.getText(source)).join('\n') + '\nreturn renderMarkdown',
    ['marked', 'sanitizeMarkdownHTML', 'pages'],
    [marked, (html: string) => html, { value: [{ slug: 'entity/acme', title: 'Acme' }] }],
  ) as (markdown: string) => string
}

// Compile the production SFC and use the production Markdown renderer.
// DOMPurify requires a browser DOM, so sanitization is an explicit test seam.
export function reportMarkdownTestComponent(sanitizeHtml: (html: string) => string = html => html) {
  const filename = 'ReportMarkdown.vue'
  const { descriptor } = parse(readFileSync(new URL('./ReportMarkdown.vue', import.meta.url), 'utf8'), { filename })
  const script = compileScript(descriptor, { id: 'report-markdown-test' })
  const template = compileTemplate({
    id: 'report-markdown-test', filename, source: descriptor.template!.content,
    ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings },
  })
  if (template.errors.length) throw new Error(String(template.errors))
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    '@/utils/chatMarkdownRenderer': markdownRenderer,
    '@/utils/security': {
      escapeHTML: (text: string) => text.replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]!),
      safeMarkdownToHTML: (text: string) => text,
      sanitizeMarkdownHTML: sanitizeHtml,
    },
  }
  const evaluate = (code: string) => {
    const module = { exports: {} as any }
    const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', compiled)((name: string) => modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  component.setup = (props: object, context: object) => {
    component.testState = setup(props, context)
    return component.testState
  }
  return component
}

export function reportCitationClick(id: string): MouseEvent {
  return { target: { closest: () => ({ getAttribute: () => id }) } } as unknown as MouseEvent
}
