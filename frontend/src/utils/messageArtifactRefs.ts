/**
 * 把回答正文中的消息文件引用接到带鉴权的下载与预览链路。
 * resource 句柄与知识库图片相同，句柄不属于本消息时交回默认渲染。
 * 图片内联显示，其余文件通过 ChatArtifactsDrawer 预览。
 */

import { escapeHTML } from './security.ts';

/** 与后端 artifactListItem / SSE publicArtifactViews 对齐的最小字段集。 */
export interface ArtifactRefMeta {
  index: number;
  file_name: string;
  file_type?: string;
  /** `resource://<handle>`。后端未启用资源目录时为空。 */
  handle?: string;
  /**
   * 历史消息直接携带 `Message.Artifacts`，其中的存储引用叫 `url`。与 handle
   * 同义，取其一即可。
   */
  url?: string;
}

export interface ArtifactRefContext {
  sessionId: string;
  messageId: string;
}

export interface ArtifactRefLabels {
  /** 卡片副标题，如「点击预览」。 */
  previewHint: string;
  /** 本轮已结束但引用对不上任何产物时的副标题，如「文件不可用」。 */
  missingHint: string;
}

const RESOURCE_HANDLE_RE = /^resource:\/\/([A-Za-z0-9_-]{22})$/;
const IMAGE_EXTENSIONS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'avif']);
const TRANSPARENT_PIXEL =
  'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==';

type ArtifactRef = { handle: string };

function parseArtifactRef(href: string): ArtifactRef | null {
  const handle = (href || '').trim().match(RESOURCE_HANDLE_RE)?.[1];
  return handle ? { handle } : null;
}

/**
 * 该链接目标是否可能是消息文件引用。
 *
 * 句柄形式与知识库图片同形，因此这里为真只说明「值得交给产物解析试一次」，
 * 不代表本消息真有这个文件。
 */
export function isArtifactRefHref(href: string): boolean {
  return parseArtifactRef(href) !== null;
}

function artifactHandle(artifact: ArtifactRefMeta): string {
  const raw = (artifact.handle || artifact.url || '').trim();
  return raw.match(RESOURCE_HANDLE_RE)?.[1] || '';
}

/** 把引用解析成具体产物；解析不到返回 null。 */
export function resolveArtifactRef(
  href: string,
  artifacts: ArtifactRefMeta[] | undefined | null,
): ArtifactRefMeta | null {
  const ref = parseArtifactRef(href);
  if (!ref || !artifacts?.length) return null;

  return artifacts.find((item) => artifactHandle(item) === ref.handle) || null;
}

function fileExtension(fileName: string): string {
  const base = (fileName || '').trim().toLowerCase();
  const dot = base.lastIndexOf('.');
  return dot > 0 ? base.slice(dot + 1) : '';
}

/**
 * 是否按图片内联渲染。SVG 刻意排除：它是可执行内容，走卡片与隔离预览路径。
 */
function rendersAsImage(artifact: ArtifactRefMeta): boolean {
  const ext = fileExtension(artifact.file_name);
  if (ext) return IMAGE_EXTENSIONS.has(ext);
  const type = (artifact.file_type || '').toLowerCase();
  return type.startsWith('image/') && !type.includes('svg');
}

// blob URL 按 (会话, 消息, 下标) 缓存，并挂在 window 上：Vite 热更新会替换模块
// 但不会重建文档，模块级 Map 会让已加载的图片退回占位图。
type ArtifactBlobState = { blobByKey: Map<string, string>; inflight: Map<string, Promise<string | null>> };

const artifactBlobState: ArtifactBlobState = (() => {
  const fresh = (): ArtifactBlobState => ({ blobByKey: new Map(), inflight: new Map() });
  if (typeof window === 'undefined') return fresh();
  const scope = window as typeof window & { __reliquaryArtifactBlobCacheV1__?: ArtifactBlobState };
  scope.__reliquaryArtifactBlobCacheV1__ ||= fresh();
  return scope.__reliquaryArtifactBlobCacheV1__;
})();

function blobCacheKey(ctx: ArtifactRefContext, index: number): string {
  return `${ctx.sessionId}\u0000${ctx.messageId}\u0000${index}`;
}

function fileIconSvg(): string {
  return (
    '<svg class="artifact-ref-card__glyph" viewBox="0 0 24 24" aria-hidden="true">'
    + '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6z" '
    + 'fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/>'
    + '<path d="M14 2v6h6" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/>'
    + '</svg>'
  );
}

function renderCard(fileName: string, hint: string, index: number | null): string {
  const safeName = escapeHTML(fileName);
  const safeHint = escapeHTML(hint);
  // 卡片必须是内联元素：marked 会把图片包在 <p> 里，块级元素会被 HTML 解析器
  // 提到段落外面，破坏正文结构。
  const interactive = index === null
    ? ''
    : ` data-artifact-index="${index}" role="button" tabindex="0"`;
  const state = index === null ? ' artifact-ref-card--pending' : '';
  return (
    `<span class="artifact-ref-card${state}"${interactive} title="${safeName}">`
    + `<span class="artifact-ref-card__icon" aria-hidden="true">${fileIconSvg()}</span>`
    + '<span class="artifact-ref-card__text">'
    + `<span class="artifact-ref-card__name">${safeName}</span>`
    + `<span class="artifact-ref-card__hint">${safeHint}</span>`
    + '</span></span>'
  );
}

function renderImage(
  artifact: ArtifactRefMeta,
  alt: string,
  ctx: ArtifactRefContext | null,
): string {
  const safeAlt = escapeHTML(alt || artifact.file_name || '');
  // 已经拉取过就直接给 blob：流式重渲染会重建 <img>，否则每帧都会闪回占位图。
  const cached = ctx ? artifactBlobState.blobByKey.get(blobCacheKey(ctx, artifact.index)) : undefined;
  const src = cached || TRANSPARENT_PIXEL;
  const loading = cached ? '' : ' data-img-loading="1"';
  return (
    `<img class="markdown-image artifact-ref-image" src="${src}" alt="${safeAlt}"`
    + ` data-artifact-index="${artifact.index}"${loading}>`
  );
}

/**
 * 渲染一个 Markdown 图片/链接目标。
 *
 * 返回 null 表示这不是本消息的文件引用，调用方应回落到默认渲染（普通图片、
 * `resource://` 受保护图片、外链等一律不受影响）。
 */
export function renderArtifactReference(args: {
  href: string;
  alt?: string;
  artifacts?: ArtifactRefMeta[] | null;
  labels: ArtifactRefLabels;
  context?: ArtifactRefContext | null;
  /** 本轮回答还在生成中。产物要到本轮结束才会收集，此时解析不到是正常的。 */
  streaming?: boolean;
}): string | null {
  const ref = parseArtifactRef(args.href);
  if (!ref) return null;

  const artifact = resolveArtifactRef(args.href, args.artifacts);
  if (!artifact) return null;

  if (rendersAsImage(artifact)) {
    return renderImage(artifact, args.alt || '', args.context ?? null);
  }
  return renderCard(artifact.file_name, args.labels.previewHint, artifact.index);
}

async function loadArtifactBlobURL(ctx: ArtifactRefContext, index: number): Promise<string | null> {
  const key = blobCacheKey(ctx, index);
  const cached = artifactBlobState.blobByKey.get(key);
  if (cached) return cached;

  let task = artifactBlobState.inflight.get(key);
  if (!task) {
    task = (async () => {
      try {
        // Imported lazily so parsing/rendering stays free of the axios
        // transport — those parts run in plain Node during unit tests.
        const { downloadArtifact } = await import('@/api/chat');
        const blob = await downloadArtifact(ctx.sessionId, ctx.messageId, index);
        const blobURL = URL.createObjectURL(blob);
        artifactBlobState.blobByKey.set(key, blobURL);
        return blobURL;
      } catch (error) {
        console.warn('[messageArtifactRefs] artifact image load failed:', error);
        return null;
      } finally {
        artifactBlobState.inflight.delete(key);
      }
    })();
    artifactBlobState.inflight.set(key, task);
  }
  return task;
}

/**
 * 把正文里的产物图片换成带鉴权拉取的 blob。
 *
 * 与 hydrateProtectedFileImages 同样是幂等的：已换过的元素带 authHydrated 标记，
 * 相同文件的并发请求共享同一个 Promise。
 */
export async function hydrateArtifactImages(
  root: ParentNode | null | undefined,
  ctx: ArtifactRefContext | null | undefined,
): Promise<void> {
  if (!root || !ctx?.sessionId || !ctx?.messageId) return;

  const images = Array.from(
    root.querySelectorAll<HTMLImageElement>('img.artifact-ref-image[data-artifact-index]'),
  ).filter((img) => img.dataset.authHydrated !== '1');
  if (!images.length) return;

  await Promise.all(images.map(async (img) => {
    const index = Number(img.getAttribute('data-artifact-index'));
    if (!Number.isInteger(index) || index < 0) return;
    img.dataset.authHydrated = '1';

    const blobURL = await loadArtifactBlobURL(ctx, index);
    if (!blobURL) {
      img.dataset.authHydrated = '0';
      return;
    }
    img.src = blobURL;
    img.removeAttribute('data-img-loading');
  }));
}

/**
 * 从点击/键盘事件里取出被激活的产物卡片下标；返回 null 表示事件与卡片无关。
 */
export function artifactIndexFromEventTarget(target: EventTarget | null): number | null {
  if (!(target instanceof Element)) return null;
  const card = target.closest('.artifact-ref-card[data-artifact-index]');
  if (!card) return null;
  const index = Number(card.getAttribute('data-artifact-index'));
  return Number.isInteger(index) && index >= 0 ? index : null;
}
