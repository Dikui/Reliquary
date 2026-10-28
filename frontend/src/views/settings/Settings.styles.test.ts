import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'
import postcss from 'postcss'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../../utils/sfcStyleTestHelpers'

const files = ['Settings.vue', 'GeneralSettings.vue', 'ModelSettings.vue', '../system/RuntimeQueues.vue', '../system/SystemSettings.vue', 'VaultStatus.vue', 'OllamaSettings.vue', 'SystemInfo.vue', 'SpaceInfo.vue', 'UserProfile.vue', '../library/OperationLogs.vue', '../integrations/IntegrationSettingsSection.vue', '../../components/ModelTierEditor.vue']
const sheets: string[] = []
for (const file of files) {
  const filename = new URL(file, import.meta.url).pathname
  const { descriptor } = parse(readFileSync(filename, 'utf8'), { filename })
  for (const style of descriptor.styles) {
    const result = await compileStyleAsync({ filename, source: style.content, id: 'data-v-settings-style', scoped: style.scoped, preprocessLang: style.lang as 'less' | undefined })
    assert.deepEqual(result.errors, [])
    sheets.push(result.code)
  }
}
// Child styles deliberately follow the page stylesheet, as in a lazy-loaded app.
const css = postcss.parse(sheets.join('\n'))
function computed(path: string[], property: string) {
  let best = -1, value = ''
  css.walkRules(rule => {
    if (rule.parent?.type === 'atrule') return
    for (const selector of rule.selectors) {
      if (/[:>+~]/.test(selector)) continue
      const parts = selector.replaceAll('[data-v-settings-style]', '').trim().split(/\s+/)
      let cursor = path.length - 1, matches = true
      for (let index = parts.length - 1; index >= 0; index--) {
        const part = parts[index]!
        const tag = part.match(/^[\w-]+/)?.[0]
        const classes = [...part.matchAll(/\.([\w-]+)/g)].map(match => match[1]!)
        const match = (node: string) => (!tag || node.split('.')[0] === tag) && classes.every(name => node.split('.').includes(name))
        if (part.includes('[') || index === parts.length - 1 && !match(path[cursor]!)) { matches = false; break }
        while (cursor >= 0 && !match(path[cursor]!)) cursor--
        if (cursor < 0) { matches = false; break }
        cursor--
      }
      if (!matches) continue
      const weight = (selector.match(/[.\[]/g)?.length || 0) * 100 + parts.filter(part => /^[a-z]/.test(part)).length
      rule.walkDecls(property, declaration => {
        const priority = weight + (declaration.important ? 10000 : 0)
        if (priority >= best) { best = priority; value = declaration.value }
      })
    }
  })
  return value
}
test('page form geometry wins over retained component styles in either load order', () => {
  const path = ['main.settings-page', 'div.settings-content', 'div.content-wrapper', 'div.general-settings']
  assert.equal(computed([...path, 'div.settings-group', 'div.setting-row'], 'grid-template-columns'), '224px minmax(0, 1fr)')
  assert.equal(computed([...path, 'div.settings-group', 'div.setting-row', 'div.setting-control'], 'min-width'), '0')
  assert.equal(computed([...path, 'div.settings-group', 'div.setting-row', 'div.setting-control'], 'max-width'), 'none')
  assert.equal(computed([...path, 'div.section-header'], 'position'), 'static')
})
test('legacy cards, system tabs and queue panels use square surfaces without shadows', () => {
  for (const children of [['div.model-settings', 'div.model-card'], ['div.model-settings', 'div.builtin-models-hint'], ['div.runtime-queues', 'div.rq-state'], ['div.system-settings', 'div.settings-section-tabs']]) {
    const path = ['main.settings-page', 'div.settings-content', 'div.content-wrapper', ...children]
    assert.equal(computed(path, 'border-radius'), '0')
    assert.equal(computed(path, 'box-shadow'), 'none')
  }
  assert.equal(computed(['div.general-settings', 'div.settings-group', 'div.setting-row', 'div.setting-control'], 'min-width'), '280px', 'legacy use outside settings is unchanged')
})
test('settings focus uses on-select text and styles never override hidden visibility', () => {
  css.walkRules(rule => {
    if (!rule.selector.includes('.settings-page')) return
    if (rule.nodes.some(node => node.type === 'decl' && node.prop === 'background' && node.value === 'var(--select)')) {
      assert.ok(rule.nodes.some(node => node.type === 'decl' && node.prop === 'color' && node.value === 'var(--on-select)'))
    }
    if (rule.selector.includes('.settings-page[data-v-settings-style] *')) assert.ok(!rule.nodes.some(node => node.type === 'decl' && node.prop === 'display'))
  })
  assert.doesNotMatch(sheets[0]!, /\d(?:\.\d+)?rem\b/)
})


test('every settings group shows one page heading while keeping useful section headings and actions', () => {
  const duplicates = [
    ['div.general-settings', 'div.section-header', 'h2'],
    ['div.library-catalog', 'section.catalog-section', 'section.model-tier-editor', 'div.model-tier-heading', 'div', 'h3'],
    ['section.vault-status', 'div.vault-status-heading', 'h3'],
    ['div.model-settings', 'div.section-header', 'div.section-header__top', 'div', 'h2'],
    ['div.ollama-settings', 'div.section-header', 'h2'],
    ['div.runtime-queues', 'header.section-header', 'div.rq-title-block', 'h2'],
    ['div.integrations-settings', 'div.section-header', 'h2'],
    ['div.system-settings', 'div.section-header', 'div.section-header__titlewrap', 'h2'],
    ['main.operation-log-page', 'header', 'h1'],
    ['div.system-info', 'div.section-header', 'h2'],
    ['div.space-info', 'div.section-header', 'h2'],
    ['div.user-profile', 'div.section-header', 'h2'],
  ]
  for (const children of duplicates) {
    assert.equal(computed(['main.settings-page', 'div.content-wrapper', ...children], 'display'), 'none', children.join(' '))
  }
  for (const children of [
    ['h1.settings-title'],
    ['div.library-catalog', 'section.catalog-section', 'header', 'h2'],
    ['div.library-settings', 'section.model-tier-editor', 'div.model-tier-heading', 'div', 'h3'],
    ['div.library-catalog', 'section.catalog-section', 'section.model-tier-editor', 'div.model-tier-heading', 'button'],
    ['main.operation-log-page', 'header', 'p'],
    ['div.runtime-queues', 'section', 'h3.rq-section-title'],
    ['section.vault-status', 'div.vault-status-heading', 'div.vault-status-actions', 'button'],
  ]) {
    assert.notEqual(computed(['main.settings-page', 'div.content-wrapper', ...children], 'display'), 'none', children.join(' '))
  }
})

test('settings groups have rule separators and use the main sidebar selection colors at all widths', async () => {
  const rules = await compiledSFCStyleRules(new URL('./Settings.vue', import.meta.url))
  for (const width of [1440, 390]) {
    const group = styleDeclarationsAtWidth(rules, '.settings-nav-group', width)
    assert.equal(group['border-top'], '2px solid var(--rule)')
    assert.notEqual(group.display, 'contents', 'group borders need a box on narrow screens')
    const selected = styleDeclarationsAtWidth(rules, ".settings-sidebar a[aria-current='page']", width)
    assert.equal(selected.background, 'var(--text)')
    assert.equal(selected.color, 'var(--bg)')
  }
})
