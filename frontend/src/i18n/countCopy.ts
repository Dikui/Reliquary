type CountTranslation = (key: string, named: Record<string, unknown>, plural?: number) => string

const countFields: Record<string, Record<string, readonly [string, string]>> = {
  'chat.referencesDocAndWebCount': {'docCount': ['documents', 'documents'], 'webCount': ['webSources', 'webSources']},
  'agentStream.search.foundResultsFromFiles': {'count': ['results', 'results'], 'files': ['fileCount', 'files']},
  'agentStream.search.grepSummary': {'chunks': ['matchingChunks', 'matchingChunks'], 'docs': ['documents', 'documents']},
  'agent.contextCompactedSummary': {'before': ['beforeTokens', 'tokens'], 'after': ['afterTokens', 'tokens']},
  'knowledgeBase.strategy.reprocessResult': {'flagged': ['reviewFlags', 'reviewFlags'], 'conflicts': ['manualConflicts', 'manualConflicts']},
  'system.globalSettings.runtime.tasks.purgeArchivedConfirm': {'count': ['records', 'failedRecords']},
}

// Keep the numeric selectors separate from formatted numbers, including HTML.
export function formatCountCopy(
  t: CountTranslation,
  key: string,
  counts: Record<string, number>,
  named: Record<string, unknown> = {},
): string {
  const params: Record<string, unknown> = { ...counts, ...named }
  for (const [field, [output, noun]] of Object.entries(countFields[key] || {})) {
    params[output] = t(`countCopy.${noun}`, { count: params[field] }, counts[field]!)
  }
  return t(key, params)
}
