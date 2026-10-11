export type KnowledgePollStatus = {
  parse_status?: string
}

export function isKnowledgeParseInFlight(status?: string): boolean {
  return status === 'pending' || status === 'processing' || status === 'finalizing'
}

export function knowledgeNeedsStatusPolling(item: KnowledgePollStatus): boolean {
  return isKnowledgeParseInFlight(item.parse_status)
}
