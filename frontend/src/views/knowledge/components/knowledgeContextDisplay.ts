import type { KnowledgeContextResult, KnowledgeEvent, KnowledgeEvidence } from '@/api/knowledge-context'

type NullableContextResult = {
  [Key in keyof KnowledgeContextResult]?: KnowledgeContextResult[Key] | null
}

export function normalizeKnowledgeContextResult(result?: NullableContextResult | null): KnowledgeContextResult {
  return {
    excluded_missing_published_at_count: result?.excluded_missing_published_at_count ?? 0,
    published_at_missing_source_ids: result?.published_at_missing_source_ids ?? [],
    entities: Array.isArray(result?.entities) ? result.entities : [],
    facts: Array.isArray(result?.facts) ? result.facts : [],
    events: Array.isArray(result?.events) ? result.events : [],
    evidence: Array.isArray(result?.evidence) ? result.evidence : [],
    attention: Array.isArray(result?.attention) ? result.attention : [],
  }
}

export function eventEvidence(event: KnowledgeEvent, evidence?: KnowledgeEvidence[] | null): KnowledgeEvidence[] {
  const ids = new Set(Array.isArray(event.evidence_ids) ? event.evidence_ids : [])
  return (Array.isArray(evidence) ? evidence : []).filter(item => ids.has(item.id))
}
