export interface EntrySourceLabelInput {
  source_kind?: string
  type?: string
  channel?: string
  acquisition?: string
  ingest_api_key_id?: number
  ingest_api_key_name?: string
  last_ingest_api_key_name?: string
  metadata?: { intake?: { source?: string } } | null
}

const ingestAcquisitions = new Set(['verbatim', 'extracted', 'model_written'])
const legacyChannels = new Set(['web', 'rss', 'browser_extension'])

// Channel alone cannot distinguish old API manual creation from ingest.
// Supplements retain the historical channel and only update the last tool.
export function isIngestAPIEntry(entry: EntrySourceLabelInput): boolean {
  if (entry.source_kind === 'mine' || entry.source_kind === 'report') return false
  const intakeSource = entry.metadata?.intake?.source
  if (intakeSource) return intakeSource === 'ingest'
  if (entry.channel && legacyChannels.has(entry.channel)) return false
  return entry.channel === 'api' && (
    ingestAcquisitions.has(entry.acquisition || '')
    || Boolean(entry.ingest_api_key_id)
    || Boolean(entry.ingest_api_key_name?.trim())
  )
}

export function getExternalToolName(entry: EntrySourceLabelInput): string {
  return entry.ingest_api_key_name?.trim() || entry.last_ingest_api_key_name?.trim() || ''
}
