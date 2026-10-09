import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import enUS from './locales/en-US'
import zhCN from './locales/zh-CN'
import { formatCountCopy } from './countCopy'
import { getKnowledgeSearchSummaryHtml } from '../utils/agent-tool-display'
import { getAttachmentParsingSummaryHtml } from '../utils/attachmentParsingDisplay'
import { getKnowledgeChunksSummaryHtml } from '../utils/knowledgeChunksDisplay'

const i18n = createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': enUS, 'zh-CN': zhCN }, warnHtmlMessage: false })
const t = i18n.global.t
const marked = (count: number) => `<strong>${count}</strong>`
for (const count of [1, 2]) {
  test(`search summary and relevance candidates select raw quantity ${count}`, () => {
    assert.equal(getKnowledgeSearchSummaryHtml(t, { count }), `Found ${marked(count)} ${count === 1 ? 'result' : 'results'}`)
    assert.equal(getKnowledgeSearchSummaryHtml(t, { count: 0, candidate_count: count }), `Matched ${marked(count)} ${count === 1 ? 'candidate' : 'candidates'}, none relevant enough to use`)
  })
  for (const files of [1, 2]) {
    test(`search quantities remain independent with marked numbers ${count}/${files}`, () => {
      const kb_counts = Object.fromEntries(Array.from({ length: files }, (_, index) => [index, 1]))
      assert.equal(getKnowledgeSearchSummaryHtml(t, { count, kb_counts }), `Found ${marked(count)} ${count === 1 ? 'result' : 'results'} from ${marked(files)} ${files === 1 ? 'file' : 'files'}`)
    })
  }
  test(`attachment parsing and chunk ranges select raw quantity ${count}`, () => {
    assert.equal(getAttachmentParsingSummaryHtml(t, { tool_data: { parsed_count: count, skipped_count: 0 } }), `Parsed ${marked(count)} ${count === 1 ? 'attachment' : 'attachments'}`)
    assert.equal(getAttachmentParsingSummaryHtml(t, { tool_data: { parsed_count: count, skipped_count: 2 } }), `Parsed ${marked(count)} ${count === 1 ? 'attachment' : 'attachments'}, ${marked(2)} skipped (still processing)`)
    assert.equal(getKnowledgeChunksSummaryHtml(t, { display_type: 'knowledge_chunks_list', fetched_chunks: 1, total_chunks: count }), `Loaded ${marked(1)} / ${marked(count)} ${count === 1 ? 'chunk' : 'chunks'}`)
  })
  test(`grep quantities select independently from HTML ${count}`, () => {
    assert.equal(formatCountCopy(t, 'agentStream.search.grepSummary', { chunks: count, docs: 1 }, { chunks: marked(count), docs: marked(1) }), `Found ${marked(count)} matching ${count === 1 ? 'chunk' : 'chunks'} across ${marked(1)} document`)
  })
}
