import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./SystemInfo.vue', import.meta.url), 'utf8')

test('system information rows stack and fit within narrow screens', () => {
  const mobile = source.match(/@media\s*\(max-width:\s*640px\)\s*\{([\s\S]*)\}\s*<\/style>/)?.[1]
  assert.ok(mobile, 'System information needs a narrow-screen layout')
  const rule = (selector: string) => {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const declarations = mobile.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1]
    assert.ok(declarations, `Missing mobile rule for ${selector}`)
    return declarations
  }
  assert.match(rule('.setting-row'), /flex-direction:\s*column/)
  assert.match(rule('.setting-row'), /gap:\s*12px/)
  for (const selector of ['.setting-info', '.setting-control']) {
    assert.match(rule(selector), /min-width:\s*0/)
    assert.match(rule(selector), /width:\s*100%/)
    assert.match(rule(selector), /max-width:\s*100%/)
  }
  assert.match(rule('.setting-info'), /padding-right:\s*0/)
  assert.match(rule('.setting-info'), /overflow-wrap:\s*anywhere/)
  assert.match(rule('.setting-control'), /justify-content:\s*flex-start/)
  assert.match(rule('.setting-control'), /flex-wrap:\s*wrap/)
  assert.match(rule('.info-value'), /text-align:\s*left/)
  assert.match(rule('.info-value'), /overflow-wrap:\s*anywhere/)
})

test('credential storage explanation and status use the responsive row layout', () => {
  assert.match(source, /<div class="setting-row">\s*<div class="setting-info">\s*<label>\{\{ \$t\('system\.credentialEncryptionLabel'\)[\s\S]*?class="desc"[\s\S]*?credentialEncryptionDisabledDescription[\s\S]*?<div class="setting-control">\s*<t-tag[\s\S]*?credentialEncryptionDisabled/)
})
