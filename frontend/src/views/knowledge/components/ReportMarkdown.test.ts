import assert from 'node:assert/strict'
import test from 'node:test'
import { createSSRApp } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { marked } from 'marked'
import { compiledSFCStyleRules, LONG_SOURCE_URL, styleDeclarationsAtWidth } from '../../../utils/sfcStyleTestHelpers'
import { configureMarkedForChatMarkdown } from '../../../utils/chatMarkdownRenderer'
import { registerDocumentMarkdownForReportTest, reportCitationClick, reportMarkdownTestComponent } from './reportMarkdownTestHarness'

const citations = [{ id: 'C1', kind: 'evidence', evidence_id: 'evidence-1', quote: 'Acme 发布了 新模型。' }]

test('report SFC keeps complete long URL labels and destinations alongside code and tables', async () => {
  const html = await renderToString(createSSRApp(reportMarkdownTestComponent(), {
    markdown: `${LONG_SOURCE_URL}\n\n[${LONG_SOURCE_URL}](${LONG_SOURCE_URL})\n\n[[C1]](${LONG_SOURCE_URL})\n\n\`\`\`text\n${LONG_SOURCE_URL}\n\`\`\`\n\n| Source | Label |\n| --- | --- |\n| [Original](${LONG_SOURCE_URL}) | Source |`, citations,
  }))
  assert.equal((html.match(new RegExp(`href="${LONG_SOURCE_URL.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}"`, 'g')) || []).length, 4)
  assert.ok(html.includes(`>${LONG_SOURCE_URL}</a>`))
  assert.ok(html.includes(`<pre><code class="language-text">${LONG_SOURCE_URL}\n</code></pre>`))
  assert.match(html, /<div class="chat-markdown-table"><table>/)
  assert.equal((html.match(/data-report-citation=/g) || []).length, 0)
})

test('compiled report CSS bounds content and wraps prose links at desktop and narrow widths', async () => {
  const rules = await compiledSFCStyleRules(new URL('./ReportMarkdown.vue', import.meta.url))
  for (const width of [1440, 390]) {
    for (const selector of ['.report-markdown', '.report-markdown .md-content']) {
      const style = styleDeclarationsAtWidth(rules, selector, width)
      assert.equal(style['min-width'], '0', `${selector} at ${width}`)
      assert.equal(style['max-width'], '100%', `${selector} at ${width}`)
      assert.equal(style['box-sizing'], 'border-box', `${selector} at ${width}`)
      assert.equal(style['overflow-wrap'], 'anywhere', `${selector} at ${width}`)
      assert.notEqual(style.overflow, 'hidden')
    }
    const link = styleDeclarationsAtWidth(rules, '.report-markdown .md-content a', width)
    assert.equal(link['overflow-wrap'], 'anywhere')
    assert.equal(link['white-space'], 'normal')
    for (const selector of ['.report-markdown .chat-markdown-table', '.report-markdown .md-content pre']) {
      const style = styleDeclarationsAtWidth(rules, selector, width)
      assert.equal(style['min-width'], '0')
      assert.equal(style['max-width'], '100%')
      assert.equal(style['overflow-x'], 'auto')
    }
  }
})

test('reports preserve nested Markdown link labels after document rendering', async () => {
  registerDocumentMarkdownForReportTest()
  configureMarkedForChatMarkdown()

  const opened: unknown[][] = []
  const component = reportMarkdownTestComponent()
  const html = await renderToString(createSSRApp(component, {
    markdown: '[C1]\n\n[[C1]](https://example.com/a)。 [[plain]](https://example.com/b)。 [普通公告](https://example.com/c) [Label [C1]](https://example.com/d)\n\n`[[C1]](https://example.com/code)`\n\n```md\n[[plain]](https://example.com/fence)\n```\n\n\\(x+y\\)',
    citations, onOpenEvidence: (...args: unknown[]) => opened.push(args),
  }))
  assert.match(html, /<a href="https:\/\/example.com\/a">\[C1\]<\/a>。/)
  assert.match(html, /<a href="https:\/\/example.com\/b">\[plain\]<\/a>。/)
  assert.match(html, /<a href="https:\/\/example.com\/c">普通公告<\/a>/)
  assert.match(html, /<a href="https:\/\/example.com\/d">Label \[C1\]<\/a>/)
  assert.match(html, /<code>\[\[C1\]\]\(https:\/\/example.com\/code\)<\/code>/)
  assert.match(html, /<pre><code[^>]*>\[\[plain\]\]\(https:\/\/example.com\/fence\)\n<\/code><\/pre>/)
  assert.match(html, /class="katex"/)
  assert.equal((html.match(/data-report-citation=/g) || []).length, 1)
  assert.doesNotMatch(html, /href="#"/)
  component.testState.openCitation(reportCitationClick('C1'))
  assert.deepEqual(opened, [['evidence-1', citations[0].quote]])
})

test('report parsing stays isolated when another view registers a global extension', async () => {
  const previous = { ...marked.defaults }
  try {
    marked.use({ hooks: { preprocess: markdown => markdown.replace('[[C1]]', 'GLOBAL_EXTENSION') } })
    assert.match(marked.parse('[[C1]](https://example.com/a)', { async: false }), /GLOBAL_EXTENSION/)
    const html = await renderToString(createSSRApp(reportMarkdownTestComponent(), {
      markdown: '[[C1]](https://example.com/a) [C1]', citations,
    }))
    assert.match(html, /<a href="https:\/\/example.com\/a">\[C1\]<\/a>/)
    assert.equal((html.match(/data-report-citation=/g) || []).length, 1)
    assert.doesNotMatch(html, /GLOBAL_EXTENSION/)
  } finally {
    marked.setOptions(previous)
  }
})

test('report SFC renders Markdown headings, paragraphs, lists and line breaks', async () => {
  const component = reportMarkdownTestComponent()
  const html = await renderToString(createSSRApp(component, {
    markdown: '# 研究报告\n\n第一行\n第二行\n\n## 来源\n\n- [C1] 发布公告\n- 补充材料\n\n1. 首项\n2. 次项', citations,
  }))
  assert.match(html, /<h1>研究报告<\/h1>/)
  assert.match(html, /<h2>来源<\/h2>/)
  assert.match(html, /<p>第一行<br>第二行<\/p>/)
  assert.match(html, /<ul>[\s\S]*?<li>[\s\S]*?发布公告<\/li>[\s\S]*?<li>补充材料<\/li>[\s\S]*?<\/ul>/)
  assert.match(html, /<ol>[\s\S]*?<li>首项<\/li>[\s\S]*?<li>次项<\/li>[\s\S]*?<\/ol>/)
  assert.match(html, /<button type="button" class="report-citation" data-report-citation="C1" aria-label="\[C1\] knowledgeWorkbench.evidence.openSnapshot">\[C1\]<\/button>/)
})

test('only real snapshot citations are interactive and clicks preserve the model quote', async () => {
  const component = reportMarkdownTestComponent()
  const opened: unknown[][] = []
  const html = await renderToString(createSSRApp(component, {
    markdown: '[C1] [C2] [C99] `literal [C1]`\n\n```text\n[C1]\n```\n\n[Label [C1]](https://example.com/source)\n\n<code>raw [C1]</code> <a href="https://example.com/raw">raw [C1]</a>\n\n\\[C1]',
    citations: [...citations, { id: 'C2', kind: 'web', url: 'https://example.com/other' }],
    onOpenEvidence: (...args: unknown[]) => opened.push(args),
  }))
  assert.equal((html.match(/data-report-citation=/g) || []).length, 1)
  assert.match(html, /<code>literal \[C1\]<\/code>/)
  assert.match(html, /<pre><code[^>]*>\[C1\]\n<\/code><\/pre>/)
  assert.match(html, /<a href="https:\/\/example.com\/source">Label \[C1\]<\/a>/)
  assert.match(html, /<code>raw \[C1\]<\/code>/)
  assert.match(html, /<a href="https:\/\/example.com\/raw">raw \[C1\]<\/a>/)
  assert.match(html, /\[C2\] \[C99\]/)
  component.testState.openCitation(reportCitationClick('C1'))
  component.testState.openCitation(reportCitationClick('C99'))
  component.testState.openCitation({ target: null })
  assert.deepEqual(opened, [['evidence-1', citations[0].quote]])
})

test('the report SFC inserts only HTML returned by the shared sanitizer', async () => {
  const rendered: string[] = []
  const component = reportMarkdownTestComponent(html => {
    rendered.push(html)
    return '<p>sanitized report</p>'
  })
  const html = await renderToString(createSSRApp(component, {
    markdown: '# Report\n\n[C1]<img src="x" onerror="alert(1)"><script>alert(1)</script>', citations,
  }))
  assert.match(rendered[0]!, /<h1>Report<\/h1>/)
  assert.match(rendered[0]!, /data-report-citation="C1"/)
  assert.match(html, /<p>sanitized report<\/p>/)
  assert.doesNotMatch(html, /onerror|<script|data-report-citation=/)
})
