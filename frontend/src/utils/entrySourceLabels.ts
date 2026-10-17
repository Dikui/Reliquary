export interface EntrySourceLabelInput {
  source_kind?: string
  type?: string
  file_type?: string
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

// Storage aliases describe Markdown, not how the material was submitted.
export function normalizeEntryFileType(value: string): string {
  const type = value.trim().toLowerCase()
  return ['manual', 'markdown', 'url'].includes(type) ? 'md' : type
}

export function entryFileFormatLabel(entry: EntrySourceLabelInput, t: (key: string) => string): string {
  const format = normalizeEntryFileType(entry.file_type || '')
  if (format === 'md' || (!format && ['manual', 'url'].includes(entry.type || ''))) return 'Markdown'
  return format ? format.toUpperCase() : t('library.typeFile')
}

export function entryChannelLabel(entry: EntrySourceLabelInput, t: (key: string) => string): string {
  switch (entry.channel) {
    case 'api': return t('library.channelApi')
    case 'web': return t('library.channelWeb')
    case 'rss': return t('library.channelRSS')
    case 'browser_extension': return t('library.channelBrowserExtension')
    default: return t('library.channelUnknown')
  }
}
