export const natureNames = ['描述', '数据', '原理', '观点', '案例', '问题'];
export const tagKinds = ['nature', 'topic', 'object', 'event'];

export function tagKindLabel(kind: string, t: (key: string) => string): string {
  return t(`knowledgeBase.tagKind${kind[0].toUpperCase()}${kind.slice(1)}`);
}

export function tagLabel(tag: { name: string; kind?: string }, t: (key: string) => string): string {
  if (!tag.kind) return tag.name;
  return `${tagKindLabel(tag.kind, t)} · ${tag.name}`;
}
