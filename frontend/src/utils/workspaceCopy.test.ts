import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import enUS from '../i18n/locales/en-US'
import zhCN from '../i18n/locales/zh-CN'
import { workspaceName, workspaceDescription } from './workspaceCopy'

for (const locale of ['zh-CN', 'en-US'] as const) {
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  test(`${locale}: server workspace defaults are localized only for display`, () => {
    assert.equal(workspaceName('Personal workspace', i18n.global.t), locale === 'zh-CN' ? '个人空间' : 'Personal workspace')
    assert.equal(workspaceDescription('Personal knowledge', i18n.global.t), locale === 'zh-CN' ? '个人知识' : 'Personal knowledge')
    assert.equal(workspaceName('My reading desk', i18n.global.t), 'My reading desk')
    assert.equal(workspaceDescription('My notes', i18n.global.t), 'My notes')
    assert.equal(workspaceName(null, i18n.global.t), '')
    assert.equal(workspaceDescription(undefined, i18n.global.t), '')
  })
}
