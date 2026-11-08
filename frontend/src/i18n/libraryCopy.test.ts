import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse as parseSFC } from '@vue/compiler-sfc'
import { parse, NodeTypes, type RootNode, type TemplateChildNode } from '@vue/compiler-dom'
import enUS from './locales/en-US'
import zhCN from './locales/zh-CN'
import { collectLocaleKeys, getLocaleValueAtPath } from './localeKeyAudit'
import { collectTranslationCallsFromSource } from './translationCallAudit'

// 认证对象 is the shelf certification field name (CONTEXT.md), not the retired generic term.
const retiredCopy = /消息|(?<!认证)对象|知识上下文|领域|领域模板|开放字段|字段归并|事实记录|证据版本|抽取模式|关注范围|事件聚合|事件热度|具体所指标签|内容性质标签|自由标签|关注价值|Agent 策略|策略版本|跟踪任务|知识工作流|知识报告|可编辑工作副本|不可变原文|知识工作台|message content|existing message|knowledge context|knowledge workbench/i
const pages = [
  '../views/library/Library.vue',
  '../views/library/SearchView.vue',
  '../views/library/components/EntryWorkbench.vue',
  '../views/library/TopicManagement.vue',
  '../views/library/OperationLogs.vue',
  '../views/settings/Settings.vue',
  '../views/settings/LibrarySettings.vue',
  '../views/settings/SpaceInfo.vue',
  '../components/DocContent.vue',
  '../components/SideMenu.vue',
]

for (const [locale, bundle] of Object.entries({ 'zh-CN': zhCN, 'en-US': enUS })) {
  const keys = collectLocaleKeys(bundle)
  test(`${locale}: displayed knowledge copy uses current article and entity terminology`, () => {
    const failures = [...keys].filter(key => retiredCopy.test(String(getLocaleValueAtPath(bundle, key))))
    assert.deepEqual(failures, [])
  })
  for (const page of pages) {
    test(`${locale}: ${page} has no retired interface wording`, () => {
      const source = readFileSync(new URL(page, import.meta.url), 'utf8')
      const calls = collectTranslationCallsFromSource(source, page)
      const used = new Set(calls.flatMap(call => [
        ...call.literalKeys,
        ...(call.prefix ? [...keys].filter(key => key.startsWith(call.prefix!)) : []),
      ]))
      assert.ok(used.size > 0, 'The check covers this page’s translated copy')
      for (const key of used) {
        const value = getLocaleValueAtPath(bundle, key)
        if (typeof value === 'string') assert.doesNotMatch(value, retiredCopy, key)
      }
      const { descriptor, errors } = parseSFC(source, { filename: page })
      assert.deepEqual(errors, [])
      function check(node: RootNode | TemplateChildNode) {
        if (node.type === NodeTypes.TEXT) assert.doesNotMatch(node.content, retiredCopy)
        if (node.type === NodeTypes.ROOT || node.type === NodeTypes.ELEMENT) node.children.forEach(check)
      }
      check(parse(descriptor.template!.content))
    })
  }
}

test('navigation and article labels use the same current names in both locales', () => {
  assert.equal(zhCN.menu.library, '库')
  assert.equal(enUS.menu.library, 'Libraries')
  assert.equal(zhCN.libraryEditor.document.title, '资料')
  assert.equal(enUS.libraryEditor.document.title, 'Materials')
  assert.equal(zhCN.menu.searchMaterials, '检索')
  assert.equal(zhCN.menu.entities, '实体')
  assert.equal(zhCN.settings.vault.title, '资料导出与备份')
  assert.equal(zhCN.settingsPage.queues, '任务队列')
})

test('library settings labels and model source labels describe their separate roles in both locales', () => {
  for (const [bundle, label] of [[enUS, 'Library settings'], [zhCN, '资料库设置']] as const) {
    assert.equal(bundle.settings.librarySettings, label)
    assert.equal(bundle.modelSettings.usage.librarySettings, label)
  }
})

test('removed screenshot carousel copy stays absent', () => {
  for (const bundle of [zhCN, enUS]) assert.equal(getLocaleValueAtPath(bundle, 'platform.carousel') !== undefined, false)
})
