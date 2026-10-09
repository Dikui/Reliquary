import assert from 'node:assert/strict'
import test from 'node:test'
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse as parseSFC } from '@vue/compiler-sfc'
import { parse, NodeTypes, type RootNode, type TemplateChildNode } from '@vue/compiler-dom'

const sourceRoot = dirname(fileURLToPath(import.meta.url))

function vueFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name)
    return entry.isDirectory() ? vueFiles(path) : entry.name.endsWith('.vue') ? [path] : []
  })
}

function bareTemplates(source: string, filename: string): string[] {
  const { descriptor, errors } = parseSFC(source, { filename })
  assert.deepEqual(errors, [], `Invalid SFC: ${filename}`)
  if (!descriptor.template) return []
  const block = descriptor.template
  const findings: string[] = []
  function visit(node: RootNode | TemplateChildNode) {
    if (node.type === NodeTypes.ELEMENT) {
      // Vue removes v-pre from the AST, so inspect attributes in the opening tag too.
      const openingTag = node.loc.source.match(/^<[^>"']*(?:(?:"[^"]*"|'[^']*')[^>"']*)*>/)?.[0] || ''
      const hasVPre = /\sv-pre(?=[\s=>])/.test(openingTag.replace(/"[^"]*"|'[^']*'/g, '""'))
      const hasDirective = hasVPre || node.props.some(prop => prop.type === NodeTypes.DIRECTIVE
        || (prop.type === NodeTypes.ATTRIBUTE && (prop.name.startsWith('v-') || prop.name.startsWith('#'))))
      if (node.tag === 'template' && !hasDirective) {
        findings.push(`${filename}:${block.loc.start.line + node.loc.start.line - 1}`)
      }
    }
    if (node.type === NodeTypes.ROOT || node.type === NodeTypes.ELEMENT) node.children.forEach(visit)
  }
  visit(parse(block.content))
  return findings
}

test('Vue components have no nested template elements without directives', () => {
  const findings = vueFiles(sourceRoot).flatMap(path => bareTemplates(readFileSync(path, 'utf8'), relative(sourceRoot, path)))
  assert.deepEqual(findings, [], `Nested template elements hide their contents:\n${findings.join('\n')}`)
})

test('template structure scanning accepts Vue directives and rejects ordinary attributes', () => {
  for (const directive of ['v-if="visible"', 'v-else', 'v-else-if="visible"', 'v-for="item in items"',
    'v-slot:content', '#content', '#[slotName]', 'v-pre', 'v-once', 'v-show="visible"']) {
    assert.deepEqual(bareTemplates(`<template><div><template ${directive}><span>Content</span></template></div></template>`, 'fixture.vue'), [])
  }
  assert.deepEqual(bareTemplates('<template><div><template class="group"><span>Hidden</span></template></div></template>', 'fixture.vue'), ['fixture.vue:1'])
  assert.deepEqual(bareTemplates('<template><div><template title=" v-pre " /></div></template>', 'fixture.vue'), ['fixture.vue:1'])
  assert.deepEqual(bareTemplates('<template><div><!-- <template>Comment</template> -->&lt;template&gt;</div></template>', 'fixture.vue'), [])
})
