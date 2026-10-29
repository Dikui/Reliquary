import { formatCountCopy } from './countCopy'
import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import enUS from './locales/en-US'
import zhCN from './locales/zh-CN'

const cases = [
  ["knowledgeWorkbench.nameEmbeddings.pending","count","Entity name pending rebuild: {count}","Entity names pending rebuild: {count}"],
  ["knowledgeWorkbench.nameEmbeddings.latest","total","Latest rebuild: {status}, {processed}/{total} entity processed","Latest rebuild: {status}, {processed}/{total} entities processed"],
  ["model.editor.contextWindowTokens","count","{count} token","{count} tokens"],
  ["system.globalSettings.runtime.poolInstances","value","{value} instance","{value} instances"],
  ["system.globalSettings.runtime.queueCount","value","{value} queue","{value} queues"],
  ["system.globalSettings.runtime.failedNotice.title","count","{count} failed task needs attention","{count} failed tasks need attention"],
  ["system.globalSettings.runtime.tasks.openAria","count","View {count} {state} task in {queue}","View {count} {state} tasks in {queue}"],
  ["system.globalSettings.runtime.tasks.purgeArchivedSuccess","count","Cleared {count} failed task","Cleared {count} failed tasks"],
  ["ollama.daysAgo","days","{days} day ago","{days} days ago"],
  ["knowledgeEditor.chunking.characters", "$plural", "character", "characters"]] as const
const i18n = createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': enUS, 'zh-CN': zhCN }, warnHtmlMessage: false })
for (const [key, selector, singular, plural] of cases) {
  for (const count of [1, 2]) {
    test(`${key}: English quantity ${count}`, () => {
      const named: Record<string, string | number> = { count, processed: count, completed: count, fetched: count, total: count, steps: count, rounds: count, tools: count, value: count, max: count, n: count, days: count, parsed: count, skipped: count, status: 'Completed', state: 'pending', queue: 'default' }
      const expected = (count === 1 ? singular : plural).replace(/\{(\w+)\}/g, (_, key: string) => String(named[key]))
      const actual = selector === '$plural' ? i18n.global.t(key, count) : i18n.global.t(key, named, count)
      assert.equal(actual, expected)
      if (selector === 'count') assert.equal(i18n.global.t(key, named), expected, 'named numeric count selects the same plural form')
    })
  }
}

const composedCases: Array<[string, Record<string, number>, Record<string, number>, string, string]> = [
  ["system.globalSettings.runtime.tasks.purgeArchivedConfirm",{"count":1},{"count":1,"processed":1,"updated":2,"failed":0},"Remove 1 failed record from this queue at once. This only clears failed tasks in the queue \u2014 it does not affect running or pending tasks, nor does it roll back the business status of documents that already failed. Clear them?","将从当前队列一次性移除全部 1 条失败记录，仅清理队列里的失败任务，不影响运行中或排队中的任务，也不会回滚已失败文档的业务状态。确认清除？"],
  ["system.globalSettings.runtime.tasks.purgeArchivedConfirm",{"count":2},{"count":2,"processed":1,"updated":2,"failed":0},"Remove 2 failed records from this queue at once. This only clears failed tasks in the queue \u2014 it does not affect running or pending tasks, nor does it roll back the business status of documents that already failed. Clear them?","将从当前队列一次性移除全部 2 条失败记录，仅清理队列里的失败任务，不影响运行中或排队中的任务，也不会回滚已失败文档的业务状态。确认清除？"],
  ["system.globalSettings.runtime.tasks.purgeArchivedConfirm",{"count":1},{"count":1,"processed":1,"updated":2,"failed":0},"Remove 1 failed record from this queue at once. This only clears failed tasks in the queue \u2014 it does not affect running or pending tasks, nor does it roll back the business status of documents that already failed. Clear them?","将从当前队列一次性移除全部 1 条失败记录，仅清理队列里的失败任务，不影响运行中或排队中的任务，也不会回滚已失败文档的业务状态。确认清除？"]]
for (const [key, counts, named, english, chinese] of composedCases) {
  test(`${key}: independent quantities ${JSON.stringify(counts)}`, () => {
    assert.equal(formatCountCopy(i18n.global.t, key, counts, named), english)
    const zh = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zhCN }, warnHtmlMessage: false })
    assert.equal(formatCountCopy(zh.global.t, key, counts, named), chinese, 'existing Chinese copy is unchanged')
  })
}
