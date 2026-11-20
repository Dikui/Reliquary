import type { ShelfCertification, ShelfCertificationStatus, ShelfCondition } from '../api/shelf'

const statusKeys = {
  date_conflict: 'shelfModel.certStatus.date_conflict',
  obtained: 'shelfModel.certStatus.obtained',
  paused: 'shelfModel.certStatus.paused',
  delayed: 'shelfModel.certStatus.delayed',
  not_started: 'shelfModel.certStatus.not_started',
  in_progress: 'shelfModel.certStatus.in_progress',
  unknown: 'shelfModel.certStatus.unknown',
} as const satisfies Record<ShelfCertificationStatus, string>

export function certificationStatusKey(status?: ShelfCertificationStatus) {
  return statusKeys[status ?? 'unknown']
}

type CertificationGroupStatus = 'completed' | 'delayed' | 'date_conflict' | 'in_progress'

export function summarizeCertificationProgress(rows: readonly Pick<ShelfCertification, 'status'>[]): {
  status: CertificationGroupStatus
  delayedCount: number
} {
  const delayedCount = rows.filter(row => row.status === 'delayed').length
  // Same order as shelf-supplement.md: dates to verify outrank delays.
  if (rows.some(row => row.status === 'date_conflict')) return { status: 'date_conflict', delayedCount }
  if (rows.length && rows.every(row => row.status === 'obtained')) return { status: 'completed', delayedCount }
  if (delayedCount) return { status: 'delayed', delayedCount }
  return { status: 'in_progress', delayedCount }
}

interface CertificationGroup<T> {
  key: string
  cellPartId: string
  conditions: ShelfCondition[] | null
  rows: T[]
}

export function groupCertifications<T extends Pick<ShelfCertification, 'certification'>>(rows: readonly T[]) {
  const groups = new Map<string, CertificationGroup<T>>()
  for (const row of rows) {
    const { cell_part_id: cellPartId = '', conditions } = row.certification
    // Each condition is a set of allowed parts, and all module conditions apply together.
    const conditionKeys = (conditions ?? []).map(({ module_type, part_ids }) =>
      JSON.stringify([module_type, [...new Set(part_ids)].sort()]),
    ).sort()
    const key = JSON.stringify([cellPartId, conditionKeys])
    let group = groups.get(key)
    if (!group) {
      group = { key, cellPartId, conditions, rows: [] }
      groups.set(key, group)
    }
    group.rows.push(row)
  }
  return [...groups.values()]
}
