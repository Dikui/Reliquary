export function tagLabel(tag: { name: string; kind?: string }, t: (key: string) => string): string {
  if (!tag.kind) return tag.name;
  const key = `knowledgeBase.tagKind${tag.kind[0].toUpperCase()}${tag.kind.slice(1)}`;
  return `${t(key)} · ${tag.name}`;
}
