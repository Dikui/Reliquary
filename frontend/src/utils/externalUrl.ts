/** Only absolute HTTP and HTTPS source links may leave the application. */
export function safeExternalUrl(value: unknown): string {
  if (typeof value !== 'string' || !value) return ''
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : ''
  } catch {
    return ''
  }
}
