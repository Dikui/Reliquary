import assert from 'node:assert/strict'
import test from 'node:test'

import { collectTranslationCallsFromSource, findTranslationReferenceFailures } from './translationCallAudit.ts'

test('translation call audit distinguishes multiline literal keys, dynamic prefixes and comments', () => {
  const calls = collectTranslationCallsFromSource([
    '// t("removed.comment")',
    'const comment = "t(\\"removed.string\\")"',
    't(\n "common.save"\n)',
    'i18n.global.t(`common.close`)',
    'te("model." + provider)',
    't(enabled ? "common.enabled" : "common.disabled")',
    'composer["t"]("common.cancel")',
  ].join('\n'), 'example.ts')

  assert.deepEqual(calls.map((call) => call.literalKeys), [
    ['common.save'], ['common.close'], [], ['common.enabled', 'common.disabled'], ['common.cancel'],
  ])
  assert.equal(calls[0].line, 3)
  assert.equal(calls[2].dynamic, true)
  assert.equal(calls[2].prefix, 'model.')
})

test('dynamic translation audit keeps conditional prefixes and follows scoped variables and key maps', () => {
  const calls = collectTranslationCallsFromSource([
    'const labels = { first: "model.valid", second: "missingEditor.tab" }',
    't(labels[name])',
    'function panel() { const path = `missingEditor.sidebar.${tab}`; i18n.global.t(path) }',
    'function other() { const path = `model.sidebar.${tab}`; t(path) }',
    'te("missingEditor.strategies." + strategy + ".label")',
    't(ok ? `missingEditor.sidebar.${tab}` : `model.sidebar.${tab}`)',
    'const key = "model.valid"; function unknown(key: string) { t(key) }',
  ].join('\n'), 'example.ts')
  const locales = { en: new Set(['model.valid', 'model.sidebar.basic']), zh: new Set(['model.valid', 'model.sidebar.basic']) }
  const failures = findTranslationReferenceFailures(calls, locales)
  assert.deepEqual(calls[0]!.literalKeys, ['model.valid', 'missingEditor.tab'])
  assert.equal(calls[1]!.prefix, 'missingEditor.sidebar.')
  assert.equal(calls[2]!.prefix, 'model.sidebar.')
  assert.deepEqual(calls[4]!.prefixes, ['missingEditor.sidebar.', 'model.sidebar.'])
  assert.deepEqual([...new Set(failures.map(({ key }) => key))], ['missingEditor.tab', 'missingEditor.sidebar', 'missingEditor.strategies'])
  assert.equal(failures.length, 8)
  assert.equal(calls[5]!.dynamic, true)
  assert.deepEqual(calls[5]!.literalKeys, [])
})

test('Vue template audit visits interpolation, bound props, event handlers and both scripts', () => {
  const source = [
    '<template>',
    '  <!-- {{ $t("removed.comment") }} -->',
    '  <input :placeholder="$t(\'common.name\')" @change="notify(t(\'common.changed\'))">',
    '  <p>{{ $t(`state.${status}`) }} {{ $t(ok ? \'common.yes\' : \'common.no\') }}</p>',
    '</template>',
    '<script>i18n.global.t("common.close")</script>',
    '<script setup lang="ts">tm("auth.loginSuccessContent")</script>',
  ].join('\n')
  const calls = collectTranslationCallsFromSource(source, 'example.vue')

  assert.deepEqual(calls.map((call) => call.literalKeys), [
    ['common.name'], ['common.changed'], [], ['common.yes', 'common.no'], ['common.close'], ['auth.loginSuccessContent'],
  ])
  assert.equal(calls[0].line, 3)
  assert.equal(calls[0].column, source.split('\n')[2].indexOf('$t') + 1)
  assert.equal(calls[2].prefix, 'state.')
  assert.equal(calls[5].kind, 'tree')
})
