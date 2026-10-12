import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import { translatePersonalServerMessage } from './personalServerError'

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: image model errors resolve to readable translated messages`, () => {
    const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
    for (const [code, key] of [[2301, 'imageProcessingUnsupported'], [2302, 'imageProcessingNotFound'], [2303, 'imageProcessingUnavailable']] as const) {
      assert.equal(translatePersonalServerMessage('Server error', i18n.global.t, code), (locale === 'zh-CN' ? zh : en).model[key])
      assert.ok(!i18n.global.t(`model.${key}`).startsWith('model.'))
    }
    assert.equal(translatePersonalServerMessage('Other error', i18n.global.t, 2300), 'Other error')
  })
}
