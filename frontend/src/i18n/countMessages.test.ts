import { formatCountCopy } from './countCopy'
import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import enUS from './locales/en-US'
import zhCN from './locales/zh-CN'

const cases = [["extractionDetails.segments","total","Processed {processed} / {total} segment","Processed {processed} / {total} segments"],
  ["knowledgeStrategy.evaluation.countedSamples","count","{count} sample counted in metrics","{count} samples counted in metrics"],
  ["knowledgeStrategy.evaluation.allSamples","count","The {count} sample","All {count} samples"],
  ["knowledgeStrategy.evaluation.mistakes","count","{count} example with a wrong decision","{count} examples with a wrong decision"],
  ["knowledgeWorkbench.sourceTime.excluded","count","Source excluded because publication time is missing: {count}","Sources excluded because publication time is missing: {count}"],
  ["knowledgeWorkbench.nameEmbeddings.pending","count","Object name pending rebuild: {count}","Object names pending rebuild: {count}"],
  ["knowledgeWorkbench.nameEmbeddings.latest","total","Latest rebuild: {status}, {processed}/{total} object processed","Latest rebuild: {status}, {processed}/{total} objects processed"],
  ["knowledgeWorkbench.rebuildQueued","count","{count} source was queued for rebuilding. Results will update after background processing finishes.","{count} sources were queued for rebuilding. Results will update after background processing finishes."],
  ["knowledgeWorkbench.events.sourceCount","count","{count} source","{count} sources"],
  ["batchManage.deleteConfirmBody","count","Are you sure you want to delete the selected {count} conversation? This action cannot be undone.","Are you sure you want to delete the selected {count} conversations? This action cannot be undone."],
  ["knowledgeBase.folderTree.folderCardCount","count","{count} document","{count} documents"],
  ["knowledgeBase.moveToFolder.success","count","Moved {count} document","Moved {count} documents"],
  ["knowledgeBase.tagFilterMulti","count","{count} tag","{count} tags"],
  ["knowledgeBase.tagManageDocCount","count","{count} document","{count} documents"],
  ["knowledgeBase.batchDeleteSuccess","count","Deleted {count} document","Deleted {count} documents"],
  ["knowledgeBase.batchReparseSuccess","count","Submitted {count} rebuild task","Submitted {count} rebuild tasks"],
  ["uploadConfirm.summaryTagsCount","count","{count} tag","{count} tags"],
  ["uploadConfirm.filesAdded","count","Added {count} file","Added {count} files"],
  ["uploadConfirm.manualCharCount","count","{count} character","{count} characters"],
  ["agent.capabilities.kbCount","count","{count} knowledge base specified","{count} knowledge bases specified"],
  ["mentionDetail.kbCount","count","{count} document","{count} documents"],
  ["chat.referenceChunkCount","count","{count} chunk","{count} chunks"],
  ["chat.contextResultsHeader","count","{count} related result found","{count} related results found"],
  ["chat.chunkCountValue","count","{count} chunk","{count} chunks"],
  ["chat.knowledgeBaseCount","count","{count} knowledge base","{count} knowledge bases"],
  ["model.editor.contextWindowTokens","count","{count} token","{count} tokens"],
  ["agentStream.grepResults.chunkHits","count","{count} chunk","{count} chunks"],
  ["agentStream.grepResults.keywordHits","count","{count} hit","{count} hits"],
  ["knowledgeBase.uploadAllSuccess","count","Successfully uploaded {count} file!","Successfully uploaded {count} files!"],
  ["knowledgeBase.uploadingFolder","total","Uploading {total} file from folder...","Uploading {total} files from folder..."],
  ["knowledgeBase.videosFilteredNoVLM","count","Skipped {count} video file (video upload is not supported)","Skipped {count} video files (video upload is not supported)"],
  ["knowledgeBase.chunkCount","count","Total {count} segment","Total {count} segments"],
  ["knowledgeBase.confirmBatchDeleteDocument","count","Delete {count} selected document? This action cannot be undone.","Delete {count} selected documents? This action cannot be undone."],
  ["knowledgeBase.batchTagSubtitle","count","Set tags for {count} selected document (will replace existing tags)","Set tags for {count} selected documents (will replace existing tags)"],
  ["knowledgeBase.batchTagSuccess","count","Tags applied to {count} document","Tags applied to {count} documents"],
  ["knowledgeBase.confirmBatchReparseDocument","count","Rebuild {count} selected document? Existing content will be cleared and the document will be re-parsed.","Rebuild {count} selected documents? Existing content will be cleared and each document will be re-parsed."],
  ["knowledgeBase.batchReparseSkippedInFlight","count","Skipped {count} document still being parsed","Skipped {count} documents still being parsed"],
  ["knowledgeBase.filesSkippedNoEngine","count","{count} file skipped due to no available parser engine","{count} files skipped due to no available parser engine"],
  ["knowledgeBase.allUploadSuccess","count","The file uploaded successfully ({count} file)","All files uploaded successfully ({count} files)"],
  ["knowledgeBase.allUploadFailed","count","The file failed to upload ({count} file)","All files failed to upload ({count} files)"],
  ["uploadConfirm.folderUploadHint","count","{count} file; the local folder structure will be kept","{count} files; the local folder structure will be kept"],
  ["knowledgeStages.head.completedWithActiveTrace","n","Processing completed, but {n} trace task remains active","Processing completed, but {n} trace tasks remain active"],
  ["agent.stepsCompleted","steps","Completed <strong>{steps}</strong> step","Completed <strong>{steps}</strong> steps"],
  ["agent.reasoningRounds","rounds","<strong>{rounds}</strong> reasoning round","<strong>{rounds}</strong> reasoning rounds"],
  ["agent.toolCalls","tools","<strong>{tools}</strong> tool call","<strong>{tools}</strong> tool calls"],
  ["input.knowledgeBaseWithCount","count","Knowledge Base ({count})","Knowledge Bases ({count})"],
  ["knowledgeList.uploadProgress.detail","total","{completed}/{total} file finished","{completed}/{total} files finished"],
  ["knowledgeList.uploadProgress.completedDetail","total","The {total} file uploaded. Refreshing list to show parsing status...","All {total} files uploaded. Refreshing list to show parsing status..."],
  ["knowledgeEditor.indexing.rebuildConfirmBody","count","Indexing strategy has changed. Re-process {count} existing document? This may take some time.","Indexing strategy has changed. Re-process {count} existing documents? This may take some time."],
  ["knowledgeEditor.indexing.rebuildSuccess","count","Rebuild task submitted for {count} document","Rebuild task submitted for {count} documents"],
  ["chat.referencesTitle","count","Referenced {count} related item","Referenced {count} related items"],
  ["chat.referencesDocCount","count","Referenced {count} document","Referenced {count} documents"],
  ["chat.referencesWebCount","count","Referenced {count} web source","Referenced {count} web sources"],
  ["chat.webGroupIntro","count","The following {count} item is from","The following {count} items are from"],
  ["chat.lengthChars","value","{value} character","{value} characters"],
  ["chat.attachmentWithCount","count","{count} attachment uploaded","{count} attachments uploaded"],
  ["chat.attachmentTooMany","max","Maximum {max} attachment allowed","Maximum {max} attachments allowed"],
  ["system.globalSettings.runtime.poolInstances","value","{value} instance","{value} instances"],
  ["system.globalSettings.runtime.queueCount","value","{value} queue","{value} queues"],
  ["system.globalSettings.runtime.failedNotice.title","count","{count} failed task needs attention","{count} failed tasks need attention"],
  ["system.globalSettings.runtime.tasks.openAria","count","View {count} {state} task in {queue}","View {count} {state} tasks in {queue}"],
  ["system.globalSettings.runtime.tasks.purgeArchivedSuccess","count","Cleared {count} failed task","Cleared {count} failed tasks"],
  ["modelSettings.usage.knowledgeBases","count","Knowledge base ({count})","Knowledge bases ({count})"],
  ["modelSettings.usage.agents","count","Agent ({count})","Agents ({count})"],
  ["agentStream.search.candidatesBelowThreshold","count","Matched {count} candidate, none relevant enough to use","Matched {count} candidates, none relevant enough to use"],
  ["agentStream.search.foundResults","count","Found {count} result","Found {count} results"],
  ["agentStream.knowledgeChunksList.chunkRange","total","Loaded {fetched} / {total} chunk","Loaded {fetched} / {total} chunks"],
  ["agentStream.attachmentParsing.parsedSummary","count","Parsed {count} attachment","Parsed {count} attachments"],
  ["agentStream.attachmentParsing.parsedWithSkipped","parsed","Parsed {parsed} attachment, {skipped} skipped (still processing)","Parsed {parsed} attachments, {skipped} skipped (still processing)"],
  ["agentEditor.tools.statusInactive","count","{count} ticked tool cannot take effect with the current config","{count} ticked tools cannot take effect with the current config"],
  ["agentEditor.agentType.kbIncompatibleWarn","count","{count} selected KB is not compatible with this type, please adjust manually.","{count} selected KBs are not compatible with this type, please adjust manually."],
  ["ollama.daysAgo","days","{days} day ago","{days} days ago"],
  ["datasource.logDetail.failedItemsMore","n","{n} more failed document not shown","{n} more failed documents not shown"],
  ["datasource.logDetail.docsFailedSummary","n","{n} document failed to sync","{n} documents failed to sync"],
  ["knowledgeBase.characters","$plural","character","characters"],
  ["agentEditor.tools.kbMetricRag","$plural","RAG KB","RAG KBs"],
  ["knowledgeEditor.chunking.characters", "$plural", "character", "characters"],["knowledgeEditor.chunking.debug.stats.chunks", "$plural", "chunk", "chunks"]] as const
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

const composedCases: Array<[string, Record<string, number>, Record<string, number>, string, string]> = [["knowledgeWorkbench.query.resultCounts",{"entities":1,"facts":1,"events":1,"evidence":1},{"entities":1,"facts":1,"events":1,"evidence":1,"processed":1,"updated":2,"failed":0},"Found 1 entity, 1 fact, 1 event, and 1 evidence item.","找到 1 个对象、1 条事实、1 个事件和 1 条证据。"],
  ["knowledgeWorkbench.query.resultCounts",{"entities":2,"facts":2,"events":2,"evidence":2},{"entities":2,"facts":2,"events":2,"evidence":2,"processed":1,"updated":2,"failed":0},"Found 2 entities, 2 facts, 2 events, and 2 evidence items.","找到 2 个对象、2 条事实、2 个事件和 2 条证据。"],
  ["knowledgeWorkbench.query.resultCounts",{"entities":1,"facts":2,"events":1,"evidence":2},{"entities":1,"facts":2,"events":1,"evidence":2,"processed":1,"updated":2,"failed":0},"Found 1 entity, 2 facts, 1 event, and 2 evidence items.","找到 1 个对象、2 条事实、1 个事件和 2 条证据。"],
  ["chat.referencesDocAndWebCount",{"docCount":1,"webCount":1},{"docCount":1,"webCount":1,"processed":1,"updated":2,"failed":0},"Referenced 1 document and 1 web source","引用了1篇文档和1条网页"],
  ["chat.referencesDocAndWebCount",{"docCount":2,"webCount":2},{"docCount":2,"webCount":2,"processed":1,"updated":2,"failed":0},"Referenced 2 documents and 2 web sources","引用了2篇文档和2条网页"],
  ["chat.referencesDocAndWebCount",{"docCount":1,"webCount":2},{"docCount":1,"webCount":2,"processed":1,"updated":2,"failed":0},"Referenced 1 document and 2 web sources","引用了1篇文档和2条网页"],
  ["agentStream.search.foundResultsFromFiles",{"count":1,"files":1},{"count":1,"files":1,"processed":1,"updated":2,"failed":0},"Found 1 result from 1 file","找到 1 个结果，来自 1 个文件"],
  ["agentStream.search.foundResultsFromFiles",{"count":2,"files":2},{"count":2,"files":2,"processed":1,"updated":2,"failed":0},"Found 2 results from 2 files","找到 2 个结果，来自 2 个文件"],
  ["agentStream.search.foundResultsFromFiles",{"count":1,"files":2},{"count":1,"files":2,"processed":1,"updated":2,"failed":0},"Found 1 result from 2 files","找到 1 个结果，来自 2 个文件"],
  ["agentStream.search.grepSummary",{"chunks":1,"docs":1},{"chunks":1,"docs":1,"processed":1,"updated":2,"failed":0},"Found 1 matching chunk across 1 document","找到 1 个匹配片段，来自 1 个文档"],
  ["agentStream.search.grepSummary",{"chunks":2,"docs":2},{"chunks":2,"docs":2,"processed":1,"updated":2,"failed":0},"Found 2 matching chunks across 2 documents","找到 2 个匹配片段，来自 2 个文档"],
  ["agentStream.search.grepSummary",{"chunks":1,"docs":2},{"chunks":1,"docs":2,"processed":1,"updated":2,"failed":0},"Found 1 matching chunk across 2 documents","找到 1 个匹配片段，来自 2 个文档"],
  ["agent.contextCompactedSummary",{"before":1,"after":1},{"before":1,"after":1,"processed":1,"updated":2,"failed":0},"1 token → 1 token","1 → 1 tokens"],
  ["agent.contextCompactedSummary",{"before":2,"after":2},{"before":2,"after":2,"processed":1,"updated":2,"failed":0},"2 tokens → 2 tokens","2 → 2 tokens"],
  ["agent.contextCompactedSummary",{"before":1,"after":2},{"before":1,"after":2,"processed":1,"updated":2,"failed":0},"1 token → 2 tokens","1 → 2 tokens"],
  ["knowledgeBase.strategy.reprocessResult",{"flagged":1,"conflicts":1},{"flagged":1,"conflicts":1,"processed":1,"updated":2,"failed":0},"Processed 1; updated 2; 1 review flag; 1 manual conflict; failed 0","处理 1 条，更新 2 条，审核疑点 1 条，人工冲突 1 条，失败 0 条"],
  ["knowledgeBase.strategy.reprocessResult",{"flagged":2,"conflicts":2},{"flagged":2,"conflicts":2,"processed":1,"updated":2,"failed":0},"Processed 1; updated 2; 2 review flags; 2 manual conflicts; failed 0","处理 1 条，更新 2 条，审核疑点 2 条，人工冲突 2 条，失败 0 条"],
  ["knowledgeBase.strategy.reprocessResult",{"flagged":1,"conflicts":2},{"flagged":1,"conflicts":2,"processed":1,"updated":2,"failed":0},"Processed 1; updated 2; 1 review flag; 2 manual conflicts; failed 0","处理 1 条，更新 2 条，审核疑点 1 条，人工冲突 2 条，失败 0 条"],
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
