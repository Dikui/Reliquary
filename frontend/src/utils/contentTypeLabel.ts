/** Built-in types follow the active locale; library-defined names stay verbatim. */
export function contentTypeLabel(value: string | null | undefined, t: (key: string) => string): string {
  switch (value) {
    case 'event': return t('library.contentTypes.event')
    case 'opinion': return t('library.contentTypes.opinion')
    case 'research': return t('library.contentTypes.research')
    case 'tutorial': return t('library.contentTypes.tutorial')
    case 'case': return t('library.contentTypes.case')
    case 'reference': return t('library.contentTypes.reference')
    case 'other': return t('library.contentTypes.other')
    default: return value || ''
  }
}
