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

test('shared form styles keep controls and previews inside mobile rows', async () => {
  const css = readFileSync(new URL('./styles/settings-form.css', import.meta.url), 'utf8')
  assert.match(css, /@media \(max-width: 640px\)/)
  assert.match(css, /\.settings-form \.setting-row \{ grid-template-columns: minmax\(0, 1fr\)/)
  assert.match(css, /\.settings-form \.font-preview \{[^}]*width: 100%/)
  assert.match(css, /\.settings-form \.setting-control \{[^}]*min-width: 0/)
})

test('SSRF whitelist tags wrap and retain keyboard operable removal buttons', async () => {
  const source = readFileSync(new URL('./views/system/SystemSettings.vue', import.meta.url), 'utf8')
  assert.match(source, /<Tag v-for="entry in ssrfWhitelistModelValue\(\)"/)
  assert.match(source, /<Button[^>]*:aria-label="`\$\{t\('common.delete'\)\} \$\{entry\}`/)
  const css = await compiledStyles('./views/system/SystemSettings.vue')
  let found = false
  css.walkRules(rule => { if (rule.selector.includes('.ssrf-whitelist-input')) found = true })
  assert.ok(found)
})
