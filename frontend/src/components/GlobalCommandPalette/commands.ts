import type { Router } from 'vue-router'
import type { Composer } from 'vue-i18n'

/**
 * A single command that can be searched and invoked from the palette.
 * Kept intentionally minimal — commands are "just do something now", not
 * entities with detail pages.
 */
export interface CmdkCommand {
  id: string
  /** Localized display label. */
  label: string
  /** TDesign icon name rendered on the left. */
  icon: string
  /** Extra tokens used purely for fuzzy matching (aliases, synonyms). */
  keywords?: string[]
  /** Executed on primary action. Should close the palette itself if needed. */
  run: () => void
}

export interface CommandContext {
  router: Router
  t: Composer['t']
  /** Closes the palette; typically wires to commandPaletteStore.closePalette. */
  close: () => void
}

/**
 * Build the flat command list. Commands are intentionally static — dynamic
 * article matches live in their own result group.
 */
export function buildCommands(ctx: CommandContext): CmdkCommand[] {
  const { router, t, close } = ctx
  return [
    {
      id: 'open-entry',
      label: t('commandPalette.quick.knowledge'),
      icon: 'file',
      keywords: ['kb', 'entry', 'base', '知识库', '文档'],
      run: () => {
        close()
        router.push('/platform/entries')
      },
    },
    {
      id: 'open-settings',
      label: t('commandPalette.quick.settings'),
      icon: 'setting',
      keywords: ['settings', 'preferences', 'config', '设置', '配置'],
      run: () => {
        close()
        router.push('/platform/settings')
      },
    },
  ]
}

/**
 * Filter commands by a free-text query. Matches on label OR any keyword,
 * case-insensitive. Empty query returns the full list unchanged so it can
 * double as a "default" empty-state listing.
 */
export function filterCommands(commands: CmdkCommand[], query: string): CmdkCommand[] {
  const q = (query || '').trim().toLowerCase()
  if (!q) return commands
  return commands.filter((cmd) => {
    if (cmd.label.toLowerCase().includes(q)) return true
    if (cmd.keywords) {
      for (const kw of cmd.keywords) {
        if (kw.toLowerCase().includes(q)) return true
      }
    }
    return false
  })
}
