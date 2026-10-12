export interface EntryModelCall { stage: string; tier: string; model_id?: string; calls: number }

export function modelCallsFromSpan(span?: { output?: unknown; metadata?: unknown }): EntryModelCall[] {
  for (const value of [span?.metadata, span?.output]) {
    if (!value || typeof value !== 'object' || !('model_calls' in value)) continue
    const calls = (value as { model_calls?: unknown }).model_calls
    if (!Array.isArray(calls)) continue
    return calls.filter((call): call is EntryModelCall => call && typeof call.stage === 'string' && typeof call.tier === 'string' && Number.isInteger(call.calls) && call.calls >= 0)
  }
  return []
}
