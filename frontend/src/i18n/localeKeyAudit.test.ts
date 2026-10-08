import assert from 'node:assert/strict'
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

const REFERENCE_LOCALE: LocaleName = 'en-US'

let localeKeysByName: Record<LocaleName, Set<string>>
let referencedKeys: Set<string>

before(() => {
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

const PARSER_ENGINE_NAMES = [
  'builtin',
  'simple',
  'mineru',
  'mineru_cloud',
  'paddleocr_vl',
  'paddleocr_vl_cloud',
  'markitdown',
  'opendataloader',
] as const

/** Worker pools / queues use dynamic te() paths in RuntimeQueues.vue; keep in sync with internal/types/task.go. */
const RUNTIME_WORKER_POOL_NAMES = [
  'core',
  'postprocess',
  'enrichment',
  'maintenance',
  'shared',
  'wiki',
] as const

const RUNTIME_QUEUE_NAMES = [
  'default',
  'chat_attachment',
  'postprocess',
  'summary',
  'sync',
  'low',
  'multimodal',
  'graph',
  'question',
  'wiki',
] as const

const RUNTIME_TASK_TYPE_KEYS = [
  'documentProcess',
  'manualProcess',
  'temporaryDocumentProcess',
  'postProcess',
  'summary',
  'tableSummary',
  'question',
  'multimodal',
  'sync',
  'batchReparse',
  'batchDelete',
  'move',
  'indexDelete',
  'kbClone',
  'kbDelete',
  'wikiIngest',
  'wikiFinalize',
] as const

test('runtime queue worker pool and queue labels exist in every locale', () => {
  const failures: string[] = []

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

  for (const taskType of RUNTIME_TASK_TYPE_KEYS) {
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

test('locale messages compile with vue-i18n syntax rules', () => {
  const failures = findAllLocaleMessageCompileErrors(LOCALE_BUNDLES)
  const summary = failures.map(
    (failure) => `${failure.path}: ${failure.message}\n  ${failure.value}`,
  )
  assert.deepEqual(summary, [], summary.slice(0, 20).join('\n'))
})
