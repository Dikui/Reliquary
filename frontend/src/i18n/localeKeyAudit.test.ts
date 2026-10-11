import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { parseStatusToTimelineStatus } from '../utils/knowledgeProcessingStatus.ts'
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
  LITERAL_TRANSLATION_KEY_EXCEPTIONS,
  collectTranslationCallsFromSources,
  type TranslationCall,
} from './translationCallAudit.ts'

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
      const exception = LITERAL_TRANSLATION_KEY_EXCEPTIONS.find((entry) => entry.file === call.file && entry.key === key)
      if (exception) {
        assert.ok(exception.reason.trim(), `Exception requires a reason: ${call.file}:${key}`)
        continue
      }
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
  'sync',
  'low',
  'entity',
] as const

const RUNTIME_TASK_TYPE_KEYS = [
  'documentProcess',
  'manualProcess',
  'sync',
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
        const key = `kbSettings.parser.engines.${engineName}.${suffix}`
        if (!keys.has(key)) failures.push(`${localeName}: missing ${key}`)
      }
    }
  }

  assert.deepEqual(failures, [], failures.join('\n'))
})

test('current backend parsing statuses and error codes are localized and retained', () => {
  const parsingSource = readFileSync(new URL('../../../internal/errors/parse_error_codes.go', import.meta.url), 'utf8')
  const parsingCodes = [...parsingSource.matchAll(/ErrCode\w+\s*=\s*"([A-Z_]+)"/g)].map(match => match[1])
  const fetchDirectory = new URL('../../../internal/infrastructure/webfetch/', import.meta.url)
  const fetchSource = readdirSync(fetchDirectory)
    .filter(file => file.endsWith('.go') && !file.endsWith('_test.go'))
    .map(file => readFileSync(new URL(file, fetchDirectory), 'utf8'))
    .join('\n')
  const fetchCodes = [...new Set([...fetchSource.matchAll(/\bError\w+\s+ErrorCode\s*=\s*"([^"]+)"/g)].map(match => match[1]))]
  assert.ok(fetchCodes.length, 'Web fetch error code declarations must be scanned')
  const syncSource = readFileSync(new URL('../../../internal/application/service/datasource.go', import.meta.url), 'utf8')
  const syncCodes = [...new Set([...syncSource.matchAll(/\bCode:\s*"([a-z_]+)"/g)].map(match => match[1]))]
  assert.ok(parsingCodes.length, 'Parser error code declarations must be scanned')
  assert.ok(syncCodes.length, 'Datasource error code declarations must be scanned')
  const knowledgeSource = readFileSync(new URL('../../../internal/types/knowledge.go', import.meta.url), 'utf8')
  const parseStatuses = [...knowledgeSource.matchAll(/ParseStatus\w+\s*=\s*"([a-z_]+)"/g)].map(match => match[1])
  const spanSource = readFileSync(new URL('../../../internal/types/knowledge_span.go', import.meta.url), 'utf8')
  const spanStatuses = [...spanSource.matchAll(/SpanStatus\w+\s*=\s*"([a-z_]+)"/g)].map(match => match[1])
  const manualStatuses = new Map([...knowledgeSource.matchAll(/(ManualKnowledgeStatus\w+)\s*=\s*"([a-z_]+)"/g)].map(match => [match[1], match[2]]))
  const createSource = readFileSync(new URL('../../../internal/application/service/knowledge_create.go', import.meta.url), 'utf8')
  const manualParseStatuses = [...createSource.matchAll(/(?:ParseStatus:\s*|\.ParseStatus\s*=\s*)types\.(ManualKnowledgeStatus\w+)/g)].map(match => {
    const value = manualStatuses.get(match[1])
    assert.ok(value, `Resolve the manual parse status ${match[1]}`)
    return value
  })
  assert.ok(parseStatuses.length && spanStatuses.length && manualParseStatuses.length, 'Audit both spans and actual knowledge parse statuses')
  const timelineStatuses = new Set([...spanStatuses, ...parseStatuses, ...manualParseStatuses].map(parseStatusToTimelineStatus))

  const keys = [
    ...[...parsingCodes, ...fetchCodes].flatMap(code => [`knowledgeStages.errorCode.${code}`, `knowledgeStages.errorCode.${code}_SUGGESTION`]),
    ...syncCodes.map(code => `datasource.syncError.${code}`),
    ...[...timelineStatuses].map(status => `knowledgeStages.status.${status}`),
  ]
  assert.deepEqual(findUsedKeysMissingInLocales(keys, localeKeysByName), [])
  for (const [localeName, localeKeys] of Object.entries(localeKeysByName)) {
    for (const code of [...parsingCodes, ...fetchCodes]) {
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
