import { buildReferenceSections, type KnowledgeReferenceLike } from './referenceSources'

export function formatTemporarySummary(answer: string, references: KnowledgeReferenceLike[]): string {
  const sources = buildReferenceSections(references)
    .flatMap(section => section.items)
    .filter(item => item.kind === 'document')
    .map(item => `- ${item.url ? `[${item.title}](${item.url})` : `${item.title} (${item.knowledgeId || item.key})`}`)
  return `${answer.trim()}${sources.length ? `\n\n---\n\n${sources.join('\n')}` : ''}\n`
}

export function downloadTemporarySummary(answer: string, references: KnowledgeReferenceLike[]): void {
  const url = URL.createObjectURL(new Blob([formatTemporarySummary(answer, references)], { type: 'text/markdown;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url
  link.download = 'knowledge-summary.md'
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
