export type EntryPollStatus = {
  parse_status?: string
}

export function isEntryParseInFlight(status?: string): boolean {
  return status === 'pending' || status === 'processing' || status === 'finalizing'
}

export function entryNeedsStatusPolling(item: EntryPollStatus): boolean {
  return isEntryParseInFlight(item.parse_status)
}
