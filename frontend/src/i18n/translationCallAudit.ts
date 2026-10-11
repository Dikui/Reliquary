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
  /** All static prefixes, including separate conditional branches. */
  prefixes?: string[]
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

type KeyChoices = { literals: string[]; dynamic: boolean; prefixes: string[] }

function keyChoices(argument: ts.Expression, resolve: (name: string, node: ts.Node) => ts.Expression[], seen = new Set<ts.Node>()): KeyChoices {
  const node = unwrap(argument)
  if (seen.has(node)) return { literals: [], dynamic: true, prefixes: [] }
  const next = new Set(seen).add(node)
  const choices = (expression: ts.Expression) => keyChoices(expression, resolve, next)
  const merge = (items: KeyChoices[]): KeyChoices => ({
    literals: items.flatMap((item) => item.literals),
    dynamic: items.some((item) => item.dynamic),
    prefixes: items.flatMap((item) => item.prefixes),
  })
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    return { literals: [node.text], dynamic: false, prefixes: [] }
  }
  if (ts.isIdentifier(node)) {
    const values = resolve(node.text, node)
    if (values.length) return merge(values.map(choices))
  }
  if (ts.isConditionalExpression(node)) {
    return merge([choices(node.whenTrue), choices(node.whenFalse)])
  }
  if (ts.isTemplateExpression(node)) {
    const head = node.head.text
    const first = choices(node.templateSpans[0]!.expression)
    return { literals: [], dynamic: true, prefixes: head ? [head] : [...first.prefixes, ...first.literals] }
  }
  if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.PlusToken) {
    const left = choices(node.left)
    const right = choices(node.right)
    if (!left.dynamic && !right.dynamic) {
      return { literals: left.literals.flatMap((a) => right.literals.map((b) => a + b)), dynamic: false, prefixes: [] }
    }
    return { literals: [], dynamic: true, prefixes: [...left.literals, ...left.prefixes] }
  }
  if (ts.isElementAccessExpression(node)) return choices(node.expression)
  if (ts.isObjectLiteralExpression(node)) {
    return merge(node.properties.flatMap((property) => ts.isPropertyAssignment(property) ? [choices(property.initializer)] : []))
  }
  if (ts.isArrayLiteralExpression(node)) {
    return merge(node.elements.filter(ts.isExpression).map(choices))
  }
  return { literals: [], dynamic: true, prefixes: [] }
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

  const bindings: Array<{ name: string; scope: ts.Node; value?: ts.Expression }> = []
  function indexBindings(source: ts.SourceFile): void {
    function visit(node: ts.Node): void {
      if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.initializer) {
        let scope: ts.Node = node.parent
        while (!ts.isBlock(scope) && !ts.isSourceFile(scope) && !ts.isFunctionLike(scope)) scope = scope.parent
        bindings.push({ name: node.name.text, scope, value: node.initializer })
      }
      if (ts.isParameter(node) && ts.isIdentifier(node.name)) {
        bindings.push({ name: node.name.text, scope: node.parent, value: node.initializer })
      }
      ts.forEachChild(node, visit)
    }
    visit(source)
  }
  function resolve(name: string, node: ts.Node): ts.Expression[] {
    for (let scope: ts.Node | undefined = node; scope; scope = scope.parent) {
      const matches = bindings.filter((binding) => binding.name === name && binding.scope === scope)
      if (matches.length) return matches.flatMap((binding) => binding.value ? [binding.value] : [])
    }
    // Template expressions use script-level bindings from the same Vue file.
    return bindings.flatMap((binding) => binding.name === name && ts.isSourceFile(binding.scope) && binding.value ? [binding.value] : [])
  }

  function inspect(script: string, offset: number, expression = false): void {
    const wrapped = expression ? `(${script})` : script
    const scriptSource = ts.createSourceFile(file, wrapped, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
    indexBindings(scriptSource)
    function visit(node: ts.Node): void {
      if (ts.isCallExpression(node)) {
        const functionName = translationFunction(node.expression)
        const argument = node.arguments[0]
        if (functionName && argument) {
          const position = offset + node.getStart(scriptSource) - (expression ? 1 : 0)
          const { line, character } = completeSource.getLineAndCharacterOfPosition(position)
          const choices = keyChoices(argument, resolve)
          const prefixes = [...new Set(choices.prefixes)]
          calls.push({
            file,
            line: line + 1,
            column: character + 1,
            functionName,
            kind: functionName.endsWith('tm') ? 'tree' : 'message',
            expression: argument.getText(scriptSource),
            literalKeys: [...new Set(choices.literals)],
            dynamic: choices.dynamic,
            ...(prefixes.length ? { prefix: prefixes[0], prefixes } : {}),
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

/** Validate complete keys and the static tree before a runtime substitution. */
export function findTranslationReferenceFailures(
  calls: TranslationCall[],
  locales: Record<string, ReadonlySet<string>>,
  prefixExists?: (locale: string, key: string) => boolean,
) {
  return calls.flatMap((call) => {
    const references = [
      ...call.literalKeys.map((key) => ({ key, kind: call.kind === 'tree' ? 'prefix' : 'key' })),
      ...(call.prefixes ?? (call.prefix ? [call.prefix] : [])).map((prefix) => ({ key: prefix.replace(/\.$/, ''), kind: 'prefix' })),
    ].filter((reference) => !LITERAL_TRANSLATION_KEY_EXCEPTIONS.some((entry) => reference.kind === 'key' && entry.file === call.file && entry.key === reference.key))
    return references.flatMap((reference) => Object.entries(locales).flatMap(([locale, keys]) => {
      const exists = keys.has(reference.key) || (reference.kind === 'prefix' && (prefixExists
        ? prefixExists(locale, reference.key)
        : [...keys].some((key) => key.startsWith(`${reference.key}.`))))
      return exists ? [] : [{ ...call, ...reference, locale }]
    }))
  })
}

/** Existing missing messages are limited to this unchanged helper and these exact keys. */
export const LITERAL_TRANSLATION_KEY_EXCEPTIONS: ReadonlyArray<{
  file: string
  key: string
  reason: string
}> = [
  { file: 'utils/personalServerError.ts', key: 'model.imageProcessingUnsupported', reason: 'Already absent from both locales before naming cleanup; retain the existing image error behavior outside this repair.' },
  { file: 'utils/personalServerError.ts', key: 'model.imageProcessingNotFound', reason: 'Already absent from both locales before naming cleanup; retain the existing image error behavior outside this repair.' },
  { file: 'utils/personalServerError.ts', key: 'model.imageProcessingUnavailable', reason: 'Already absent from both locales before naming cleanup; retain the existing image error behavior outside this repair.' },
]
