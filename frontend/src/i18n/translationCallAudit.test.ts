import assert from 'node:assert/strict'
import test from 'node:test'

import { collectTranslationCallsFromSource } from './translationCallAudit.ts'

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
