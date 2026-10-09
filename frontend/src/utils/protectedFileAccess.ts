/**
 * 受保护文件（provider:// / resource:// 等）的访问上下文。
 *
 * 文件代理通过登录态 Bearer 鉴权，并按知识库或消息校验访问作用域。
 * 渲染组件声明作用域，由此处统一构造代理请求。
 */

export const PROVIDER_SCHEME_PATTERN = 'resource|local';

const PROVIDER_FILE_SCHEME_RE = new RegExp(`^(${PROVIDER_SCHEME_PATTERN}):\\/\\/\\S+$`, 'i');

const KB_FILE_PROXY_PATH_RE = /^\/api\/v1\/knowledge-bases\/[^/]+\/files$/;
const MESSAGE_FILE_PROXY_PATH_RE = /^\/api\/v1\/sessions\/[^/]+\/messages\/[^/]+\/files$/;

export type ProtectedFileAccessContext =
  /** 登录用户通过 Bearer 访问文件。 */
  | { mode: 'tenant' }
  /** 知识库作用域。 */
  | { mode: 'knowledgeBase'; kbId: string }
  /** 消息作用域。 */
  | { mode: 'message'; sessionId: string; messageId: string };

export interface ProtectedFileRequest {
  url: string;
  headers: Record<string, string>;
}

const TENANT_ACCESS: ProtectedFileAccessContext = { mode: 'tenant' };

interface ProtectedFileAccessState {
  current: ProtectedFileAccessContext;
}

// 与 blob 缓存同样挂在 window 上：Vite 热更新会替换模块但不会重建文档，
// 保留应用启动时注册的上下文。
const accessState: ProtectedFileAccessState = (() => {
  const fresh = (): ProtectedFileAccessState => ({ current: TENANT_ACCESS });
  if (typeof window === 'undefined') return fresh();
  const scope = window as typeof window & {
    __reliquaryProtectedFileAccessV1__?: ProtectedFileAccessState;
  };
  scope.__reliquaryProtectedFileAccessV1__ ||= fresh();
  return scope.__reliquaryProtectedFileAccessV1__;
})();

/**
 * 注册当前文档的默认访问上下文。
 */
export function setDefaultProtectedFileAccess(
  access: ProtectedFileAccessContext | null,
): void {
  accessState.current = access ?? TENANT_ACCESS;
}

export function getDefaultProtectedFileAccess(): ProtectedFileAccessContext {
  return accessState.current;
}

/**
 * 合并默认上下文与组件传入的作用域。
 */
export function resolveProtectedFileAccess(
  override?: ProtectedFileAccessContext | null,
): ProtectedFileAccessContext {
  const fallback = accessState.current;

  if (!override) return fallback;
  if (override.mode === 'knowledgeBase' && !override.kbId.trim()) return fallback;
  if (override.mode === 'message' && (!override.sessionId.trim() || !override.messageId.trim())) {
    return fallback;
  }
  return override;
}

/** 是否为需要经代理拉取的 resource:// 或 local:// 路径。 */
export function isProviderFileURL(url: string): boolean {
  const trimmed = url.trim();
  return PROVIDER_FILE_SCHEME_RE.test(trimmed);
}

/** 是否为受保护文件代理之一的路径。 */
export function isProtectedFileProxyPath(pathname: string): boolean {
  return (
    pathname === '/files'
    || KB_FILE_PROXY_PATH_RE.test(pathname)
    || MESSAGE_FILE_PROXY_PATH_RE.test(pathname)
  );
}

function tenantRequestHeaders(): Record<string, string> {
  const headers: Record<string, string> = {};
  try {
    const token = (localStorage.getItem('reliquary_token') || '').trim();
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }
  } catch {
    // ignore localStorage read errors
  }
  return headers;
}

/**
 * 为存储路径构造代理请求。非存储路径返回 null。
 */
export function buildProtectedFileRequest(
  sourceURL: string,
  access: ProtectedFileAccessContext,
): ProtectedFileRequest | null {
  const filePath = sourceURL.trim();
  if (!isProviderFileURL(filePath)) return null;

  const query = new URLSearchParams({ file_path: filePath }).toString();

  if (access.mode === 'knowledgeBase') {
    return {
      url: `/api/v1/knowledge-bases/${encodeURIComponent(access.kbId.trim())}/files?${query}`,
      headers: tenantRequestHeaders(),
    };
  }

  if (access.mode === 'message') {
    return {
      url: `/api/v1/sessions/${encodeURIComponent(access.sessionId.trim())}/messages/${encodeURIComponent(access.messageId.trim())}/files?${query}`,
      headers: tenantRequestHeaders(),
    };
  }

  return { url: `/files?${query}`, headers: tenantRequestHeaders() };
}
