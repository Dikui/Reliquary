// Country → shelf regions. A country shows the records of every region it maps to.
// Each group lists the region keys or names it may appear under in a library's
// shelf configuration (compared ignoring case, width and spaces); a group resolves
// to the first configured region matching exactly, or with `prefix`, starting with
// a candidate (so "IEC-欧洲（含英国…）" matches "IEC-欧洲").
//
// Provisional mapping (2026-11-25): the customer will supply the official
// country → region relationship; replace the tables below when it arrives.
export interface RegionGroup {
  names: readonly string[]
  prefix: boolean
}

const listedCountries: Readonly<Record<string, readonly string[]>> = {
  CN: ['中国大陆', 'GB', 'CN'],
  JP: ['日本', 'JP', 'JIS'],
  AU: ['澳洲', 'AU', 'AS'],
  US: ['UL 系列标准适用地区', 'UL', 'US'],
}

// Countries not listed above use an IEC sub-region (when the library has it) plus the generic IEC region.
const iecSubRegions: readonly { names: readonly string[]; countries: string }[] = [
  {
    names: ['IEC-欧洲', 'iec_europe'],
    countries: 'AL AD AT BY BE BA BG HR CY CZ DK EE FI FR DE GR HU IS IE IT XK LV LI LT LU MT MD MC ME NL MK NO PL PT RO RU SM RS SK SI ES SE CH UA GB VA',
  },
  {
    names: ['IEC-拉美', 'iec_latam'],
    countries: 'MX GT BZ SV HN NI CR PA CU DO HT JM TT BS BB PR AR BO BR CL CO EC GY PY PE SR UY VE',
  },
  {
    names: ['IEC-中东', 'iec_middle_east'],
    countries: 'AE SA QA KW BH OM YE IQ IR JO LB SY IL PS TR',
  },
  {
    names: ['IEC-非洲', 'iec_africa'],
    countries: 'DZ AO BJ BW BF BI CM CV CF TD KM CG CD CI DJ EG GQ ER SZ ET GA GM GH GN GW KE LS LR LY MG MW ML MR MU MA MZ NA NE NG RW ST SN SC SL SO ZA SS SD TZ TG TN UG ZM ZW EH',
  },
  {
    names: ['IEC-东南亚', 'iec_se_asia'],
    countries: 'BN KH ID LA MY MM PH SG TH TL VN',
  },
]

export const genericIecRegion: RegionGroup = { names: ['IEC 系列标准适用地区', 'IEC'], prefix: false }

export const marketShortcuts = [
  { code: 'CN', label: '中国大陆', standard: 'GB' },
  { code: 'JP', label: '日本', standard: 'JIS' },
  { code: 'AU', label: '澳洲', standard: 'AS' },
  { code: 'US', label: '美国', standard: 'UL' },
]

export function countryRegionGroups(code: string): RegionGroup[] {
  const upper = code.toUpperCase()
  const listed = listedCountries[upper]
  if (listed) return [{ names: listed, prefix: false }]
  const sub = iecSubRegions.find(({ countries }) => countries.split(' ').includes(upper))
  return sub ? [{ names: sub.names, prefix: true }, genericIecRegion] : [genericIecRegion]
}

export function countryStandard(code: string): string {
  const upper = code.toUpperCase()
  if (upper === 'US') return 'UL'
  return listedCountries[upper] ? '' : 'IEC'
}
