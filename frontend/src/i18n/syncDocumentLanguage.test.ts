import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import { syncDocumentLanguage } from './syncDocumentLanguage'

test('document language follows the initial i18n locale and subsequent language switches', () => {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: {} })
  const target = { documentElement: { lang: '' } } as Pick<Document, 'documentElement'>
  const stop = syncDocumentLanguage(i18n.global.locale, target)
  try {
    assert.equal(target.documentElement.lang, 'zh-CN')
    i18n.global.locale.value = 'en-US'
    assert.equal(target.documentElement.lang, 'en-US')
    i18n.global.locale.value = 'zh-CN'
    assert.equal(target.documentElement.lang, 'zh-CN')
  } finally {
    stop()
    i18n.dispose()
  }
})

test('i18n initialization installs document language synchronization centrally', () => {
  const source = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
  assert.match(source, /syncDocumentLanguage\(i18n.global.locale, document\)/)
})
