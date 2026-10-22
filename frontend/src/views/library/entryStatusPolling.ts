import { isEntryProcessingActive, type EntryProcessingInput } from '@/utils/entryProcessingStatus'

export type EntryPollStatus = EntryProcessingInput

export function isEntryParseInFlight(status?: string): boolean {
  return status === 'pending' || status === 'processing' || status === 'finalizing'
}

export function entryNeedsStatusPolling(item: EntryPollStatus): boolean {
  return isEntryProcessingActive(item)
}
