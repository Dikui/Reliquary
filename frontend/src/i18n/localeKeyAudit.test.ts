import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { readdirSync, readFileSync } from 'node:fs'
import { parseStatusToTimelineStatus } from '../utils/entryProcessingStatus.ts'
import { CHUNKING_SAMPLES } from '../views/library/settings/chunkingSamples.ts'
import { KNOWN_MODEL_USAGE_BINDINGS, modelUsageBindingI18nKey } from '../api/model-usage.ts'
import { before, test } from 'node:test'

import {
  CRITICAL_LOCALE_KEYS,
  LOCALE_BUNDLES,
  alignLocaleBundleToReferenceKeys,
  collectI18nUsageFromSources,
  collectLocaleKeys,
  collectReferencedLocaleKeys,
  diffLocaleKeys,
  findAllLocaleMessageCompileErrors,
  findUsedKeysMissingInLocales,
  getLocaleValueAtPath,
  rebuildPrunedLocales,
  type LocaleName,
} from './localeKeyAudit.ts'
import {
  collectTranslationCallsFromSources,
  findTranslationReferenceFailures,
  type TranslationCall,
} from './translationCallAudit.ts'
import { DYNAMIC_TRANSLATION_ALLOWLIST, findUnlistedDynamicTranslationCalls } from './dynamicTranslationAllowlist.ts'

const REFERENCE_LOCALE: LocaleName = 'en-US'

let localeKeysByName: Record<LocaleName, Set<string>>
let referencedKeys: Set<string>
let translationCalls: TranslationCall[]

before(() => {
  translationCalls = collectTranslationCallsFromSources()
  const usage = collectI18nUsageFromSources()
  localeKeysByName = Object.fromEntries(
    Object.entries(LOCALE_BUNDLES).map(([name, bundle]) => [name, collectLocaleKeys(bundle)]),
  ) as Record<LocaleName, Set<string>>
  referencedKeys = collectReferencedLocaleKeys(LOCALE_BUNDLES[REFERENCE_LOCALE], usage)
})

test('locale bundles expose the same translation keys', () => {
  const referenceKeys = localeKeysByName[REFERENCE_LOCALE]
  const mismatches: string[] = []

  for (const [localeName, keys] of Object.entries(localeKeysByName) as Array<[LocaleName, Set<string>]>) {
    if (localeName === REFERENCE_LOCALE) continue
    const { missing, extra } = diffLocaleKeys(referenceKeys, keys)
    for (const key of missing) mismatches.push(`${localeName}: missing ${key}`)
    for (const key of extra) mismatches.push(`${localeName}: extra ${key}`)
  }

  assert.deepEqual(mismatches, [], mismatches.slice(0, 20).join('\n'))
})

test('critical runtime i18n trees are present', () => {
  const en = localeKeysByName['en-US']
  const missing = CRITICAL_LOCALE_KEYS.filter((key) => !en.has(key))
  assert.deepEqual(missing, [], missing.join('\n'))
})

test('referenced i18n keys used in app code exist in every locale', () => {
  const failures = findUsedKeysMissingInLocales(referencedKeys, localeKeysByName)
  assert.deepEqual(failures, [], failures.slice(0, 20).join('\n'))
})

test('literal translation calls and conditional keys exist in both locales', () => {
  const failures: string[] = []
  for (const call of translationCalls) {
    for (const key of call.literalKeys) {
      for (const [localeName, keys] of Object.entries(localeKeysByName) as Array<[LocaleName, Set<string>]>) {
        const exists = call.kind === 'tree'
          ? getLocaleValueAtPath(LOCALE_BUNDLES[localeName], key) !== undefined
          : keys.has(key)
        if (!exists) failures.push(`${call.file}:${call.line}:${call.column}: ${key} missing in ${localeName}`)
      }
    }
  }
  assert.deepEqual(failures, [], failures.join('\n'))
})

test('literal translation audit fails when a currently referenced key is deliberately missing from either locale', () => {
  const key = 'common.cancel'
  assert.ok(translationCalls.some(call => !call.dynamic && call.literalKeys.includes(key)))
  for (const locale of Object.keys(LOCALE_BUNDLES) as LocaleName[]) {
    const incomplete = { ...localeKeysByName, [locale]: new Set(localeKeysByName[locale]) }
    incomplete[locale].delete(key)
    const failures = findTranslationReferenceFailures(translationCalls, incomplete)
      .filter(failure => failure.kind === 'key')
    assert.ok(failures.some(failure => failure.locale === locale && failure.key === key))
    assert.throws(() => assert.deepEqual(failures, []), { name: 'AssertionError' })
  }
})

test('literal translation audit process returns failure for an intentionally missing locale key', () => {
  const auditModule = new URL('./translationCallAudit.ts', import.meta.url).href
  const localesModule = new URL('./localeKeyAudit.ts', import.meta.url).href
  for (const locale of Object.keys(LOCALE_BUNDLES)) {
    const script = `
      import assert from 'node:assert/strict'
      import { collectTranslationCallsFromSources, findTranslationReferenceFailures } from ${JSON.stringify(auditModule)}
      import { LOCALE_BUNDLES, collectLocaleKeys, getLocaleValueAtPath } from ${JSON.stringify(localesModule)}
      const keys = Object.fromEntries(Object.entries(LOCALE_BUNDLES).map(([locale, bundle]) => [locale, collectLocaleKeys(bundle)]))
      const calls = collectTranslationCallsFromSources()
      const audit = () => findTranslationReferenceFailures(calls, keys, (locale, key) => getLocaleValueAtPath(LOCALE_BUNDLES[locale], key) !== undefined)
      assert.deepEqual(audit(), [])
      keys[${JSON.stringify(locale)}].delete('common.cancel')
      assert.deepEqual(audit(), [])
    `
    const child = spawnSync(process.execPath, ['--import', 'tsx', '--input-type=module', '-e', script], { encoding: 'utf8' })
    assert.equal(child.status, 1, child.stderr)
    assert.match(child.stderr, /AssertionError/)
    assert.match(child.stderr, /common\.cancel/)
    assert.ok(child.stderr.includes(locale))
  }
})

test('all runtime translation expressions are explicitly allowlisted', () => {
  const unlisted = findUnlistedDynamicTranslationCalls(translationCalls)
  assert.deepEqual(unlisted, [], unlisted.map(call => `${call.file}:${call.line}: ${call.expression}`).join('\n'))
  for (const entry of DYNAMIC_TRANSLATION_ALLOWLIST) {
    assert.ok(entry.reason.trim())
    for (const expression of entry.expressions) {
      assert.ok(translationCalls.some(call => call.dynamic && call.file === entry.file && call.expression === expression),
        `Remove stale allowlist entry: ${entry.file}: ${expression}`)
    }
  }
})

test('runtime translation allowlist rejects a new concatenated key', () => {
  const call = { file: 'components/Unreviewed.vue', line: 1, column: 1, functionName: 't',
    kind: 'message' as const, expression: "'unreviewed.' + state", literalKeys: [], dynamic: true }
  assert.deepEqual(findUnlistedDynamicTranslationCalls([call]), [call])
})

test('dynamic translation prefixes and mapped keys exist in both locales', () => {
  const failures = findTranslationReferenceFailures(translationCalls, localeKeysByName,
    (locale, key) => getLocaleValueAtPath(LOCALE_BUNDLES[locale as LocaleName], key) !== undefined)
  assert.deepEqual(failures, [], failures.map(({ file, line, key, locale }) => `${file}:${line}: ${key} missing in ${locale}`).join('\n'))
})

test('indirect translation helpers and relative sample label maps resolve in both locales', () => {
  const sampleCall = translationCalls.find(call => call.file.endsWith('/LibraryChunkingDebug.vue') && call.expression.includes('p.labelKey'))
  assert.ok(sampleCall?.prefix)
  const keys = [
    ...CHUNKING_SAMPLES.map(sample => `${sampleCall.prefix}${sample.labelKey}`),
    ...[...KNOWN_MODEL_USAGE_BINDINGS, 'unknown'].map(modelUsageBindingI18nKey),
  ]
  assert.deepEqual(findUsedKeysMissingInLocales(keys, localeKeysByName), [])
})

const PARSER_ENGINE_NAMES = [
  'simple',
  'mineru',
  'mineru_cloud',
  'anydoc',
] as const

/** Worker pools / queues use dynamic te() paths in RuntimeQueues.vue; keep in sync with internal/types/task.go. */
const RUNTIME_WORKER_POOL_NAMES = [
  'core',
  'enrichment',
  'maintenance',
  'shared',
] as const

const RUNTIME_QUEUE_NAMES = [
  'default',
  'low',
  'entity',
] as const

const RUNTIME_TASK_TYPE_KEYS = [
  'documentProcess',
  'manualProcess',
  'batchReparse',
  'batchDelete',
  'indexDelete',
  'entityNameEmbeddingRebuild',
  'embeddingRebuild',
  'topicBackfill',
] as const

test('runtime queue worker pool and queue labels exist in every locale', () => {
  const failures: string[] = []
  const runtimeSource = readFileSync(new URL('../views/system/RuntimeQueues.vue', import.meta.url), 'utf8')
  const taskTypeMap = runtimeSource.match(/const runtimeTaskTypeKeys:[^=]+=[\s\S]*?\{([\s\S]*?)\n\}/)?.[1]
  assert.ok(taskTypeMap, 'Audit the actual task type labels rendered by the runtime queue')
  const renderedTaskTypes = [...taskTypeMap.matchAll(/:\s*'([^']+)'/g)].map((match) => match[1])
  assert.ok(renderedTaskTypes.length > 0, 'The runtime task type map must expose labels to audit')

  for (const pool of RUNTIME_WORKER_POOL_NAMES) {
    for (const [localeName, keys] of Object.entries(localeKeysByName) as Array<[LocaleName, Set<string>]>) {
      if (!keys.has(`system.globalSettings.runtime.pools.${pool}`)) {
        failures.push(`${localeName}: missing system.globalSettings.runtime.pools.${pool}`)
      }
      if (!keys.has(`system.globalSettings.runtime.poolDescriptions.${pool}`)) {
        failures.push(`${localeName}: missing system.globalSettings.runtime.poolDescriptions.${pool}`)
      }
    }
  }

  for (const queue of RUNTIME_QUEUE_NAMES) {
    for (const [localeName, keys] of Object.entries(localeKeysByName) as Array<[LocaleName, Set<string>]>) {
      if (!keys.has(`system.globalSettings.runtime.queueNames.${queue}`)) {
        failures.push(`${localeName}: missing system.globalSettings.runtime.queueNames.${queue}`)
      }
      if (!keys.has(`system.globalSettings.runtime.queueDescriptions.${queue}`)) {
        failures.push(`${localeName}: missing system.globalSettings.runtime.queueDescriptions.${queue}`)
      }
    }
  }

  for (const taskType of new Set([...RUNTIME_TASK_TYPE_KEYS, ...renderedTaskTypes])) {
    for (const [localeName, keys] of Object.entries(localeKeysByName) as Array<[LocaleName, Set<string>]>) {
      if (!keys.has(`system.globalSettings.runtime.tasks.taskTypes.${taskType}`)) {
        failures.push(`${localeName}: missing system.globalSettings.runtime.tasks.taskTypes.${taskType}`)
      }
    }
  }

  assert.deepEqual(failures, [], failures.slice(0, 20).join('\n'))
})

test('parser engine display keys exist in every locale', () => {
  const failures: string[] = []

  for (const engineName of PARSER_ENGINE_NAMES) {
    for (const [localeName, keys] of Object.entries(localeKeysByName) as Array<[LocaleName, Set<string>]>) {
      for (const suffix of ['name', 'desc'] as const) {
        const key = `librarySettings.parser.engines.${engineName}.${suffix}`
        if (!keys.has(key)) failures.push(`${localeName}: missing ${key}`)
      }
    }
  }

  assert.deepEqual(failures, [], failures.join('\n'))
})

test('current backend parsing statuses and error codes are localized and retained', () => {
  const parsingSource = readFileSync(new URL('../../../internal/errors/parse_error_codes.go', import.meta.url), 'utf8')
  const parsingCodes = [...parsingSource.matchAll(/ErrCode\w+\s*=\s*"([A-Z_]+)"/g)].map(match => match[1])
  const entrySource = readFileSync(new URL('../../../internal/types/entry.go', import.meta.url), 'utf8')
  const parseStatuses = [...entrySource.matchAll(/ParseStatus\w+\s*=\s*"([a-z_]+)"/g)].map(match => match[1])
  const spanSource = readFileSync(new URL('../../../internal/types/entry_span.go', import.meta.url), 'utf8')
  const spanStatuses = [...spanSource.matchAll(/SpanStatus\w+\s*=\s*"([a-z_]+)"/g)].map(match => match[1])
  assert.ok(parsingCodes.length && parseStatuses.length && spanStatuses.length, 'Audit parser codes and current statuses')
  const timelineStatuses = new Set([...spanStatuses, ...parseStatuses, 'draft'].map(parseStatusToTimelineStatus))

  const keys = [
    ...parsingCodes.flatMap(code => [`knowledgeStages.errorCode.${code}`, `knowledgeStages.errorCode.${code}_SUGGESTION`]),
    ...[...timelineStatuses].map(status => `knowledgeStages.status.${status}`),
  ]
  assert.deepEqual(findUsedKeysMissingInLocales(keys, localeKeysByName), [])
  for (const [localeName, localeKeys] of Object.entries(localeKeysByName)) {
    for (const code of parsingCodes) {
      for (const suffix of ['', '_SUGGESTION']) {
        const key = `knowledgeStages.errorCode.${code}${suffix}`
        const caseVariants = [...localeKeys].filter(candidate => candidate.toLowerCase() === key.toLowerCase())
        assert.deepEqual(caseVariants, [key], `${localeName}: preserve the backend error code casing without duplicate keys`)
      }
    }
  }

  const usage = collectI18nUsageFromSources()
  for (const localeName of Object.keys(LOCALE_BUNDLES) as LocaleName[]) {
    const retained = collectReferencedLocaleKeys(LOCALE_BUNDLES[localeName], usage)
    assert.deepEqual(keys.filter(key => !retained.has(key)), [], 'Dynamic error translations must survive locale pruning')
  }
})

test('locale messages compile with vue-i18n syntax rules', () => {
  const failures = findAllLocaleMessageCompileErrors(LOCALE_BUNDLES)
  const summary = failures.map(
    (failure) => `${failure.path}: ${failure.message}\n  ${failure.value}`,
  )
  assert.deepEqual(summary, [], summary.slice(0, 20).join('\n'))
})
