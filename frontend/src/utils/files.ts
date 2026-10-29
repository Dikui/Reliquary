/**
 * Format a byte count as a human-readable string (B / KB / MB).
 * Returns '' for null/undefined/non-positive values so callers can use it as a v-if guard.
 */
export function formatFileSize(bytes?: number | string | null): string {
  if (bytes == null || bytes === '') return '';
  const n = typeof bytes === 'string' ? parseInt(bytes, 10) : bytes;
  if (Number.isNaN(n) || n <= 0) return '';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}
