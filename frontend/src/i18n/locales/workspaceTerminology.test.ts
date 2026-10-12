import assert from 'node:assert/strict'
import test from 'node:test'

import enUS from './en-US.ts'
import zhCN from './zh-CN.ts'

type LocaleValue = string | Record<string, unknown> | unknown[]

function collectStrings(value: LocaleValue, path = ''): Array<{ path: string; value: string }> {
  if (typeof value === 'string') return [{ path, value }]
  if (Array.isArray(value)) {
    return value.flatMap((item, index) =>
      collectStrings(item as LocaleValue, `${path}[${index}]`),
    )
  }
  if (value && typeof value === 'object') {
    return Object.entries(value).flatMap(([key, item]) =>
      collectStrings(item as LocaleValue, path ? `${path}.${key}` : key),
    )
  }
  return []
}

function withoutTechnicalSpaceTokens(value: string): string {
  return value
    .replace(/\{space(?:Id)?\}/g, '')
    .replace(/\bspace_id\b/gi, '')
    .replace(/\bX-Space-ID\b/g, '')
}

const localeChecks = [
  { name: 'zh-CN', locale: zhCN, forbidden: /租户|团队|成员|知识图谱/ },
  { name: 'en-US', locale: enUS, forbidden: /\b(?:tenants?|teams?|members?)\b|knowledge graph/i },
]

test('user-facing locale values use workspace terminology', () => {
  for (const check of localeChecks) {
    const legacyValues = collectStrings(check.locale)
      .filter(({ value }) => check.forbidden.test(withoutTechnicalSpaceTokens(value)))
      .map(({ path, value }) => `${check.name}:${path}=${value}`)

    assert.deepEqual(
      legacyValues,
      [],
      `${check.name} still contains user-facing legacy workspace terminology`,
    )
  }
})
