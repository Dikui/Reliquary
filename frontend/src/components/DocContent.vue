<script setup lang="ts">
// @ts-nocheck
import { marked } from 'marked'
import markedKatex from 'marked-katex-extension'
import 'katex/dist/katex.min.css'
import hljs from 'highlight.js'
import 'highlight.js/styles/github.css'
import mermaid from 'mermaid'
import { ref, nextTick, onUnmounted, watch, computed, defineAsyncComponent, type PropType } from 'vue'
import { getEntryDetails } from '@/api/library'
import { numberReadingParagraphs, focusReadingParagraph, bindReadingImages } from '@/utils/readingParagraphs'
import { sanitizeHTML, safeMarkdownToHTML, createSafeImage, isValidImageURL, hydrateProtectedFileImages, createProtectedFileImageScope } from '@/utils/security'
import { normalizeSpuriousTablePrefixes } from '@/utils/markdownTableNormalize'
import { openMermaidFullscreen } from '@/utils/mermaidViewer'
import { useI18n } from 'vue-i18n'
import { entryProcessingState } from '@/utils/entryProcessingStatus'
import type { SourceLocation } from '@/types/search'
const { t, locale } = useI18n();
const EntryContentEditor = defineAsyncComponent(() => import('./EntryContentEditor.vue'));
const contentEditor = ref();
const editingEntry = ref<{ id: string; title: string } | null>(null);
const savedBody = ref<{ id: string; content: string } | null>(null);
let restoringEditedEntry = false;
function startContentEdit() {
  if (isProcessing.value || props.details.parse_status === 'deleting') return;
  editingEntry.value = { id: props.details.id, title: getDisplayTitle() };
  releasePreviewResources();
}
async function finishContentEdit() {
  restoringEditedEntry = false;
  editingEntry.value = null;
  releasePreviewResources();
  await nextTick();
  void runMarkdownPostRenderPipeline();
}
async function contentSaved(value: { id: string; content: string; status: string }) {
  savedBody.value = { id: value.id, content: value.content };
  await finishContentEdit();
  if (props.details.id !== value.id) return;
  if (value.status !== 'exists') applyProcessingState({ parse_status: 'pending', summary_status: 'pending', error_message: '', md: [], total: 0, description: '', structured_summary: null, entities: [], topics: [] });
  try {
    const result = await getEntryDetails(value.id);
    if (props.details.id !== value.id || !result.success) return;
    applyProcessingState(result.data);
    if (!isProcessing.value) emit('getDoc', 1);
  } catch { /* The existing processing poll refreshes status after a saved update. */ }
}


// Mermaid 初始化计数器，用于生成唯一ID
let mermaidRenderCount = 0;

// 初始化 Mermaid
mermaid.initialize({
  startOnLoad: false,
  theme: 'default',
  securityLevel: 'strict',
  fontFamily: 'PingFang SC, Microsoft YaHei, sans-serif',
  flowchart: {
    useMaxWidth: true,
    htmlLabels: true,
    curve: 'basis'
  },
  sequence: {
    useMaxWidth: true,
    diagramMarginX: 8,
    diagramMarginY: 8,
    actorMargin: 50,
    width: 150,
    height: 65
  },
  gantt: {
    useMaxWidth: true,
    leftPadding: 75,
    gridLineStartPadding: 35,
    barHeight: 20,
    barGap: 4,
    topPadding: 50
  }
});
const props = defineProps({
  visible: { type: Boolean, default: false },
  details: { type: Object as PropType<Record<string, any>>, required: true },
  libraryId: String,
  reference: Object as PropType<SourceLocation>,
});
let previewImages = createProtectedFileImageScope();
let markdownRenderGeneration = 0;
let mermaidRenderTask = Promise.resolve();
function releasePreviewResources() {
  markdownRenderGeneration++;
  previewImages.dispose();
  previewImages = createProtectedFileImageScope();
}
watch(() => props.details?.id, releasePreviewResources);
watch(() => props.details?.id, async id => {
  savedBody.value = null;
  const entry = editingEntry.value;
  if (!entry || id === entry.id) { restoringEditedEntry = false; return; }
  if (restoringEditedEntry) return;
  if (contentEditor.value && await contentEditor.value.confirmLeave()) await finishContentEdit();
  else if (!restoringEditedEntry) {
    restoringEditedEntry = true;
    emit('openEntry', entry.id);
  }
});

const emit = defineEmits(["closeDoc", "getDoc", "processingStateChange", "openEntry", "previewImage", "editingChange"]);
watch(editingEntry, value => { emit('editingChange', Boolean(value)); });
const isProcessing = computed(() => entryProcessingState(props.details || {}).active)
const applyProcessingState = (value: Record<string, unknown>) => {
  Object.assign(props.details, value)
  emit('processingStateChange', { ...value, id: props.details.id })
}
defineExpose({ startContentEdit })
marked.use({
  breaks: true,      // 启用单行换行转 <br>
  gfm: true,         // 启用 GitHub Flavored Markdown
});
marked.use(markedKatex({ throwOnError: false, nonStandard: true }));

const preprocessMathDelimiters = (rawText: string): string => {
  if (!rawText || typeof rawText !== 'string') {
    return '';
  }
  return rawText
    .replace(/\\\[([\s\S]*?)\\\]/g, '$$$$$1$$$$')
    .replace(/\\\(([\s\S]*?)\\\)/g, '$$$1$$');
};
const renderer = new marked.Renderer();
const docMarkdownRoot = ref<HTMLElement | null>(null)
const getMarkdownRenderRoot = (): ParentNode | null => docMarkdownRoot.value
const appendChunkContent = (acc: string, next: string, positionOverlap: number): string => {
  if (!acc) return next;
  if (!next) return acc;
  if (positionOverlap <= 0) return acc + next;

  const MIN_OVERLAP = 12;          // 过短的后缀容易误匹配（如分隔行），忽略
  const span = Math.max(positionOverlap, 0);
  // 搜索的后缀最大长度；按位置重叠量放大几倍兜底，并设下限
  const maxK = Math.min(acc.length, next.length, Math.max(span * 3, 400));
  // 重叠行之前最多允许多少前缀（补写的表头）被跳过
  const headSlack = Math.max(span * 2, 320);

  for (let k = maxK; k >= MIN_OVERLAP; k--) {
    const suffix = acc.slice(acc.length - k);
    const pos = next.indexOf(suffix);
    if (pos !== -1 && pos <= headSlack) {
      return acc + next.slice(pos + k);
    }
  }
  return acc + next;
};

/**
 * 合并分块内容，还原完整文档。chunks 按 start_at 排序后逐段用文本重叠匹配拼接。
 */
const mergeChunks = (chunks: any[]): string => {
  if (!chunks || chunks.length === 0) return '';

  // 按 start_at 排序
  const sortedChunks = [...chunks].sort((a, b) => {
    const startA = a.start_at ?? a.chunk_index ?? 0;
    const startB = b.start_at ?? b.chunk_index ?? 0;
    return startA - startB;
  });

  let merged = sortedChunks[0].content || '';
  let mergedEnd = sortedChunks[0].end_at ?? 0;

  for (let i = 1; i < sortedChunks.length; i++) {
    const currentChunk = sortedChunks[i];
    const currentStartAt = currentChunk.start_at ?? 0;
    const currentEndAt = currentChunk.end_at ?? 0;
    const currentContent = currentChunk.content || '';

    if (!currentContent) continue;

    // 与上一段有明显间隙（位置不相邻），用空行分隔后整段拼接
    if (currentStartAt > mergedEnd && mergedEnd > 0) {
      merged = merged + '\n\n' + currentContent;
    } else {
      const positionOverlap = mergedEnd - currentStartAt;
      merged = appendChunkContent(merged, currentContent, positionOverlap);
    }

    if (currentEndAt > mergedEnd) {
      mergedEnd = currentEndAt;
    }
  }

  return merged;
};

onUnmounted(() => previewImages.dispose())
const checkImage = (url) => {
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => resolve(true);
    img.onerror = () => resolve(false);
    img.src = url;
  });
};
const resolveVaultImageSource = (href: string) => ['article', 'mine', 'report'].includes(props.details?.source_kind) && props.details?.vault_path && href
    && !/^(?:[a-z][a-z\d+.-]*:|\/)/i.test(href)
    ? `vault://${encodeURIComponent(props.details.id)}/${encodeURIComponent(href)}`
    : href;
renderer.image = function ({ href, title, text }) {
  const source = resolveVaultImageSource(href);
  if (!isValidImageURL(source)) {
    return `<p>${t('error.invalidImageLink')}</p>`;
  }

  const safeImage = createSafeImage(source, text || '', title || '');
  return `<figure>
                ${safeImage}
                <figcaption style="text-align: left;">${text || ''}</figcaption>
            </figure>`;
};

// 自定义代码块渲染器，只显示语言标签
renderer.code = function ({ text, lang }) {
  // 空值校验：防止 text 为 undefined 或 null
  if (!text || typeof text !== 'string') {
    text = '';
  }

  // Mermaid 图表处理
  if (lang === 'mermaid') {
    // 生成唯一ID
    const id = `mermaid-${++mermaidRenderCount}`;
    // 返回带有 mermaid 类的 div，后续由 mermaid.run() 处理
    return `<div class="mermaid" id="${id}">${text}</div>`;
  }

  let detectedLang = lang;
  let highlighted = '';
  if (lang && hljs.getLanguage(lang)) {
    try {
      highlighted = hljs.highlight(text, { language: lang }).value;
    } catch (e) {
      highlighted = hljs.highlightAuto(text).value;
      detectedLang = hljs.highlightAuto(text).language || lang;
    }
  } else {
    const auto = hljs.highlightAuto(text);
    highlighted = auto.value;
    detectedLang = auto.language || lang;
  }
  const displayLang = detectedLang || 'Code';
  return `
    <div class="code-block-wrapper">
      <div class="code-block-header">
        <span class="code-block-lang">${displayLang}</span>
      </div>
      <pre class="code-block-pre"><code class="hljs language-${detectedLang || ''}">${highlighted}</code></pre>
    </div>
  `;
};
const isLegacyManualDraft = computed(() => props.details?.type === 'manual'
  && props.details?.parse_status === 'draft' && !props.details?.vault_path);
const legacyManualBody = computed(() => isLegacyManualDraft.value
  && typeof props.details?.metadata?.content === 'string' ? props.details.metadata.content : '');

// 监听 chunks 变化，自动更新合并内容（已改为 computed 属性）
const mergedContent = computed(() => {
  if (savedBody.value?.id === props.details?.id) return savedBody.value.content;
  const newChunks = props.details?.md;
  if (newChunks && newChunks.length > 0) {
    return mergeChunks(newChunks);
  }
  return legacyManualBody.value;
});

const runMarkdownPostRenderPipeline = async () => {
  if (!props.visible) return;
  const generation = ++markdownRenderGeneration;
  const scope = previewImages;
  await nextTick();
  if (generation !== markdownRenderGeneration || scope.signal.aborted || !props.visible) return;
  const renderRoot = getMarkdownRenderRoot();
  if (!renderRoot) {
    return;
  }
  const isCurrent = () => generation === markdownRenderGeneration && !scope.signal.aborted
    && props.visible && renderRoot === getMarkdownRenderRoot();
  if (!editingEntry.value) {
    const bodyRoot = renderRoot.querySelector('.md-content') as HTMLElement | null;
    if (bodyRoot) {
      numberReadingParagraphs(bodyRoot, mergedContent.value);
      focusReadingParagraph(bodyRoot, props.reference);
    }
  }
  await hydrateProtectedFileImages(
    renderRoot,
    props.libraryId ? { mode: 'library', libraryId: props.libraryId } : undefined,
    scope,
  );
  if (!isCurrent()) return;
  bindReadingImages(renderRoot as HTMLElement, image => emit('previewImage', image), t('entryDetail.previewImage'));
  const images = renderRoot?.querySelectorAll?.('img.markdown-image') as NodeListOf<HTMLImageElement> | undefined;
  if (images) {
    images.forEach(async item => {
      const isValid = await checkImage(item.src);
      if (isCurrent() && !isValid) {
        item.remove();
      }
    })
  }
  // 渲染 Mermaid 图表
  await renderMermaidDiagrams(renderRoot, isCurrent);
};

watch(() => props.visible, visible => {
  if (visible) void runMarkdownPostRenderPipeline()
  else { savedBody.value = null; releasePreviewResources() }
}, { flush: 'post' })
watch(() => props.reference, () => {
  const root = getMarkdownRenderRoot()?.querySelector('.md-content') as HTMLElement | null;
  if (root) focusReadingParagraph(root, props.reference);
}, { flush: 'post' });


// 渲染 Mermaid 图表的函数
const renderMermaidDiagrams = (renderRoot: ParentNode, isCurrent: () => boolean) => {
  const render = async () => {
    if (!isCurrent()) return;
    try {
      const nodes = Array.from(renderRoot.querySelectorAll('.mermaid'))
        .filter(node => node.getAttribute('data-processed') !== 'true');
      if (nodes.length) await mermaid.run({ nodes });
      if (isCurrent()) bindMermaidClickEvents(renderRoot);
    } catch (error) {
      if (isCurrent()) console.error('Mermaid rendering error:', error);
    }
  };
  // Mermaid mutates its nodes asynchronously. Keep renders ordered so an old
  // invocation cannot interrupt a newer body while it is drawing.
  mermaidRenderTask = mermaidRenderTask.then(render, render);
  return mermaidRenderTask;
};

// Mermaid 点击处理函数 - 必须在 bindMermaidClickEvents 之前定义
const handleMermaidClick = (e: Event) => {
  e.stopPropagation();
  const target = e.currentTarget as HTMLElement;
  const svg = target.querySelector('svg');
  if (svg) {
    openMermaidFullscreen(svg.outerHTML);
  }
};

// 为 Mermaid 容器绑定点击全屏事件（绑定在 div 上，不是 SVG 上）
const bindMermaidClickEvents = (renderRoot = getMarkdownRenderRoot()) => {
  if (!renderRoot) {
    return;
  }
  // 绑定在 .mermaid div 上，而不是 SVG 上
  const mermaidDivs = renderRoot.querySelectorAll('.mermaid');
  mermaidDivs.forEach((div) => {
    const divEl = div as HTMLElement;
    divEl.style.cursor = 'pointer';
    // 移除旧的事件监听器（避免重复绑定）
    divEl.removeEventListener('click', handleMermaidClick);
    divEl.addEventListener('click', handleMermaidClick);
  });
};

// 安全地处理 Markdown 内容（使用 marked）
const processMarkdown = (markdownText) => {
  if (!markdownText || typeof markdownText !== 'string') return '';

  // 去除 Markdown 头部的 YAML Frontmatter（例如 --- title: xxx ---）
  let processedText = markdownText.replace(/^\s*---\r?\n[\s\S]*?\r?\n---\r?\n/, '');

  // 先还原原始文本中的 HTML 实体，让它们作为普通字符参与渲染
  processedText = processedText
    .replace(/&#39;/g, "'")
    .replace(/&#x27;/gi, "'")
    .replace(/&apos;/g, "'")
    .replace(/&#34;/g, '"')
    .replace(/&#x22;/gi, '"')
    .replace(/&quot;/g, '"')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&');

  // 处理被 <p> 包裹的表格行，转换为正常的表格行，并在前后补空行
  processedText = processedText.replace(/<p>\s*(\|[\s\S]*?\|)\s*<\/p>/gi, '\n$1\n');

  // MarkItDown 常在表格前插入空行 + 分隔行，渲染会出现多余空行
  processedText = normalizeSpuriousTablePrefixes(processedText);

  // 保留表格单元格中的 <br>，不转成换行，避免打散表格；其他区域原样交给 marked 处理

  // 先预处理数学定界符，再做安全预处理
  const mathSafeText = preprocessMathDelimiters(processedText);
  const safeMarkdown = safeMarkdownToHTML(mathSafeText);

  // 使用标记渲染
  // Do not register this renderer globally. DocumentPreview uses the same
  // `marked` module; registering here made its later Markdown preview reuse
  // this image validator after the user had opened the chunk view.
  let html = marked.parse(safeMarkdown, { renderer }) as string;
  html = html.replace(/(<img\b[^>]*\ssrc\s*=\s*)(["'])([^"']+)\2/gi,
    (_match, prefix, quote, src) => `${prefix}${quote}${resolveVaultImageSource(src)}${quote}`);

  // 还原被转义的 <br>
  html = html.replace(/&lt;br\s*\/?&gt;/gi, '<br>');

  // 最终安全清理
  let result = sanitizeHTML(html);

  return result;
};
// v-html must be stable while status, reference or other metadata changes.
// Recomputing the renderer on every component render replaces hydrated images
// and rendered SVG with their original Markdown placeholders.
let previousRenderKey: unknown[] | undefined;
let previousRenderedContent = '';
const renderedMergedContent = computed(() => {
  const key = [mergedContent.value, props.details.id, props.details.source_kind, props.details.vault_path, locale?.value];
  if (!previousRenderKey || key.some((value, index) => value !== previousRenderKey[index])) {
    previousRenderedContent = processMarkdown(mergedContent.value);
    previousRenderKey = key;
  }
  return previousRenderedContent;
});
watch([renderedMergedContent, () => props.details.id, () => props.details.source_kind, () => props.details.vault_path, () => props.libraryId], () => {
  void runMarkdownPostRenderPipeline();
}, { immediate: true, flush: 'post' });
// 获取显示标题
const getDisplayTitle = () => {
  if (!props.details.title) return '';
  if (props.details.type === 'file') {
    // 文件类型去掉扩展名
    const lastDotIndex = props.details.title.lastIndexOf(".");
    return lastDotIndex > 0 ? props.details.title.substring(0, lastDotIndex) : props.details.title;
  }
  // URL和手动创建直接返回标题
  return props.details.title;
};

</script>
<template>
  <div ref="docMarkdownRoot" class="doc-reading">
    <EntryContentEditor v-if="editingEntry" ref="contentEditor" :key="editingEntry.id"
      :entry-id="editingEntry.id" :title="editingEntry.title" :index-only="details.source_kind === 'mine'"
      @cancel="finishContentEdit" @saved="contentSaved" @preview="runMarkdownPostRenderPipeline">
      <template #preview="{ content }"><div class="md-content" v-html="processMarkdown(content)" /></template>
    </EntryContentEditor>
    <div v-else class="md-content" v-html="renderedMergedContent" />
  </div>
</template>
<style scoped>
.doc-reading { min-width: 0; color: var(--text); font-family: var(--font); font-size: 17px; line-height: 1.85; }
.doc-reading :deep(.md-content) { font: inherit; color: var(--text); overflow-wrap: anywhere; }
.doc-reading :deep(.md-content p), .doc-reading :deep(.md-content blockquote), .doc-reading :deep(.md-content ul), .doc-reading :deep(.md-content ol), .doc-reading :deep(.md-content dl), .doc-reading :deep(.md-content table), .doc-reading :deep(.md-content summary) { font-family: inherit; font-size: inherit; line-height: inherit; }
.doc-reading :deep(.md-content p), .doc-reading :deep(.md-content blockquote), .doc-reading :deep(.md-content ul), .doc-reading :deep(.md-content ol), .doc-reading :deep(.md-content dl) { margin: 0 0 16px; }
.doc-reading :deep(.md-content li > ul), .doc-reading :deep(.md-content li > ol), .doc-reading :deep(.md-content blockquote > :last-child) { margin-bottom: 0; }
.doc-reading :deep(.md-content h1), .doc-reading :deep(.md-content h2), .doc-reading :deep(.md-content h3), .doc-reading :deep(.md-content h4), .doc-reading :deep(.md-content h5), .doc-reading :deep(.md-content h6) { font-family: var(--font); color: var(--text); font-weight: 800; }
.doc-reading :deep(.md-content h1) { font-size: var(--text-xl); }
.doc-reading :deep(.md-content h2) { font-size: var(--text-lg); padding-top: 8px; }
.doc-reading :deep(.md-content h3) { font-size: 18px; }
.doc-reading :deep(.md-content h4), .doc-reading :deep(.md-content h5), .doc-reading :deep(.md-content h6) { font-size: inherit; }
.doc-reading :deep(.md-content > :first-child) { margin-top: 0; }
.doc-reading :deep([data-paragraph]) { position: relative; scroll-margin-block: 24px; }
.doc-reading :deep([data-paragraph]::before) { content: '¶' attr(data-paragraph); position: absolute; inset-inline-end: calc(100% + 12px); top: 4px; color: var(--muted); font: var(--text-xs) var(--mono); white-space: nowrap; }
.doc-reading :deep(.md-content a) { color: inherit; text-decoration: underline; }
.doc-reading :deep(.md-content a:hover) { color: var(--accent); }
.doc-reading :deep(.md-content img), .doc-reading :deep(.md-content svg) { max-width: 100%; height: auto; }
.doc-reading :deep(.md-content .mermaid), .doc-reading :deep(.md-content figure), .doc-reading :deep(.md-content img), .doc-reading :deep(.md-content blockquote), .doc-reading :deep(.md-content .code-block-wrapper), .doc-reading :deep(.md-content pre), .doc-reading :deep(.md-content code), .doc-reading :deep(.md-content table) { border-radius: 0; box-shadow: none; }
.doc-reading :deep(.md-content blockquote) { border-left: 1px solid var(--rule); background: var(--surface-soft); padding: 16px; }
.doc-reading :deep(.md-content figure) { background: transparent; }
.doc-reading :deep(.md-content .code-block-wrapper) { border: 1px solid var(--border); background: var(--surface-soft); overflow: auto; }
.doc-reading :deep(.md-content .code-block-header), .doc-reading :deep(.md-content pre), .doc-reading :deep(.md-content code) { background: var(--surface-soft); color: var(--text); }
.doc-reading :deep(.md-content .code-block-header) { border-bottom: 1px solid var(--border); }
.doc-reading :deep(.md-content code) { font-family: var(--mono); font-size: 0.9em; padding: 2px 4px; }
.doc-reading :deep(.md-content .code-block-pre) { margin: 0; padding: 16px; overflow: auto; font-family: var(--mono); }
.doc-reading :deep(.md-content .code-block-pre code) { background: transparent; font-size: inherit; padding: 0; white-space: pre; }
.doc-reading :deep(.md-content .mermaid), .doc-reading :deep(.md-content .katex-display) { overflow-x: auto; }
.doc-reading :deep(.md-content .mermaid) { background: var(--surface-soft); }
.doc-reading :deep(.reading-image) { display: inline-block; max-width: 100%; border: 0; padding: 0; background: transparent; color: inherit; cursor: zoom-in; }
.doc-reading :deep(.reading-image:focus-visible) { outline: 2px solid var(--accent); outline-offset: 3px; }
.doc-reading :deep(.md-content table) { display: block; max-width: 100%; overflow-x: auto; border-collapse: collapse; }
.doc-reading :deep(.md-content table tr) { border-top: 1px solid var(--border); }
.doc-reading :deep(.md-content table thead), .doc-reading :deep(.md-content table tr:nth-child(2n)) { background: var(--surface-soft); }
.doc-reading :deep(.md-content table th), .doc-reading :deep(.md-content table td) { border: 1px solid var(--border); padding: 8px; }
.doc-reading :deep(.md-content [data-reference]) { background: var(--select); color: var(--on-select); outline: 6px solid var(--select); }
.doc-reading :deep(.md-content [data-reference] *) { color: var(--on-select); }
@media (max-width: 960px) { .doc-reading :deep([data-paragraph]::before) { position: static; display: block; padding-bottom: 4px; } }
</style>
