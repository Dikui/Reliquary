import {
  LOCALE_BUNDLES,
  collectLocaleKeys,
  getLocaleValueAtPath,
  type LocaleName,
} from './localeKeyAudit.ts'
import {
  collectTranslationCallsFromSources,
  findTranslationReferenceFailures,
} from './translationCallAudit.ts'

const calls = collectTranslationCallsFromSources()
const localeKeys = Object.fromEntries(
  Object.entries(LOCALE_BUNDLES).map(([name, bundle]) => [name, collectLocaleKeys(bundle)]),
) as Record<LocaleName, Set<string>>

const missing = calls.flatMap((call) => call.literalKeys.flatMap((key) => {
  return (Object.keys(LOCALE_BUNDLES) as LocaleName[]).flatMap((locale) => {
    const exists = call.kind === 'tree'
      ? getLocaleValueAtPath(LOCALE_BUNDLES[locale], key) !== undefined
      : localeKeys[locale].has(key)
    return exists ? [] : [`${call.file}:${call.line}:${call.column}: ${key} missing in ${locale}`]
  })
}))

console.log(`Translation calls: ${calls.length}`)
console.log(`Literal keys: ${new Set(calls.flatMap((call) => call.literalKeys)).size}`)
console.log(`Missing literal references: ${missing.length}`)
for (const failure of missing) console.log(failure)

const referenceFailures = findTranslationReferenceFailures(calls, localeKeys,
  (locale, key) => getLocaleValueAtPath(LOCALE_BUNDLES[locale as LocaleName], key) !== undefined)
console.log(`Missing translation keys or prefixes: ${referenceFailures.length}`)
for (const failure of referenceFailures) {
  console.log(`${failure.file}:${failure.line}:${failure.column}: ${failure.kind} ${failure.key} missing in ${failure.locale}`)
}

const dynamic = calls.filter((call) => call.dynamic)
console.log(`\nDynamic calls requiring manual review: ${dynamic.length}`)
for (const call of dynamic) {
  const matches = call.prefix
    ? (Object.keys(localeKeys) as LocaleName[]).map((locale) =>
        `${locale}: ${[...localeKeys[locale]].filter((key) => key.startsWith(call.prefix!)).length} keys`,
      ).join(', ')
    : 'variable key, inspect its callers or key map'
  console.log(`${call.file}:${call.line}:${call.column}: ${call.functionName}(${call.expression})`)
  console.log(`  ${call.prefix ? `prefix ${call.prefix}, ` : ''}${matches}`)
}

if (missing.length || referenceFailures.length) process.exitCode = 1
