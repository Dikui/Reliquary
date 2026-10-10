import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import enUS from '../../i18n/locales/en-US'
import zhCN from '../../i18n/locales/zh-CN'

// Execute the card's actual label functions with the production translations.
const source = readFileSync(new URL('./ModelSettings.vue', import.meta.url), 'utf8')
const labelFunctions = source.slice(source.indexOf('const sourceLabel ='), source.indexOf('const modelDisplayName ='))
const compiled = ts.transpileModule(`${labelFunctions}\nexport { vendorLabel }`, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

for (const [locale, messages] of Object.entries({ 'zh-CN': zhCN, 'en-US': enUS })) {
  const i18n = createI18n({ legacy: false, locale, messages: { [locale]: messages } })
  const exports: { vendorLabel?: (model: object) => string } = {}
  new Function('t', 'te', 'exports', compiled)(
    (key: string) => i18n.global.t(key),
    (key: string) => i18n.global.te(key),
    exports,
  )
  const label = exports.vendorLabel!

  test(`${locale} retired and unknown provider card labels use the generic display name`, () => {
    for (const provider of ['azure_openai', 'lkeap', 'jina', 'retired-provider']) {
      const model = Object.freeze({ source: 'remote', provider, baseUrl: 'https://custom.example/v1', _modelType: 'chat' })
      assert.equal(label(model), messages.model.editor.providers.generic.label)
      assert.equal(model.provider, provider)
      assert.equal(model.baseUrl, 'https://custom.example/v1')
    }
  })

  test(`${locale} retained provider card labels and existing custom label stay unchanged`, () => {
    for (const [provider, preset] of Object.entries(messages.model.editor.providers)) {
      const expected = provider === 'generic' ? messages.modelSettings.source.custom : preset.label
      assert.equal(label({ source: 'remote', provider, _modelType: 'chat' }), expected)
    }
    assert.equal(label({ source: 'local', provider: 'azure_openai' }), 'Ollama')
    assert.equal(label({ source: 'remote', provider: '', _modelType: 'chat' }), messages.modelSettings.source.remote)
    assert.equal(label({ source: 'remote', _modelType: 'vllm' }), messages.modelSettings.source.openaiCompatible)
  })
}
