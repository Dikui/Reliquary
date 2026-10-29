import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../../utils/sfcStyleTestHelpers'

// These checks inspect compiled production styles. Browser geometry and popup
// placement remain part of the separate browser review.
const scenarios = [
  { viewport: 1440, inner: 760, stacked: false },
  { viewport: 1280, inner: 760, stacked: false },
  { viewport: 1024, inner: 728, stacked: false },
  { viewport: 900, inner: 636, stacked: true },
  { viewport: 768, inner: 504, stacked: true },
]

test('settings page uses the designed navigation width and a scrollable content container', async () => {
  const rules = await compiledSFCStyleRules(new URL('./Settings.vue', import.meta.url))
  assert.equal(styleDeclarationsAtWidth(rules, '.settings-page', 1280)['grid-template-columns'], '208px minmax(0, 1fr)')
  assert.equal(styleDeclarationsAtWidth(rules, '.settings-page', 900)['grid-template-columns'], 'minmax(0, 1fr)')
  assert.equal(styleDeclarationsAtWidth(rules, '.content-wrapper', 1280)['container-name'], 'settings-content')
  assert.equal(styleDeclarationsAtWidth(rules, '.content-wrapper', 1280)['container-type'], 'inline-size')
  assert.equal(styleDeclarationsAtWidth(rules, '.settings-content', 1280)['min-width'], '0')
  assert.equal(styleDeclarationsAtWidth(rules, '.settings-content', 1280)['overflow-y'], 'auto')
})

test('shared setting rows stack at the content breakpoint and keep controls shrinkable', async () => {
  const rules = await compiledSFCStyleRules(new URL('../../styles/settings-form.css', import.meta.url))
  for (const { viewport, inner, stacked } of scenarios) {
    const style = (selector: string) => styleDeclarationsAtWidth(rules, selector, viewport, { 'settings-content': inner })
    assert.equal(style('.settings-form .setting-row')['grid-template-columns'], stacked ? 'minmax(0, 1fr)' : '224px minmax(0, 1fr)')
    assert.equal(style('.settings-form .setting-control')['min-width'], '0')
    assert.equal(style('.settings-form .setting-control')['max-width'], 'none')
    assert.equal(style('.settings-form .font-preview').width, '100%')
    assert.equal(style('.settings-form .info-value')['overflow-wrap'], 'anywhere')
  }
})

test('narrow model tier forms keep their natural height', async () => {
  const tiers = await compiledSFCStyleRules(new URL('../../components/ModelTierEditor.vue', import.meta.url))
  for (const { viewport, inner } of scenarios.filter(item => item.stacked)) {
    const container = { 'settings-content': inner }
    assert.equal(styleDeclarationsAtWidth(tiers, '.model-tier-row', viewport, container)['grid-template-columns'], 'minmax(0, 1fr)')
  }
  for (const rules of [tiers]) {
    assert.ok(!rules.some(rule => rule.containers.length && ['height', 'max-height', 'overflow-y'].some(property => property in rule.declarations)))
  }
  assert.equal(styleDeclarationsAtWidth(tiers, '.model-tier-row', 1440)['grid-template-columns'], '224px minmax(0, 1fr)')
})
