// Matches internal/errors/knowledge_domain.go.
export const KNOWLEDGE_DOMAIN_REFERENCED_CODE = 2400

export function domainErrorMessage(
  cause: unknown,
  fallback: string,
  referencedMessage?: (count: number) => string,
): string {
  if (cause instanceof Error && cause.message.trim()) return cause.message
  if (!cause || typeof cause !== 'object') return fallback

  const error = cause as { status?: unknown; message?: unknown; error?: unknown }
  if (referencedMessage && (error.status === undefined || error.status === 409)
    && error.error && typeof error.error === 'object') {
    const detail = error.error as { code?: unknown; details?: { knowledge_base_count?: unknown } | null }
    const count = detail.details?.knowledge_base_count
    if (detail.code === KNOWLEDGE_DOMAIN_REFERENCED_CODE
      && typeof count === 'number' && Number.isSafeInteger(count) && count > 0) {
      return referencedMessage(count)
    }
  }
  if (typeof error.message === 'string' && error.message.trim()) return error.message
  if (typeof error.error === 'string' && error.error.trim()) return error.error
  if (error.error && typeof error.error === 'object') {
    const detail = error.error as { message?: unknown }
    if (typeof detail.message === 'string' && detail.message.trim()) return detail.message
  }
  return fallback
}
