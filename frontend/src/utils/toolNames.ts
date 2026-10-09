export function normalizeToolName(name: string): string {
  return name === 'query_knowledge_graph' ? 'query_knowledge_context' : name
}
export function normalizeToolNames(names: string[]): string[] {
  return [...new Set(names.map(normalizeToolName))]
}
