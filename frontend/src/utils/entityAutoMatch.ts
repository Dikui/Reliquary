export function normalizeEntityName(value: string): string {
  return value.normalize('NFKC').trim().replace(/\s+/gu, ' ').toLowerCase()
}

export function defaultEntityAutoMatch(value: string): boolean {
  const name = normalizeEntityName(value)
  return !!name && !(/^[\p{Script=Han}]{1,2}$/u.test(name) || /^[a-z0-9]{1,2}$/.test(name))
}

export function entityMatchNames(names: string[]) {
  return [...new Map(names.filter(name => normalizeEntityName(name)).map(name => [normalizeEntityName(name), name.trim()])).entries()]
    .map(([key, label]) => ({ key, label, enabled: defaultEntityAutoMatch(key) }))
}

export function currentNameOverrides(names: string[], overrides: Record<string, boolean> = {}) {
  const keys = new Set(entityMatchNames(names).map(name => name.key))
  return Object.fromEntries(Object.entries(overrides).filter(([key]) => keys.has(key)))
}
