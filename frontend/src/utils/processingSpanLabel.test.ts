import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createI18n } from 'vue-i18n'
import en from '../i18n/locales/en-US'
import zh from '../i18n/locales/zh-CN'
import { processingSpanLabel } from './processingSpanLabel'

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: segment, review and merge names and every segmented error have translations`, () => {
    const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
    const t = (key: string, values?: Record<string, string>) => i18n.global.t(key, values ?? {})
    assert.equal(processingSpanLabel('intake.review', t), locale === 'zh-CN' ? '收录审核' : 'Intake review')
    assert.equal(processingSpanLabel('processing.segment.3/12', t), locale === 'zh-CN' ? '第 3/12 段' : 'Segment 3/12')
    assert.equal(processingSpanLabel('review.segment.3/12', t), processingSpanLabel('processing.segment.3/12', t))
    assert.equal(processingSpanLabel('processing.summary_merge', t), t('processingStages.summaryMerge'))
    const errors = new Set<string>()
    for (const file of ['entry_segments.go', 'entry_article_segments.go', 'entry_intake.go']) {
      const text = readFileSync(new URL(`../../../internal/application/service/${file}`, import.meta.url), 'utf8')
      for (const match of text.matchAll(/FailSpan\(ctx, span, "([A-Z_]+)"/g)) errors.add(match[1])
    }
    assert.ok(errors.has('SEGMENT_FAILED'))
    for (const code of errors) for (const suffix of ['', '_SUGGESTION']) {
      const key = `processingStages.errorCode.${code}${suffix}`
      assert.ok(i18n.global.te(key), key)
      assert.notEqual(t(key), key)
    }
  })
}
