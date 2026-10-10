import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../../utils/sfcStyleTestHelpers'

test('domain names wrap fully within the existing desktop table column', async () => {
  const file = new URL('./DomainSettings.vue', import.meta.url)
  const source = readFileSync(file, 'utf8')
  const rules = await compiledSFCStyleRules(file)
  assert.match(source, /colKey: 'name', title: t\('knowledgeDomains\.name'\), width: 180/)
  assert.match(source, /<t-button class="domain-name-link"[^>]*>\{\{ row\.name \}\}<\/t-button>/)
  for (const [viewport, inner] of [[1440, 760], [1280, 760], [1024, 728], [900, 636], [768, 504]]) {
    const styles = (selector: string) => styleDeclarationsAtWidth(rules, selector, viewport, { 'settings-content': inner })
    const button = styles('.domain-name-link')
    assert.equal(button['min-width'], '0')
    assert.equal(button['max-width'], '100%')
    assert.equal(button.height, 'auto')
    assert.equal(button['min-height'], 'var(--td-comp-size-m)')
    assert.equal(button['white-space'], 'normal')
    const label = styles('.domain-name-link .t-button__text')
    assert.equal(label.display, 'block')
    assert.equal(label['min-width'], '0')
    assert.equal(label['white-space'], 'normal')
    assert.equal(label['overflow-wrap'], 'anywhere')
    assert.equal(label['text-align'], 'left')
    assert.equal(styles('.domain-name .t-tag')['flex-shrink'], '0')
  }
})
