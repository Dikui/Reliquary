import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'

const groups = ['GeneralSettings.vue', 'OllamaSettings.vue', 'VaultStatus.vue', 'MaintenanceStatus.vue', 'SystemInfo.vue', 'SpaceInfo.vue', 'UserProfile.vue', 'ModelSettings.vue', 'ChatGPTSettings.vue', '../system/SystemSettings.vue', '../system/RuntimeQueues.vue', '../integrations/ApiIntegrationSettings.vue', '../library/OperationLogs.vue']
for (const group of groups) {
  test(`${group} uses shared controls without hidden legacy headings`, () => {
    const source = readFileSync(new URL(group, import.meta.url), 'utf8')
    assert.doesNotMatch(source, /<\/?t-[\w-]+\b|MessagePlugin|DialogPlugin|var\(--td-/)
    const { descriptor } = parse(source)
    assert.doesNotMatch(descriptor.template!.content, /<h2>/)
    assert.match(source, /settings-form/)
  })
}
test('shared settings form keeps two columns, square surfaces and a narrow single column', async () => {
  const source = readFileSync(new URL('../../styles/settings-form.css', import.meta.url), 'utf8')
  const result = await compileStyleAsync({ filename: 'settings-form.css', source, id: 'settings' })
  assert.deepEqual(result.errors, [])
  assert.match(result.code, /grid-template-columns:\s*224px minmax\(0, 1fr\)/)
  assert.match(result.code, /border-top:\s*2px solid var\(--rule\)/)
  assert.match(result.code, /@container settings-content/)
  assert.doesNotMatch(result.code, /\drem\b/)
})
