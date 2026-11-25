// Country → shelf region. Each country lists the region keys or names it may
// appear under in a library's shelf configuration; the first configured region
// whose key or name matches (ignoring case, width and spaces) wins. Countries
// not listed fall back to a region starting with one of `defaultRegionCandidates`.
const countryRegions: Readonly<Record<string, readonly string[]>> = {
  CN: ['中国大陆', 'GB', 'CN'],
  JP: ['日本', 'JP', 'JIS'],
  AU: ['澳洲', 'AU', 'AS'],
  US: ['UL 系列标准适用地区', 'UL', 'US'],
}

// Matched by prefix, so "IEC-欧洲（含英国…）" also counts as the IEC region.
export const defaultRegionCandidates: readonly string[] = ['IEC 系列标准适用地区', 'IEC']

export const marketShortcuts = [
  { code: 'CN', label: '中国大陆', standard: 'GB' },
  { code: 'JP', label: '日本', standard: 'JIS' },
  { code: 'AU', label: '澳洲', standard: 'AS' },
  { code: 'US', label: '美国', standard: 'UL' },
]

export function countryRegionCandidates(code: string): { names: readonly string[]; prefix: boolean } {
  const names = countryRegions[code.toUpperCase()]
  return names ? { names, prefix: false } : { names: defaultRegionCandidates, prefix: true }
}

export function countryStandard(code: string): string {
  const upper = code.toUpperCase()
  if (upper === 'US') return 'UL'
  return countryRegions[upper] ? '' : 'IEC'
}
