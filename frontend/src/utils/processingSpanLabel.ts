type Translate = (key: string, values?: Record<string, string>) => string

export function processingSpanLabel(name: string, t: Translate): string {
  const segment = /^(?:review|processing)\.segment\.(\d+)\/(\d+)$/.exec(name)
  if (segment) return t('processingStages.segmentProgress', { current: segment[1], total: segment[2] })
  if (name === 'intake.review') return t('processingStages.intakeReview')
  if (name === 'processing.summary_merge') return t('processingStages.summaryMerge')
  return name
}
