import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../utils/sfcStyleTestHelpers'

const component = new URL('./LibraryInfoPopover.vue', import.meta.url)

test('English info labels use wider columns and word wrapping in compiled scoped styles', async () => {
  const source = readFileSync(component, 'utf8')
  assert.match(source, /class="library-info-card" :lang="locale"/)
  assert.match(source, /const \{ t, locale \} = useI18n\(\)/)
  const rules = await compiledSFCStyleRules(component)
  const base = styleDeclarationsAtWidth(rules, '.library-info-card-label', 1440)
  const english = {
    ...base,
    ...styleDeclarationsAtWidth(rules, '.library-info-card:lang(en) .library-info-card-label', 1440),
  }
  assert.equal(base.flex, '0 0 80px', 'Chinese retains its existing column width')
  assert.equal(english['flex-basis'], '112px')
  assert.equal(english['word-break'], 'normal')
  assert.equal(english['overflow-wrap'], 'break-word')
  assert.equal(english['white-space'], 'normal')
  assert.equal(english.overflow, 'visible')
  assert.equal(english['max-height'], undefined)
  assert.equal(english['text-overflow'], undefined)
  assert.equal(styleDeclarationsAtWidth(rules, '.library-info-card:lang(en) .library-info-card-value', 1440)['min-width'], '0')
  assert.equal(rules.some(rule => rule.selector.includes(':lang(zh)')), false)
})
