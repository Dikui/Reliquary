import assert from 'node:assert/strict'
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { compileScript, compileTemplate, parse, registerTS } from '@vue/compiler-sfc'
import ts from 'typescript'

registerTS(() => ts)
const sourceRoot = dirname(fileURLToPath(import.meta.url))
// Vue instance properties and globals provided by router and vue-i18n.
const instanceGlobals = new Set(['$t', '$route', '$router', '$attrs', '$emit', '$slots', '$refs', '$el', '$nextTick'])

function vueFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name)
    return entry.isDirectory() ? vueFiles(path) : entry.name.endsWith('.vue') ? [path] : []
  })
}

function undefinedTemplateIdentifiers(source: string, filename: string): string[] {
  const { descriptor, errors } = parse(source, { filename })
  assert.deepEqual(errors, [], filename)
  if (!descriptor.template) return []
  const script = descriptor.script || descriptor.scriptSetup ? compileScript(descriptor, {
    id: 'identifier-check',
    fs: { fileExists: existsSync, readFile: path => readFileSync(path, 'utf8') },
  }) : null
  const bindings = script?.bindings || {}
  const compiled = compileTemplate({
    source: descriptor.template.content, filename, id: 'identifier-check',
    compilerOptions: { bindingMetadata: bindings },
  })
  assert.deepEqual(compiled.errors, [], filename)
  const identifiers = new Set<string>()
  function visit(node: ts.Node) {
    if (ts.isPropertyAccessExpression(node) && ts.isIdentifier(node.expression)
      && node.expression.text === '_ctx' && !instanceGlobals.has(node.name.text)
      && !(node.name.text in bindings)) identifiers.add(node.name.text)
    ts.forEachChild(node, visit)
  }
  visit(ts.createSourceFile('render.js', compiled.code, ts.ScriptTarget.Latest, true, ts.ScriptKind.JS))
  return [...identifiers].sort()
}

test('all Vue templates reference defined component bindings or registered globals', () => {
  const failures = vueFiles(sourceRoot).flatMap(path => {
    const missing = undefinedTemplateIdentifiers(readFileSync(path, 'utf8'), path)
    return missing.map(name => `${relative(sourceRoot, path)}: ${name}`)
  })
  assert.deepEqual(failures, [], `Undefined template identifiers:\n${failures.join('\n')}`)
})

test('template identifier checks catch missing permissions even with ts-nocheck', () => {
  const source = `<script setup lang="ts">
// @ts-nocheck
const questions = [{ id: 'question-a' }]
</script>
<template><div v-for="question in questions" :key="question.id">
<button v-if="canDeleteGeneratedQuestion && !question.id.startsWith('legacy-')">Delete</button>
</div></template>`
  assert.deepEqual(undefinedTemplateIdentifiers(source, 'fixture.vue'), ['canDeleteGeneratedQuestion'])
  assert.deepEqual(undefinedTemplateIdentifiers(source.replace('canDeleteGeneratedQuestion && ', ''), 'fixture.vue'), [])
  assert.deepEqual(undefinedTemplateIdentifiers('<template>{{ $t("common.save") }} {{ Math.round(1.5) }}</template>', 'fixture.vue'), [])
})
