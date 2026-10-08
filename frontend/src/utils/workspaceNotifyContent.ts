import { h, type VNode } from 'vue'

export interface WorkspaceNotifyContentOptions {
  template: string
  name: string
}

export function renderWorkspaceNotifyContent(opts: WorkspaceNotifyContentOptions): () => VNode {
  return () => h('span', { style: { lineHeight: '1.6', color: 'var(--td-text-color-secondary)' } },
    opts.template.split(/(\{name\})/g).map(part => part === '{name}'
      ? h('strong', { style: { fontWeight: '600', color: 'var(--td-text-color-primary)' } }, opts.name)
      : h('span', part)))
}
