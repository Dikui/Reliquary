import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parse as parseSFC } from '@vue/compiler-sfc'
import { parse, NodeTypes, type RootNode, type TemplateChildNode } from '@vue/compiler-dom'
import enUS from './locales/en-US'
import zhCN from './locales/zh-CN'
import { collectLocaleKeys, getLocaleValueAtPath } from './localeKeyAudit'
import { collectTranslationCallsFromSource } from './translationCallAudit'

const retiredCopy = /消息|对象|知识上下文|领域|领域模板|开放字段|字段归并|事实记录|证据版本|抽取模式|关注范围|事件聚合|事件热度|具体所指标签|内容性质标签|自由标签|关注价值|Agent 策略|策略版本|跟踪任务|知识工作流|知识报告|可编辑工作副本|不可变原文|知识工作台|message content|existing message|knowledge context|knowledge workbench/i
const pages = [
  '../views/knowledge/KnowledgeBase.vue',
  '../views/knowledge/KnowledgeSearch.vue',
  '../views/knowledge/components/KnowledgeWorkbench.vue',
  '../views/knowledge/TopicManagement.vue',
  '../views/knowledge/OperationLogs.vue',
  '../views/settings/Settings.vue',
  '../views/settings/KnowledgeSettings.vue',
  '../views/settings/TenantInfo.vue',
  '../views/knowledge/settings/DataSourceEditorDialog.vue',
  '../components/doc-content.vue',
  '../components/menu.vue',
  '../components/UserMenu.vue',
  '../views/auth/Login.vue',
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
  assert.equal(zhCN.menu.knowledgeBase, '文章')
  assert.equal(enUS.menu.knowledgeBase, 'Articles')
  assert.equal(zhCN.knowledgeEditor.document.title, '文章')
  assert.equal(enUS.knowledgeEditor.document.title, 'Articles')
  assert.equal(zhCN.menu.search, '知识检索')
  assert.equal(zhCN.knowledgeWorkbench.tabLabel, '实体')
  assert.equal(zhCN.settings.vault.title, '文件库状态')
  assert.equal(zhCN.settings.taskQueue, '运行队列')
})

test('login presents current localized copy without obsolete screenshot claims', () => {
  const source = readFileSync(new URL('../views/auth/Login.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(source, /screenshot-[12]\.svg|screenshot-swiper|swiper\/|<swiper|platform\.carousel/)
  for (const bundle of [zhCN, enUS]) assert.equal('carousel' in bundle.platform, false)
})
