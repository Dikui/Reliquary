export const DEFAULT_SESSION_BUCKET_KEY = 'web'

export function findSessionBucketKey(
  buckets: Record<string, { items: Array<{ id: string }> }>,
  sessionId: string,
): string | null {
  for (const [key, bucket] of Object.entries(buckets)) {
    if (bucket.items.some((row) => row.id === sessionId)) return key
  }
  return null
}
