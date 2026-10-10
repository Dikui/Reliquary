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

test('settings shell names the usable content container and preserves wide desktop geometry', async () => {
  const rules = await compiledSFCStyleRules(new URL('./Settings.vue', import.meta.url))
  for (const { viewport } of scenarios) {
    const wrapper = styleDeclarationsAtWidth(rules, '.content-wrapper', viewport)
    assert.equal(wrapper['container-name'], 'settings-content')
    assert.equal(wrapper['container-type'], 'inline-size')
    assert.equal(wrapper.padding, '40px 48px')
    assert.equal(wrapper['padding-left'], viewport <= 1100 ? '24px' : undefined)
    assert.equal(wrapper['padding-right'], viewport <= 1100 ? '24px' : undefined)
    assert.equal(styleDeclarationsAtWidth(rules, '.settings-sidebar', viewport).width, viewport <= 900 ? '176px' : '208px')
    assert.equal(styleDeclarationsAtWidth(rules, '.settings-content', viewport)['min-width'], '0')
  }
  assert.equal(styleDeclarationsAtWidth(rules, '.settings-sidebar', 390).display, 'none')
  assert.equal(styleDeclarationsAtWidth(rules, '.content-wrapper', 390).padding, '20px 16px')
})

for (const [file, wideMinimum] of [
  ['GeneralSettings.vue', '280px'], ['UserProfile.vue', '280px'],
  ['OllamaSettings.vue', '360px'],
  ['SystemInfo.vue', '280px'], ['TenantInfo.vue', '0'],
] as const) {
  test(`${file}: narrow content stacks labels and allows controls to shrink`, async () => {
    const rules = await compiledSFCStyleRules(new URL(`./${file}`, import.meta.url))
    for (const { viewport, inner, stacked } of scenarios) {
      const style = (selector: string) => styleDeclarationsAtWidth(rules, selector, viewport, { 'settings-content': inner })
      assert.equal(style('.setting-row')['flex-direction'], stacked ? 'column' : undefined)
      assert.equal(style('.setting-control')['min-width'], stacked ? '0' : wideMinimum)
      if (!stacked) continue
      for (const selector of ['.setting-info', '.setting-control']) {
        assert.equal(style(selector).width, '100%', `${file} ${viewport} ${selector}`)
        assert.equal(style(selector)['max-width'], '100%')
      }
      assert.equal(style('.setting-info')['padding-right'], '0')
      assert.equal(style('.setting-control')['justify-content'], 'flex-start')
      assert.equal(style('.setting-control')['flex-wrap'], 'wrap')
    }
    // A real narrow container reflows even inside a wide window. An uncontained
    // sibling keeps its original layout and does not inherit settings rules.
    assert.equal(styleDeclarationsAtWidth(rules, '.setting-row', 1440, { 'settings-content': 720 })['flex-direction'], 'column')
    assert.equal(styleDeclarationsAtWidth(rules, '.setting-row', 1440, { 'settings-content': 721 })['flex-direction'], undefined)
    assert.equal(styleDeclarationsAtWidth(rules, '.setting-row', 1440)['flex-direction'], undefined)
  })
}

test('narrow font choices, information values and password popup preserve readable widths', async () => {
  const general = await compiledSFCStyleRules(new URL('./GeneralSettings.vue', import.meta.url))
  for (const selector of ['.setting-select', '.font-preview']) {
    assert.equal(styleDeclarationsAtWidth(general, selector, 900, { 'settings-content': 636 }).width, '100%')
    assert.equal(styleDeclarationsAtWidth(general, selector, 1440, { 'settings-content': 760 }).width, '280px')
  }
  for (const file of ['SystemInfo.vue', 'UserProfile.vue', 'TenantInfo.vue']) {
    const rules = await compiledSFCStyleRules(new URL(`./${file}`, import.meta.url))
    assert.equal(styleDeclarationsAtWidth(rules, '.setting-control .info-value', 900, { 'settings-content': 636 })['text-align'], 'left')
  }
  const profile = await compiledSFCStyleRules(new URL('./UserProfile.vue', import.meta.url))
  const popup = '.user-profile-password-popup-overlay .t-popup__content'
  assert.equal(styleDeclarationsAtWidth(profile, popup, 1440)['min-width'], '300px')
  for (const { viewport } of scenarios.filter(item => item.viewport <= 1100)) {
    const style = styleDeclarationsAtWidth(profile, popup, viewport)
    assert.equal(style['min-width'], '0')
    assert.equal(style.width, 'min(360px, calc(100vw - 72px))')
    assert.equal(style['box-sizing'], 'border-box')
    assert.equal(styleDeclarationsAtWidth(profile, '.password-popup-footer', viewport)['flex-wrap'], 'wrap')
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
  assert.equal(styleDeclarationsAtWidth(tiers, '.model-tier-row', 1440)['grid-template-columns'], 'minmax(0, 2fr) minmax(0, 3fr)')
})

test('model and parser grids can shrink below their preferred card width', async () => {
  for (const [file, selector] of [
    ['ModelSettings.vue', '.model-grid'], ['ParserEngineSettings.vue', '.engine-cards'],
  ]) {
    const rules = await compiledSFCStyleRules(new URL(`./${file}`, import.meta.url))
    assert.equal(styleDeclarationsAtWidth(rules, selector, 1440)['grid-template-columns'], 'repeat(auto-fill, minmax(min(320px, 100%), 1fr))')
  }
  const ollama = await compiledSFCStyleRules(new URL('./OllamaSettings.vue', import.meta.url))
  const models = await compiledSFCStyleRules(new URL('./ModelSettings.vue', import.meta.url))
  for (const { viewport, inner } of scenarios.filter(item => item.stacked)) {
    const container = { 'settings-content': inner }
    assert.equal(styleDeclarationsAtWidth(ollama, '.model-list-container', viewport, container)['grid-template-columns'], 'minmax(0, 1fr)')
    assert.equal(styleDeclarationsAtWidth(models, '.section-header__top', viewport, container)['flex-wrap'], 'wrap')
    assert.equal(styleDeclarationsAtWidth(models, '.model-type-tabs .t-tabs__nav-scroll', viewport, container)['scrollbar-width'], 'auto')
  }
})
