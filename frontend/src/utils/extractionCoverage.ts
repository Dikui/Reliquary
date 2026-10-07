import type { KnowledgeEvidence } from '../api/knowledge-context'

export function shouldShowExtractionCoverage(evidence: Pick<KnowledgeEvidence, 'extraction_coverage' | 'extraction_status' | 'processed_segments' | 'total_segments'>): boolean {
  return Boolean(evidence.extraction_coverage && ((evidence.processed_segments || 0) > 0 || (evidence.total_segments || 0) > 0 || evidence.extraction_status === 'complete'))
}
