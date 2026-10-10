export function normalizeToolName(name: string): string {
  return name
}
export function normalizeToolNames(names: string[]): string[] {
  return [...new Set(names.map(normalizeToolName))]
}
