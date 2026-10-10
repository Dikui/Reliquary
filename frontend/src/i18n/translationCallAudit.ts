import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

import { parse as parseTemplate, NodeTypes, type TemplateChildNode } from '@vue/compiler-dom'
import { parse } from '@vue/compiler-sfc'
import ts from 'typescript'

const SOURCE_ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
const SOURCE_EXTENSIONS = /\.(vue|[cm]?[jt]sx?)$/
const TEST_FILE_PATTERN = /\.test\.[cm]?[jt]sx?$/
const TRANSLATION_FUNCTIONS = new Set(['t', '$t', 'te', '$te', 'tm', '$tm', 'globalSettingsText'])

export type TranslationCall = {
  file: string
  line: number
  column: number
  functionName: string
  kind: 'message' | 'tree'
  expression: string
  /** Direct literals and the literal branches of conditional keys. */
  literalKeys: string[]
  dynamic: boolean
  /** The literal portion before a dynamic template or concatenation. */
  prefix?: string
}

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) {
      if (entry.name === 'node_modules' || path.endsWith('/i18n/locales')) return []
      return sourceFiles(path)
    }
    return SOURCE_EXTENSIONS.test(path) && !TEST_FILE_PATTERN.test(path) ? [path] : []
  }).sort()
}

function unwrap(node: ts.Expression): ts.Expression {
  while (ts.isParenthesizedExpression(node) || ts.isAsExpression(node) || ts.isTypeAssertionExpression(node) || ts.isNonNullExpression(node)) {
    node = node.expression
  }
  return node
}

function keyChoices(argument: ts.Expression): { literals: string[]; dynamic: boolean; prefix?: string } {
  const node = unwrap(argument)
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    return { literals: [node.text], dynamic: false }
  }
  if (ts.isConditionalExpression(node)) {
    const yes = keyChoices(node.whenTrue)
    const no = keyChoices(node.whenFalse)
    return { literals: [...yes.literals, ...no.literals], dynamic: yes.dynamic || no.dynamic }
  }
  if (ts.isTemplateExpression(node)) return { literals: [], dynamic: true, prefix: node.head.text || undefined }
  if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.PlusToken) {
    const left = keyChoices(node.left)
    const right = keyChoices(node.right)
    if (!left.dynamic && !right.dynamic) {
      return { literals: left.literals.flatMap((a) => right.literals.map((b) => a + b)), dynamic: false }
    }
    return { literals: [], dynamic: true, prefix: left.literals[0] || left.prefix }
  }
  return { literals: [], dynamic: true }
}

function translationFunction(expression: ts.LeftHandSideExpression): string | undefined {
  if (ts.isIdentifier(expression) && TRANSLATION_FUNCTIONS.has(expression.text)) return expression.text
  if (ts.isPropertyAccessExpression(expression)) {
    const name = expression.name.text
    // Covers i18n.global.t(), composer.t() and this.$t(), including optional calls.
    if (TRANSLATION_FUNCTIONS.has(name)) return name
  }
  if (ts.isElementAccessExpression(expression) && expression.argumentExpression && ts.isStringLiteral(expression.argumentExpression)) {
    const name = expression.argumentExpression.text
    if (TRANSLATION_FUNCTIONS.has(name)) return name
  }
  return undefined
}

/** Parse source code rather than regexes, so comments, concatenations and multiline calls are classified correctly. */
export function collectTranslationCallsFromSource(content: string, file: string): TranslationCall[] {
  const calls: TranslationCall[] = []
  const completeSource = ts.createSourceFile(file, content, ts.ScriptTarget.Latest, true)

  function inspect(script: string, offset: number, expression = false): void {
    const wrapped = expression ? `(${script})` : script
    const scriptSource = ts.createSourceFile(file, wrapped, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
    function visit(node: ts.Node): void {
      if (ts.isCallExpression(node)) {
        const functionName = translationFunction(node.expression)
        const argument = node.arguments[0]
        if (functionName && argument) {
          const position = offset + node.getStart(scriptSource) - (expression ? 1 : 0)
          const { line, character } = completeSource.getLineAndCharacterOfPosition(position)
          const choices = keyChoices(argument)
          calls.push({
            file,
            line: line + 1,
            column: character + 1,
            functionName,
            kind: functionName.endsWith('tm') ? 'tree' : 'message',
            expression: argument.getText(scriptSource),
            literalKeys: [...new Set(choices.literals)],
            dynamic: choices.dynamic,
            ...(choices.prefix ? { prefix: choices.prefix } : {}),
          })
        }
      }
      ts.forEachChild(node, visit)
    }
    visit(scriptSource)
  }

  if (!file.endsWith('.vue')) {
    inspect(content, 0)
    return calls
  }

  const { descriptor, errors } = parse(content, { filename: file })
  if (errors.length) throw new Error(`Cannot audit ${file}: ${errors.map(String).join(', ')}`)
  for (const script of [descriptor.script, descriptor.scriptSetup]) {
    if (script) inspect(script.content, script.loc.start.offset)
  }
  if (descriptor.template) {
    const template = descriptor.template
    function visitTemplate(node: TemplateChildNode): void {
      if (node.type === NodeTypes.INTERPOLATION && node.content.type === NodeTypes.SIMPLE_EXPRESSION) {
        inspect(node.content.content, template.loc.start.offset + node.content.loc.start.offset, true)
      }
      if (node.type === NodeTypes.ELEMENT) {
        for (const property of node.props) {
          if (property.type !== NodeTypes.DIRECTIVE) continue
          for (const exp of [property.exp, property.arg]) {
            if (exp?.type === NodeTypes.SIMPLE_EXPRESSION && !exp.isStatic) {
              inspect(exp.content, template.loc.start.offset + exp.loc.start.offset, true)
            }
          }
        }
        node.children.forEach(visitTemplate)
      }
    }
    parseTemplate(template.content).children.forEach(visitTemplate)
  }
  return calls.sort((a, b) => a.line - b.line || a.column - b.column)
}

export function collectTranslationCallsFromSources(rootDir = SOURCE_ROOT): TranslationCall[] {
  return sourceFiles(rootDir).flatMap((file) =>
    collectTranslationCallsFromSource(readFileSync(file, 'utf8'), relative(rootDir, file)),
  )
}

export function collectLiteralTranslationKeys(calls: TranslationCall[]): Set<string> {
  return new Set(calls.filter((call) => call.kind === 'message').flatMap((call) => call.literalKeys))
}

/** No literal call currently needs an exception. Dynamic keys are separately reported for manual review. */
export const LITERAL_TRANSLATION_KEY_EXCEPTIONS: ReadonlyArray<{
  file: string
  key: string
  reason: string
}> = []
