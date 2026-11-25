import { countryRegionGroups, type RegionGroup } from '../config/countryRegions'

export interface Region {
  key: string
  name: string
}

interface NamedEntity {
  id: string
  name: string
}

export interface SaleStatus {
  region: string
  region_name: string
  status: string
  raw: string
  notes: string
  planned_eom: boolean
  conditional: boolean
}

export interface ChecklistItem {
  name: string
  status: string
  status_label: string
  gap: boolean
  has_file: boolean
}

export interface Certification {
  standard_name: string
  status: string
  region_name: string
  origin: string
}

export interface ShelfModel {
  id: string
  name: string
  product_name: string
  platform: string
  product_line: NamedEntity
  current_stage: string
  sale_statuses: SaleStatus[]
  checklist: ChecklistItem[]
  certifications: Certification[]
}

export interface ShelfSnapshot {
  library: NamedEntity
  as_of: string
  regions: Region[]
  product_lines: NamedEntity[]
  models: ShelfModel[]
}

export const statuses = [
  { key: 'GA', label: 'GA', tone: 'green', color: '#5fe0bc' },
  { key: 'DEV', label: 'DEV', tone: 'purple', color: '#428eff' },
  { key: 'Planning', label: 'Planning', tone: 'amber', color: '#edc17e' },
  { key: 'EOM', label: 'EOM', tone: 'amber', color: '#b68761' },
  { key: '待定', label: '待定', tone: 'gray', color: '#93a6ca' },
  { key: 'unrecorded', label: '未记录', tone: 'gray', color: '#687b9c' },
] as const

type Status = (typeof statuses)[number]

export interface ShelfRow {
  id: string
  model: ShelfModel
  sale?: SaleStatus
  regionKey: string
  regionLabel: string
  status: Status
  statusLabel: string
}

export function expandShelf(snapshot: ShelfSnapshot): ShelfRow[] {
  const regionNames = new Map(snapshot.regions.map(({ key, name }) => [key, name]))
  return snapshot.models.flatMap((model) => {
    const sales = model.sale_statuses.length ? model.sale_statuses : [undefined]
    return sales.map((sale, index) => {
      const status = statuses.find((entry) => entry.key === sale?.status) ?? statuses[5]
      return {
        id: JSON.stringify([model.id, index]),
        model,
        sale,
        regionKey: sale?.region ?? '',
        regionLabel: sale?.region_name || regionNames.get(sale?.region ?? '') || sale?.region || '未记录',
        status,
        statusLabel: sale?.raw || sale?.status || status.label,
      }
    })
  })
}

// Region keys and names may differ in case, width or spaces from the configured candidates.
const regionKey = (name: string) => name.normalize('NFKC').replace(/\s+/g, '').toLowerCase()

function resolveGroup(regions: Region[], { names, prefix }: RegionGroup): Region | undefined {
  const candidates = names.map(regionKey)
  const matches = (test: (own: string, candidate: string) => boolean) => {
    for (const candidate of candidates) {
      const match = regions.find((region) => [region.key, region.name].some((value) => test(regionKey(value), candidate)))
      if (match) return match
    }
    return undefined
  }
  return matches((own, candidate) => own === candidate) ??
    (prefix ? matches((own, candidate) => own.startsWith(candidate)) : undefined)
}

// Every configured region a country's records may sit under, most specific first.
export function findCountryRegions(regions: Region[], code: string): Region[] {
  const found = countryRegionGroups(code).map((group) => resolveGroup(regions, group))
  return [...new Set(found.filter((region): region is Region => region !== undefined))]
}

export function findCountryRegion(regions: Region[], code: string): Region | undefined {
  return findCountryRegions(regions, code)[0]
}

export interface ShelfFilters {
  region: string
  productLine: string
  status: string
  query: string
}

export function searchKey(value: string): string {
  return value.normalize('NFKC').trim().toLowerCase()
}

export function filterShelf(rows: ShelfRow[], filters: ShelfFilters): ShelfRow[] {
  const terms = searchKey(filters.query).split(/\s+/).filter(Boolean)
  return rows.filter(({ regionKey, model, status }) =>
    (!filters.region || regionKey === filters.region) &&
    (!filters.productLine || model.product_line.id === filters.productLine) &&
    (!filters.status || status.key === filters.status) &&
    terms.every((term) => [model.name, model.product_name].some((name) => searchKey(name).includes(term))),
  )
}

export function summarizeShelf(rows: ShelfRow[]) {
  const byRegion = new Map<string, number>()
  const byStatus = new Map<Status['key'], number>()
  for (const { regionKey, status } of rows) {
    if (regionKey) byRegion.set(regionKey, (byRegion.get(regionKey) ?? 0) + 1)
    byStatus.set(status.key, (byStatus.get(status.key) ?? 0) + 1)
  }
  return { byRegion, byStatus }
}

const certStatuses = new Map([
  ['obtained', { label: '已取证', tone: 'green' }],
  ['in_progress', { label: '办理中', tone: 'amber' }],
  ['paused', { label: '暂停', tone: 'gray' }],
  ['delayed', { label: '延期', tone: 'amber' }],
  ['not_started', { label: '未开始', tone: 'gray' }],
  ['date_conflict', { label: '日期冲突', tone: 'amber' }],
])

export function certificationStatus(status: string) {
  return certStatuses.get(status) ?? { label: '未知', tone: 'gray' }
}

export function checklistTone(item: ChecklistItem): string {
  if (item.gap || item.status === 'draft_only') return 'amber'
  return item.status === 'delivered' ? 'green' : 'gray'
}

export function snapshotDate(asOf: string): string {
  return /^\d{4}-\d{2}-\d{2}/.exec(asOf)?.[0] ?? ''
}
