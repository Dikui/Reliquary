import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileTemplate } from '@vue/compiler-sfc'
import { createSSRApp } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import { compiledSFCStyleRules } from '../utils/sfcStyleTestHelpers'
import zhCN from '../i18n/locales/zh-CN'
import enUS from '../i18n/locales/en-US'

test('dark sidebar CSS filters only the wordmark after scoped compilation', async () => {
  const rules = await compiledSFCStyleRules(new URL('./SideMenu.vue', import.meta.url))
  const filters = rules.filter(rule => 'filter' in rule.declarations)
  assert.equal(filters.length, 1)
  assert.deepEqual(filters.map(rule => [rule.selector, rule.declarations.filter]), [
    [':root[theme-mode="dark"] .logo', 'invert(1)'],
  ])
  assert.ok(!rules.some(rule => /^:root(?:\[[^\]]+\])*$/.test(rule.selector) && 'filter' in rule.declarations))
  assert.match(readFileSync(new URL('./SideMenu.vue', import.meta.url), 'utf8'), /<img class="logo"/)
})

const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const source = readFileSync(new URL('../views/knowledge/settings/KBChunkingDebug.vue', import.meta.url), 'utf8')
const statsTemplate = source.match(/<div class="chunk-stats">[\s\S]*?\n          <\/div>/)![0]
const compiled = compileTemplate({ source: statsTemplate, filename: 'chunk-stats.vue', id: 'ui09-stats', ssr: true, ssrCssVars: [] })
assert.deepEqual(compiled.errors, [])
const code = ts.transpileModule(compiled.code, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
const module = { exports: {} as { ssrRender: any } }
new Function('require', 'module', 'exports', code)(createRequire(import.meta.url), module, module.exports)

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: actual chunk statistics template renders localized minimum and maximum`, async () => {
    const app = createSSRApp({
      props: ['result'], ssrRender: module.exports.ssrRender,
    }, { result: { stats: { count: 2, avg_chars: 193, stddev_chars: 124, min_chars: 70, max_chars: 317, truncated_to: 10 } } })
    app.use(createI18n({ legacy: false, locale, messages }))
    const html = await renderToString(app)
    assert.ok(html.includes(locale === 'zh-CN' ? '最小 70' : 'Min 70'))
    assert.ok(html.includes(locale === 'zh-CN' ? '最大 317' : 'Max 317'))
    assert.ok(html.includes(locale === 'zh-CN' ? '已截断；总数 10' : 'truncated; total 10'))
    assert.ok(html.includes(locale === 'zh-CN' ? '块' : 'chunks'))
    assert.doesNotMatch(html, /knowledgeEditor\./)
    if (locale === 'zh-CN') assert.doesNotMatch(html, /\bmin\b|\bmax\b/i)
  })
}
