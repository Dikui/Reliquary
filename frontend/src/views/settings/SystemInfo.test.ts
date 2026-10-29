import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./SystemInfo.vue', import.meta.url), 'utf8')

test('system information rows stack and fit within narrow screens', () => {
  const shared = readFileSync(new URL('../../styles/settings-form.css', import.meta.url), 'utf8')
  assert.match(shared, /@media\s*\(max-width:\s*640px\)/)
  assert.match(shared, /grid-template-columns:\s*minmax\(0, 1fr\)/)
  assert.match(shared, /\.setting-control[^}]*min-width:\s*0/)
  assert.match(shared, /\.info-value[^}]*overflow-wrap:\s*anywhere/)

})

test('credential storage explanation and status use the responsive row layout', () => {
  assert.match(source, /<div class="setting-row">\s*<div class="setting-info">\s*<label>\{\{ \$t\('system\.credentialEncryptionLabel'\)[\s\S]*?class="desc"[\s\S]*?credentialEncryptionDisabledDescription[\s\S]*?<div class="setting-control">\s*<Tag[\s\S]*?credentialEncryptionDisabled/)
})
