import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { compileStyleAsync, parse } from '@vue/compiler-sfc'
import postcss from 'postcss'
import { compiledSFCStyleRules, styleDeclarationsAtWidth } from '../../utils/sfcStyleTestHelpers'

const files = ['Settings.vue', 'GeneralSettings.vue', 'ModelSettings.vue', '../system/RuntimeQueues.vue', '../system/SystemSettings.vue', 'VaultStatus.vue', 'OllamaSettings.vue', 'SystemInfo.vue', 'SpaceInfo.vue', 'UserProfile.vue', '../library/OperationLogs.vue', '../integrations/IntegrationSettingsSection.vue', '../../components/ModelTierEditor.vue']
const sheets: string[] = [readFileSync(new URL('../../styles/settings-form.css', import.meta.url), 'utf8')]
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
  const path = ['main.settings-page', 'div.settings-content', 'div.content-wrapper', 'div.general-settings.settings-form']
  assert.equal(computed([...path, 'div.settings-group', 'div.setting-row'], 'grid-template-columns'), '224px minmax(0, 1fr)')
  assert.equal(computed([...path, 'div.settings-group', 'div.setting-row', 'div.setting-control'], 'min-width'), '0')
  assert.equal(computed([...path, 'div.settings-group', 'div.setting-row', 'div.setting-control'], 'max-width'), 'none')
  assert.equal(computed([...path, 'div.section-header'], 'position'), 'static')
})
test('settings use tokens and do not retain TDesign style overrides', () => {
  const source = readFileSync(new URL('./settingsPage.less', import.meta.url), 'utf8')
  assert.doesNotMatch(source, /--td-|\.t-|:deep\(\*\)/)
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

test('group templates omit duplicate page titles instead of hiding them with CSS', () => {
  for (const file of files.filter(file => file !== 'Settings.vue' && file !== '../../components/ModelTierEditor.vue')) {
    const source = readFileSync(new URL(file, import.meta.url), 'utf8')
    const { descriptor } = parse(source)
    assert.doesNotMatch(descriptor.template?.content || '', /<h[12]>/, file)
  }
  assert.doesNotMatch(readFileSync(new URL('./settingsPage.less', import.meta.url), 'utf8'), /section-header h2|vault-status-heading h3|model-tier-heading h3|header h1/)
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
