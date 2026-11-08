import type { ShelfReviewDecision, ShelfReviewItem, ShelfReviewOperation, ShelfNameConfirmation, ShelfImportColumnFeedback } from '../api/shelf-review'

export const SHELF_REVIEW_GROUPS = ['new', 'suspected_same', 'cross_kind_same_name', 'modified', 'disappeared', 'unparseable', 'uncertain', 'required_missing', 'cross_table_contradiction', 'unmapped_column'] as const
// An operation with several findings appears once, under its blocking finding.
export function shelfReviewGroup(item: ShelfReviewItem): string {
  return ['required_missing', 'uncertain', 'unparseable', 'cross_kind_same_name', 'suspected_same', 'disappeared', 'cross_table_contradiction', 'unmapped_column', 'modified', 'new'].find(type => item.conflicts.some(conflict => conflict.type === type)) || 'modified'
}
export function shelfReviewReady(item: ShelfReviewItem, decision?: ShelfReviewDecision): boolean {
  if (!decision) return false
  if (['discard', 'defer', 'keep'].includes(decision.action)) return true
  if (decision.action === 'return_to_submitter' || decision.action === 'replace') return false
  return item.conflicts.every(conflict => ['new', 'modified'].includes(conflict.type)
    || (conflict.type === 'disappeared' && decision.action === 'retire')
    || (['cross_table_contradiction', 'unmapped_column'].includes(conflict.type) && item.operation.type === 'import_notice'))
}
export function shelfUnmappedColumnFeedback(item: ShelfReviewItem): ShelfImportColumnFeedback | undefined {
  if (item.operation.type !== 'import_notice') return undefined
  const details = item.conflicts.find(conflict => conflict.type === 'unmapped_column')?.details as Partial<ShelfImportColumnFeedback> | undefined
  if (!details || ![details.worksheet, details.column, details.header].every(value => typeof value === 'string')
    || typeof details.nonempty_rows !== 'number' || !Number.isSafeInteger(details.nonempty_rows) || details.nonempty_rows < 0
    || !Array.isArray(details.examples) || !details.examples.every(value => typeof value === 'string')) return undefined
  return details as ShelfImportColumnFeedback
}
export function shelfNameConfirmations(item: ShelfReviewItem): ShelfNameConfirmation[] {
  return item.conflicts.flatMap(conflict => {
    const details = conflict.details as { conflicts?: ShelfNameConfirmation[] } | undefined
    return conflict.type === 'cross_kind_same_name' ? details?.conflicts || conflict.candidates || [] : []
  }).map(({ name, other_entity_id, other_kind }) => ({ name, other_entity_id, other_kind }))
}
export function shelfReviewSubject(item: ShelfReviewItem): { name: string; kind: string } {
  const feedback = shelfUnmappedColumnFeedback(item)
  if (feedback) return { name: `${feedback.worksheet} · ${feedback.column}`, kind: '' }
  const operation = item.operation
  const before = item.before as Record<string, unknown> | null
  const after = item.after as Record<string, unknown> | null
  const entity = (operation.entity || operation.patch || operation.certification || operation.document) as Record<string, unknown> | undefined
  return { name: String(entity?.name ?? after?.canonical_name ?? before?.canonical_name ?? operation.alias ?? operation.entity_id ?? item.id), kind: String(entity?.kind ?? after?.kind ?? before?.kind ?? '') }
}
export function shelfOperationForEdit(item: ShelfReviewItem): ShelfReviewOperation {
  const operation = JSON.parse(JSON.stringify(item.operation)) as ShelfReviewOperation
  // A correction explicitly replaces submitter issues; the server validates all values again.
  delete operation.issues
  if (operation.entity) operation.entity = { name: '', ...(operation.entity as object) }
  if (operation.certification) operation.certification = { ...(operation.type === 'create_certification' ? { name: '' } : {}), owner_id: '', standard: '', certification_object: '', region: '', ...(operation.certification as object) }
  if (operation.document) operation.document = { owner_id: '', ...(operation.document as object) }
  return operation
}

// Known server conflict messages map to translations; a quoted value becomes {value}.
// shortcut: covers the import conflicts seen in customer acceptance; other messages show as sent, add patterns when they surface.
const SHELF_CONFLICT_MESSAGES: Array<[RegExp, string]> = [
  [/^entity, relationship or library not found$/, 'notFound'],
  [/^formula, error or formatted numeric cells require explicit values$/, 'formulaCells'],
  [/^(?:model appears in multiple rows|uncertain: multiple rows resolve to the same model); submitter must decide whether to merge$/, 'modelRows'],
  [/^name, code, alias or relationship already exists$/, 'exists'],
  [/^row has missing identity fields or is a group title; submitter must resolve$/, 'groupTitle'],
  [/^sample description requires explicit part-condition review$/, 'sampleDescription'],
  [/^uncertain: certification standard has no configured name or alias$/, 'standardUnconfigured'],
  [/^uncertain: \w+ identity "(.*)" cannot be identified$/, 'identityUnknown'],
  [/^uncertain: R&D deadline "(.*)" needs a confirmed stage mapping$/, 'deadlineStage'],
  [/^uncertain: (?:record cell fill requires an explicit interpretation|cell fill .* requires a confirmed meaning)$/, 'cellFill'],
  [/^uncertain: repeated cell part rows propose different attributes$/, 'cellPartAttributes'],
  [/^uncertain: part model attributes require an identified cell part$/, 'cellPartMissing'],
  [/^uncertain: requirement appears in multiple rows; submitter must decide whether to merge$/, 'requirementRows'],
  [/^uncertain: same product name belongs to different parents; choose identity explicitly$/, 'parents'],
  [/^configured worksheet is missing$/, 'worksheetMissing'],
  [/^uncertain: blank regional (?:status must be resolved by the submitter|cells need an explicit per-row decision)$/, 'blankRegional'],
  [/^certification key appears in multiple rows; submitter must decide whether to merge$/, 'certificationRows'],
  [/^certification key has multiple candidates; submit a corrected complete record$/, 'certificationCandidates'],
  [/^uncertain: market necessity "(.*)" has no explicit meaning$/, 'marketNecessity'],
  [/^uncertain: certification key must match exactly one record or an explicitly selected candidate$/, 'certificationKeyMatch'],
]
export function shelfConflictMessage(message: string): { key: string, value: string } | null {
  for (const [pattern, key] of SHELF_CONFLICT_MESSAGES) {
    const match = pattern.exec(message)
    if (match) return { key, value: match[1] ?? '' }
  }
  return null
}
