import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { parse as parseSFC } from '@vue/compiler-sfc'
import { NodeTypes, parse, type TemplateChildNode } from '@vue/compiler-dom'

function hasVisibleContent(node: TemplateChildNode): boolean {
  if (node.type === NodeTypes.TEXT) return !!node.content.trim()
  if (node.type === NodeTypes.INTERPOLATION) return true
  if (node.type !== NodeTypes.ELEMENT) return false
  const directives = node.props.filter(prop => prop.type === NodeTypes.DIRECTIVE)
  if (node.tag === 'template' && directives.some(prop => prop.name === 'slot' && prop.arg?.type === NodeTypes.SIMPLE_EXPRESSION && prop.arg.content !== 'default')) return false
  return ['svg', 'img', 'slot'].includes(node.tag)
    || /icon/i.test(node.tag)
    || node.tag === 'component' && directives.some(prop => prop.name === 'bind' && prop.arg?.type === NodeTypes.SIMPLE_EXPRESSION && prop.arg.content === 'is' && prop.exp?.type === NodeTypes.SIMPLE_EXPRESSION && /Icon/.test(prop.exp.content))
    || directives.some(prop => prop.name === 'html' || prop.name === 'text')
    || node.children.some(hasVisibleContent)
}

function vueFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name)
    return entry.isDirectory() ? vueFiles(path) : path.endsWith('.vue') ? [path] : []
  })
}

test('application buttons have visible content instead of only an accessible label', () => {
  const root = fileURLToPath(new URL('../../', import.meta.url))
  const failures: string[] = []
  for (const path of vueFiles(root)) {
    const { descriptor } = parseSFC(readFileSync(path, 'utf8'))
    if (!descriptor.template) continue
    const template = parse(descriptor.template.content)
    function inspect(node: TemplateChildNode) {
      if (node.type !== NodeTypes.ELEMENT) return
      if (['Button', 'button'].includes(node.tag) && !node.children.some(hasVisibleContent)) failures.push(`${relative(root, path)}:${node.loc.start.line}`)
      node.children.forEach(inspect)
    }
    template.children.forEach(inspect)
  }
  assert.deepEqual(failures, [], 'Button default slots must contain text or a visible icon; aria-label alone is insufficient')
})
