<template>
  <div class="report-markdown">
    <div class="md-content" v-html="reportHtml" @click="openCitation" />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { KnowledgeWorkflowCitation } from '@/api/knowledge-workflows'
import { createChatMarkdownRenderer, createIsolatedMarkdownParser, renderChatMarkdown } from '@/utils/chatMarkdownRenderer'
import { escapeHTML, safeMarkdownToHTML, sanitizeMarkdownHTML } from '@/utils/security'

const props = defineProps<{ markdown: string; citations: KnowledgeWorkflowCitation[] }>()
const emit = defineEmits<{ 'open-evidence': [evidenceId: string, quote?: string] }>()
const { t } = useI18n()
const markdownParser = createIsolatedMarkdownParser()
const citationById = computed(() => new Map(
  (props.citations || []).filter(citation => /^C\d+$/.test(citation.id) && citation.evidence_id)
    .map(citation => [citation.id, citation]),
))

const reportHtml = computed(() => {
  const renderer = createChatMarkdownRenderer()
  const renderText = renderer.text
  const renderLink = renderer.link
  const renderHtml = renderer.html
  const literalHtmlTags = new Map<string, number>()
  let insideLink = false
  renderer.html = function (token) {
    for (const tag of token.text.matchAll(/<(\/?)(a|code|pre|button)\b[^>]*>/gi)) {
      const name = tag[2]!.toLowerCase()
      const depth = literalHtmlTags.get(name) || 0
      literalHtmlTags.set(name, tag[1] ? Math.max(0, depth - 1) : depth + (tag[0].endsWith('/>') ? 0 : 1))
    }
    return renderHtml.call(this, token)
  }
  renderer.link = function (token) {
    const previous = insideLink
    insideLink = true
    try { return renderLink.call(this, token) }
    finally { insideLink = previous }
  }
  renderer.text = function (token) {
    const html = renderText.call(this, token)
    if (insideLink || [...literalHtmlTags.values()].some(depth => depth > 0) || token.type !== 'text' || token.tokens) return html
    return html.replace(/\[(C\d+)\]/g, (label, id: string) => {
      if (!citationById.value.has(id)) return label
      const accessibleLabel = escapeHTML(`${label} ${t('knowledgeWorkbench.evidence.openSnapshot')}`)
      return `<button type="button" class="report-citation" data-report-citation="${id}" aria-label="${accessibleLabel}">${label}</button>`
    })
  }
  return renderChatMarkdown(props.markdown, {
    renderer, escapeMarkdown: safeMarkdownToHTML, sanitizeHtml: sanitizeMarkdownHTML,
    streaming: false, markdownParser,
  })
})

function openCitation(event: MouseEvent) {
  const target = event.target as HTMLElement | null
  const button = target?.closest?.('button[data-report-citation]')
  const id = button?.getAttribute('data-report-citation') || ''
  const citation = citationById.value.get(id)
  if (citation?.evidence_id) emit('open-evidence', citation.evidence_id, citation.quote || '')
}
</script>

<style scoped lang="less">
@import '../../../components/css/markdown.less';

.report-markdown, .report-markdown :deep(.md-content) { min-width: 0; max-width: 100%; box-sizing: border-box; overflow-wrap: anywhere; }
.report-markdown { line-height: 1.7; }
.report-markdown :deep(.md-content a) { overflow-wrap: anywhere; word-break: normal; white-space: normal; }
.report-markdown :deep(.chat-markdown-table), .report-markdown :deep(.md-content pre) { min-width: 0; max-width: 100%; box-sizing: border-box; overflow-x: auto; }
.report-markdown :deep(.md-content img) { max-width: 100%; }
.report-markdown :deep(.report-citation) {
  padding: 0 3px;
  border: 0;
  border-radius: var(--td-radius-small);
  background: transparent;
  color: var(--td-brand-color);
  font: inherit;
  cursor: pointer;
}
.report-markdown :deep(.report-citation:hover) { background: var(--td-brand-color-light); }
.report-markdown :deep(.report-citation:active) { color: var(--td-brand-color-active); }
.report-markdown :deep(.report-citation:focus-visible) { outline: 2px solid var(--td-brand-color); outline-offset: 2px; }
</style>
