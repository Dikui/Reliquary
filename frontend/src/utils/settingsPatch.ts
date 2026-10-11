// Compare the loaded form with the edited form, not with display defaults
// encoded back into the stored resource. Arrays remain atomic values.
export function changedSettings(before: Record<string, any>, after: Record<string, any>): Record<string, any> {
  const patch: Record<string, any> = {}
  for (const [key, value] of Object.entries(after)) {
    if (JSON.stringify(before[key]) === JSON.stringify(value)) continue
    if (value !== null && !Array.isArray(value) && typeof value === 'object' && before[key] !== null && typeof before[key] === 'object' && !Array.isArray(before[key])) {
      const nested = changedSettings(before[key], value)
      if (Object.keys(nested).length) patch[key] = nested
    } else patch[key] = value
  }
  return patch
}
