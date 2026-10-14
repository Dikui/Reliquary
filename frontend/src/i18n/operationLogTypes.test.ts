import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import en from './locales/en-US'
import zh from './locales/zh-CN'

test('every backend operation log type has a Chinese and English display name', () => {
  const source = readFileSync(new URL('../../../internal/types/operation_log.go', import.meta.url), 'utf8')
  const declaration = source.match(/var OperationTypes = \[\]string\{([\s\S]*?)\n\}/)
  assert.ok(declaration, 'Read the authoritative backend operation list')
  const names = [...declaration[1].matchAll(/"([a-z_]+)"/g)].map(match => match[1])
  assert.ok(names.length > 20)
  assert.equal(new Set(names).size, names.length)
  const frontend = readFileSync(new URL('../api/operation-logs.ts', import.meta.url), 'utf8')
  const options = frontend.match(/export const OPERATION_TYPES = \[([\s\S]*?)\] as const/)
  assert.ok(options)
  const filterTypes = [...options[1].matchAll(/'([a-z_]+)'/g)].map(match => match[1])
  assert.deepEqual(filterTypes, names, 'Filter options and URL validation use every backend operation type in order')
  for (const [locale, bundle] of Object.entries({ 'en-US': en, 'zh-CN': zh })) {
    const labels = bundle.operationLogs.types as Record<string, string>
    for (const name of names) {
      assert.equal(typeof labels[name], 'string', `${locale}: missing operationLogs.types.${name}`)
      assert.ok(labels[name].trim(), `${locale}: empty operationLogs.types.${name}`)
      assert.notEqual(labels[name], name)
    }
    assert.deepEqual(Object.keys(labels).sort(), [...names].sort(), `${locale}: backend and locale operation types agree`)
  }
})
