// Pure logic for sidebar session grouping (replaces the old source filter).

export type SessionGroupMode = 'none' | 'date'

export const SESSION_GROUP_MODE_STORAGE_KEY = 'reliquary:session-group-mode'
export const DEFAULT_SESSION_GROUP_MODE: SessionGroupMode = 'none'

export interface SessionForGrouping {
  id: string
  title?: string
  is_pinned?: boolean
  created_at?: string
  updated_at?: string
  description?: string
  user_id?: string
  originalIndex?: number
}

export interface SessionGroup<T extends SessionForGrouping = SessionForGrouping> {
  key: string
  label: string
  items: T[]
}

export type SessionOrigin = { kind: 'web' }

export interface GroupModeOption {
  value: SessionGroupMode
  label: string
}

export interface SourceGroupLabels {
  web: string
  api: string
}

export function buildGroupModeOptions(labels: {
  none: string
  date: string
}): GroupModeOption[] {
  return [
    { value: 'none', label: labels.none },
    { value: 'date', label: labels.date },
  ]
}

export function readStoredGroupMode(): SessionGroupMode {
  if (typeof localStorage === 'undefined') return DEFAULT_SESSION_GROUP_MODE
  const raw = localStorage.getItem(SESSION_GROUP_MODE_STORAGE_KEY)
  if (raw === 'none' || raw === 'date') return raw
  // Legacy "source" mode — channels are now separate folders like OpenAI projects.
  if (raw === 'source') return 'none'
  return DEFAULT_SESSION_GROUP_MODE
}

export function storeGroupMode(mode: SessionGroupMode): void {
  if (typeof localStorage === 'undefined') return
  localStorage.setItem(SESSION_GROUP_MODE_STORAGE_KEY, mode)
}

export function resolveSessionOrigin(_session: SessionForGrouping): SessionOrigin { return { kind: 'web' } }

export function originGroupKey(_origin: SessionOrigin): string { return 'web' }

export function classifyDateBucket(dateStr: string | undefined): DateBucketKey {
  if (!dateStr) return 'earlier'

  const date = new Date(dateStr)
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterday = new Date(today.getTime() - 24 * 60 * 60 * 1000)
  const sevenDaysAgo = new Date(today.getTime() - 7 * 24 * 60 * 60 * 1000)
  const thirtyDaysAgo = new Date(today.getTime() - 30 * 24 * 60 * 60 * 1000)
  const oneYearAgo = new Date(today.getTime() - 365 * 24 * 60 * 60 * 1000)
  const sessionDate = new Date(date.getFullYear(), date.getMonth(), date.getDate())

  if (sessionDate.getTime() >= today.getTime()) return 'today'
  if (sessionDate.getTime() >= yesterday.getTime()) return 'yesterday'
  if (date.getTime() >= sevenDaysAgo.getTime()) return 'last7Days'
  if (date.getTime() >= thirtyDaysAgo.getTime()) return 'last30Days'
  if (date.getTime() >= oneYearAgo.getTime()) return 'lastYear'
  return 'earlier'
}

const DATE_BUCKET_ORDER = [
  'pinned',
  'today',
  'yesterday',
  'last7Days',
  'last30Days',
  'lastYear',
  'earlier',
] as const

export type DateBucketKey = (typeof DATE_BUCKET_ORDER)[number]

export function groupSessionsByDate<T extends SessionForGrouping>(
  sessions: T[],
  bucketLabels: Record<DateBucketKey, string>,
  categorize: (session: T) => DateBucketKey,
): SessionGroup<T>[] {
  const buckets = new Map<DateBucketKey, T[]>()
  for (const key of DATE_BUCKET_ORDER) buckets.set(key, [])

  for (const session of sessions) {
    const bucket: DateBucketKey = session.is_pinned ? 'pinned' : categorize(session)
    buckets.get(bucket)!.push(session)
  }

  return DATE_BUCKET_ORDER
    .filter((key) => (buckets.get(key)?.length ?? 0) > 0)
    .map((key) => ({
      key,
      label: bucketLabels[key],
      items: buckets.get(key)!,
    }))
}

export function groupSessionsFlat<T extends SessionForGrouping>(
  sessions: T[],
  pinnedLabel: string,
): SessionGroup<T>[] {
  const pinned = sessions.filter((s) => s.is_pinned)
  const rest = sessions.filter((s) => !s.is_pinned)
  const groups: SessionGroup<T>[] = []
  if (pinned.length > 0) groups.push({ key: 'pinned', label: pinnedLabel, items: pinned })
  if (rest.length > 0) groups.push({ key: 'all', label: '', items: rest })
  return groups
}

export function groupSessions<T extends SessionForGrouping>(
  mode: SessionGroupMode,
  sessions: T[],
  opts: {
    pinnedLabel: string
    bucketLabels: Record<DateBucketKey, string>
    categorizeDate: (session: T) => DateBucketKey
    sourceLabels: SourceGroupLabels
  },
): SessionGroup<T>[] {
  if (!sessions.length) return []
  switch (mode) {
    case 'none':
      return groupSessionsFlat(sessions, opts.pinnedLabel)
    case 'date':
    default:
      return groupSessionsByDate(sessions, opts.bucketLabels, opts.categorizeDate)
  }
}

/** Chat list rows show source badges when channels live in separate folders. */
export function shouldShowSessionSourceBadge(_mode: SessionGroupMode): boolean {
  return false
}
