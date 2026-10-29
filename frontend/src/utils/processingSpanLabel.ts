type Translate = (key: string, values?: Record<string, string>) => string

export function processingSpanLabel(name: string, t: Translate): string {
  const segment = /^(?:review|processing)\.segment\.(\d+)\/(\d+)$/.exec(name)
  if (segment) return t('knowledgeStages.segmentProgress', { current: segment[1], total: segment[2] })
  if (name === 'intake.review') return t('knowledgeStages.intakeReview')
  if (name === 'processing.summary_merge') return t('knowledgeStages.summaryMerge')
  return name
}
