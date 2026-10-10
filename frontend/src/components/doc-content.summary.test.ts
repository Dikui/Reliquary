import assert from 'node:assert/strict'
import { safeTextareaTestModule } from './__tests__/safeTextareaStub'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { createSSRApp, defineComponent, h, nextTick, onServerPrefetch } from 'vue'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zhCN from '../i18n/locales/zh-CN'
import enUS from '../i18n/locales/en-US'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const emptyComponent = defineComponent({ setup: () => () => null })
const emptyModule = { __esModule: true, default: emptyComponent }

function evaluate(code: string, modules: Record<string, unknown>) {
  const output = ts.transpileModule(code, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', output)(
    (name: string) => name.endsWith('.css') ? {} : name.endsWith('.png') ? '/image.png' : modules[name] ?? require(name),
    module, module.exports,
  )
  return module.exports
}

function compileSFC(path: string) {
  const filename = path.split('/').at(-1)!
  const { descriptor } = parse(readFileSync(new URL(path, import.meta.url), 'utf8'), { filename })
  const script = compileScript(descriptor, { id: `summary-test-${filename}` })
  const template = compileTemplate({
    id: `summary-test-${filename}`, filename, source: descriptor.template!.content,
    ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings },
  })
  assert.deepEqual(template.errors, [])
  return (modules: Record<string, unknown>) => {
    const component = evaluate(script.content, modules).default
    component.ssrRender = evaluate(template.code, modules).ssrRender
    return component
  }
}

const detailSFC = compileSFC('./doc-content.vue')
const cardSFC = compileSFC('../views/knowledge/components/DocumentCardView.vue')
const listSFC = compileSFC('../views/knowledge/components/DocumentListView.vue')

async function render(component: any, props: Record<string, unknown>, locale: keyof typeof messages) {
  const app = createSSRApp(component, props)
  app.use(createI18n({ legacy: false, locale, messages }))
  const stub = defineComponent({
    props: ['visible', 'content', 'disabled'],
    setup(props, { attrs, slots }) {
      return () => props.visible === false ? null : h('div', attrs, [slots.header?.(), slots.default?.()])
    },
  })
  for (const name of ['t-drawer', 't-button', 't-icon', 't-tag', 't-tooltip', 't-loading', 't-select', 't-input',
    't-textarea', 't-checkbox', 't-popconfirm', 't-popup', 't-empty', 't-switch', 't-pagination', 't-radio']) app.component(name, stub)
  const errors: unknown[] = []
  const warnings: string[] = []
  app.config.errorHandler = error => errors.push(error)
  app.config.warnHandler = warning => warnings.push(warning)
  const html = await renderToString(app)
  assert.deepEqual(errors, [])
  assert.deepEqual(warnings, [])
  return html
}

async function renderDetails(locale: keyof typeof messages, summary_status: string, options: {
  description?: string
  loggedIn?: boolean
  refresh?: { summary_status: string; description: string }
} = {}) {
  const notifications: Array<[string, string]> = []
  const component = detailSFC({
    marked: { marked: { use() {}, Renderer: class {}, parse: (value: string) => value } },
    'marked-katex-extension': { __esModule: true, default: () => ({}) },
    'highlight.js': { __esModule: true, default: {} },
    mermaid: { __esModule: true, default: { initialize() {} } },
    'tdesign-vue-next': { MessagePlugin: Object.fromEntries(['success', 'info', 'error'].map(tone =>
      [tone, (text: string) => notifications.push([tone, text])])) },
    '@/api/knowledge-base/index': { KNOWLEDGE_CHUNK_PAGE_SIZE: 20,
      regenerateKnowledgeSummary: async () => ({ success: true, data: options.refresh }),
    },
    '@/utils/security': {}, '@/utils/markdownTableNormalize': {}, '@/utils/mermaidViewer': {}, '@/utils/textLineDiff': {},
    '@/stores/auth': { useAuthStore: () => ({ isLoggedIn: options.loggedIn ?? true }) },
    '@/components/SafeTextarea.vue': safeTextareaTestModule,
    '@/components/document-preview.vue': emptyModule,
    '@/components/knowledge-processing-timeline.vue': emptyModule,
    '@/views/knowledge/components/RepostSources.vue': emptyModule,
    '@/views/knowledge/knowledgeDownloadFileName': {}, '@/utils/filePreview': {}, '@/utils/api-base': {},
  })
  if (options.refresh) {
    const setup = component.setup
    component.setup = (props: object, context: object) => {
      const state = setup(props, context)
      onServerPrefetch(async () => { await state.refreshSummary(); await nextTick() })
      return state
    }
  }
  const html = await render(component, {
    visible: true, canEditKB: false, canDownloadKB: false, kbId: 'kb-image', knowledgeType: 'document',
    details: { id: 'image', title: 'PNG image', type: 'manual', parse_status: 'completed',
      summary_status, description: options.description ?? '', md: [], total: 0, metadata: {} },
  }, locale)
  return { html, notifications }
}

const listModules = {
  '../tagLabel': {}, '@/utils/files': { formatFileSize: () => '100 B', getFileIcon: () => 'image' },
  '@/composables/useTagChipsOverflow': { useTagChipsOverflow: () => ({ getTagLimit: () => 0 }) },
  './DocumentActionMenu.vue': emptyModule,
  '@/components/knowledge-processing-timeline.vue': emptyModule,
}
const listProps = {
  selectedIds: new Set(), batchMode: false, canEdit: false, canDownload: false, canMutateKnowledge: false,
  traceAvailableById: {}, traceVisibleIds: {}, tagList: [], starredTagIds: new Set(),
  moveMenuMode: 'normal', moveTargetKbs: [], moveTargetsLoading: false,
  moveSelectedTargetName: '', moveMode: 'reparse', moveSubmitting: false,
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  const copy = messages[locale].knowledgeBase
  test(`${locale} actual summary section explains skipped image text for both editable and read-only details`, async () => {
    for (const loggedIn of [true, false]) {
      const { html } = await renderDetails(locale, 'skipped', { loggedIn })
      assert.ok(html.includes(copy.summarySkippedNoText))
      assert.ok(!html.includes(copy.noDocumentSummary))
      assert.ok(!html.includes(copy.generatingSummary))
      assert.ok(!html.includes(copy.summaryGenerationFailed))
    }
  })

  test(`${locale} actual document card and list keep skipped summary explanations with completed ingestion`, async () => {
    for (const create of [cardSFC, listSFC]) {
      const html = await render(create(listModules), { ...listProps,
        items: [{ id: 'image', file_name: 'image.png', parse_status: 'completed', summary_status: 'skipped' }],
      }, locale)
      assert.ok(html.includes(copy.summarySkippedNoText))
      assert.ok(!html.includes(copy.generatingSummary))
    }
  })

  test(`${locale} skipped refresh shows informational feedback and completes polling`, async () => {
    const { html, notifications } = await renderDetails(locale, 'completed', {
      refresh: { summary_status: 'skipped', description: '' },
    })
    assert.ok(html.includes(copy.summarySkippedNoText))
    assert.deepEqual(notifications, [['info', copy.summarySkippedNoText]])
  })

  test(`${locale} completed text summary remains visible and refresh failures use error feedback`, async () => {
    const { html } = await renderDetails(locale, 'completed', { description: 'Text document summary' })
    assert.ok(html.includes('Text document summary'))
    assert.ok(!html.includes(copy.summarySkippedNoText))
    const failed = await renderDetails(locale, 'completed', {
      refresh: { summary_status: 'failed', description: '' },
    })
    assert.deepEqual(failed.notifications, [['error', copy.summaryGenerationFailed]])
    assert.ok(!failed.html.includes(copy.summarySkippedNoText))
  })
}

test('vision capability explanation describes its image processing use in both product locales', () => {
  assert.match(zhCN.model.editor.supportsVisionDesc, /开启后可在知识库中用于图片文字提取和描述/)
  assert.match(enUS.model.editor.supportsVisionDesc, /When enabled, it can extract text from images and describe them in knowledge bases/)
})
