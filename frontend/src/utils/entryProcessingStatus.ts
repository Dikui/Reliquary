export type TimelineStatusInput = {
  parseStatus?: string
  traceStatus?: string
  isLatestAttempt: boolean
}

/**
 * Convert the entry-level parse status to the span status vocabulary used
 * by the processing timeline.
 */
export function parseStatusToTimelineStatus(status?: string): string {
  switch (status) {
    case 'completed':
      return 'done'
    case 'processing':
      return 'running'
    default:
      return status || ''
  }
}

/**
 * The entry row is authoritative for the latest attempt. The root span
 * only describes the main parse pipeline and may already be `done` while
 * asynchronous enrichment is still running or has subsequently failed.
 * Historical attempts have no matching entry-row snapshot, so they keep
 * their own persisted root-span status.
 */
export function resolveTimelineHeaderStatus(input: TimelineStatusInput): string {
  if (input.isLatestAttempt) {
    return parseStatusToTimelineStatus(input.parseStatus) || input.traceStatus || ''
  }
  return input.traceStatus || parseStatusToTimelineStatus(input.parseStatus)
}


export type EntryProcessingInput = {
  id?: string
  parse_status?: string
  summary_status?: string
  error_message?: string
}

export function isEntryProcessingActive(entry: EntryProcessingInput): boolean {
  return ['pending', 'processing', 'finalizing'].includes(entry.parse_status || '')
    || (entry.parse_status === 'completed' && ['pending', 'processing'].includes(entry.summary_status || ''))
}

export function entryProcessingState(entry: EntryProcessingInput) {
  const parse = entry.parse_status || ''
  const summary = entry.summary_status || ''
  const stored = Boolean(entry.id) && ['pending', 'processing', 'finalizing', 'completed', 'failed', 'cancelled'].includes(parse)
  let status = parse
  let label = 'entryStatus.unknown'
  if (parse === 'failed') label = 'entryStatus.indexFailed'
  else if (parse === 'cancelled') label = 'entryStatus.cancelled'
  else if (parse === 'pending') label = 'entryStatus.indexPending'
  else if (parse === 'processing' || parse === 'finalizing') label = 'entryStatus.indexing'
  else if (parse === 'draft') label = 'library.statusDraft'
  else if (parse === 'rejected') label = 'library.intakeWaiting'
  else if (parse === 'deleting') label = 'entryStatus.deleting'
  else if (parse === 'completed') {
    if (summary === 'failed') { status = 'failed'; label = 'entryStatus.failed' }
    else if (summary === 'pending') { status = 'pending'; label = 'entryStatus.pending' }
    else if (summary === 'processing') { status = 'processing'; label = 'entryStatus.processing' }
    else if (summary === 'completed') label = 'entryStatus.completed'
    else if (summary === 'none') label = 'entryStatus.none'
    else if (summary === 'skipped') label = 'entryStatus.skipped'
  }
  const failed = status === 'failed'
  const active = isEntryProcessingActive(entry)
  const theme: 'danger' | 'primary' | 'default' = failed ? 'danger' : active ? 'primary' : 'default'
  return { stored, status, label, failed, active, theme, retryable: stored && failed }
}
