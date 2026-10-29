import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileStyleAsync } from '@vue/compiler-sfc'

test('global popup wrapping overrides TDesign only for English document languages', async () => {
  const filename = new URL('./popup-word-wrapping.css', import.meta.url)
  const result = await compileStyleAsync({ filename: filename.pathname, id: 'popup-wrap', source: readFileSync(filename, 'utf8') })
  assert.deepEqual(result.errors, [])
  const selectors: string[] = []
  result.rawResult!.root.walkRules(rule => {
    selectors.push(rule.selector)
    const declarations: Record<string, string> = {}
    rule.walkDecls(decl => { declarations[decl.prop] = decl.value })
    assert.equal(declarations['word-break'], 'normal')
    assert.equal(declarations['overflow-wrap'], 'break-word')
  })
  assert.deepEqual(selectors, ['html[lang^="en"] .t-popup__content,\nhtml[lang^="en"] .t-message,\nhtml[lang^="en"] .t-notification__content'])
  const main = readFileSync(new URL('../main.ts', import.meta.url), 'utf8')
  const imports = [...main.matchAll(/import\s+["']([^"']+\.css)["']/g)].map(match => match[1])
  assert.ok(imports.indexOf('./assets/popup-word-wrapping.css') > imports.indexOf('tdesign-vue-next/dist/tdesign.css'))
  assert.ok(imports.includes('tdesign-vue-next/dist/tdesign.css'))
  const settings = readFileSync(new URL('../views/system/SystemSettings.vue', import.meta.url), 'utf8')
  assert.match(settings, /:content="ssrfPopconfirm.content"/)
  assert.doesNotMatch(settings, /ssrf-confirm-body/)
})
